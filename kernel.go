package loong

import (
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
)

// Kernel assembles and runs the component tree in a single process.
type Kernel struct {
	factories   map[string]regEntry
	services    map[reflect.Type]any
	serviceIdx  map[reflect.Type]string // service value type -> component type name
	nodesByType map[string][]*Node      // component type name -> registered nodes
	idIndex     map[string]*Node        // node id -> node
	root        *Node
	mu          sync.Mutex // guards services; activation happens outside it
}

// New returns a kernel seeded with all init()-registered factories.
func New() *Kernel {
	f := make(map[string]regEntry, len(factories))
	idx := make(map[reflect.Type]string)
	for name, e := range factories {
		f[name] = e
		if e.service {
			idx[e.serviceType] = name
		}
	}
	return &Kernel{
		factories:   f,
		services:    make(map[reflect.Type]any),
		serviceIdx:  idx,
		nodesByType: make(map[string][]*Node),
		idIndex:     make(map[string]*Node),
	}
}

// Assemble registers the tree and activates every non-lazy node in two
// phases: Build (instantiate + wire, parent before child) then Run
// (start serving, parent before child). Lazy nodes — service components
// declared via AsService[T], or any node marked lazy in the config
// tree — are registered only and activated on demand through Get[T]()
// or Activate. If Build or Run fails, nodes already built are stopped
// in reverse order so acquired resources (db connections, servers) are
// released.
func (k *Kernel) Assemble(root *Node) error {
	k.root = root
	if err := k.register(root, nil); err != nil {
		return err
	}
	if err := k.checkUniqueIDs(root); err != nil {
		return err
	}
	if err := k.checkServiceUniqueness(); err != nil {
		return err
	}
	var built []*Node
	if err := k.walk(root, func(n *Node) error {
		if n.Lazy || k.factories[n.Type].service {
			return nil
		}
		if err := k.buildNode(n); err != nil {
			return errors.Join(fmt.Errorf("loong: build %q: %w", n.ID, err), k.stopReverse(built))
		}
		built = append(built, n)
		return nil
	}); err != nil {
		return err
	}
	return k.walk(root, func(n *Node) error {
		if n.Lazy || k.factories[n.Type].service {
			return nil
		}
		if err := n.component.Run(&Scope{Kernel: k, Node: n}); err != nil {
			return errors.Join(fmt.Errorf("loong: run %q: %w", n.ID, err), k.stopReverse(built))
		}
		return nil
	})
}

// register walks the tree, filling default ids, setting parent
// pointers, validating component types and building the by-type /
// by-id indexes. Nothing is instantiated here — this phase is cheap
// and always runs fully.
func (k *Kernel) register(n *Node, parent *Node) error {
	n.parent = parent
	if n.ID == "" {
		n.ID = n.Type
	}
	if _, ok := k.factories[n.Type]; !ok {
		return fmt.Errorf("loong: unknown component type %q (node %q)", n.Type, n.ID)
	}
	k.nodesByType[n.Type] = append(k.nodesByType[n.Type], n)
	k.idIndex[n.ID] = n
	for _, c := range n.Children {
		if err := k.register(c, n); err != nil {
			return err
		}
	}
	return nil
}

// checkServiceUniqueness enforces that a service component type is
// mounted exactly once, since service lookup is global per Go type and
// a duplicate would make Get[T]() ambiguous.
func (k *Kernel) checkServiceUniqueness() error {
	for typeName, nodes := range k.nodesByType {
		if k.factories[typeName].service && len(nodes) > 1 {
			return fmt.Errorf("loong: service type %q mounted %d times, want exactly one node", typeName, len(nodes))
		}
	}
	return nil
}

// buildNode instantiates one node and runs its Build phase, then
// registers any service it provides (ServiceProvider). Run is
// deliberately kept separate so assembly can wire the whole tree
// before anything starts serving.
func (k *Kernel) buildNode(n *Node) error {
	n.component = k.factories[n.Type].factory()
	sc := &Scope{Kernel: k, Node: n, Config: n.Config}
	if err := n.component.Build(sc); err != nil {
		return err
	}
	return k.registerService(n)
}

// registerService stores the value returned by a node's Provide() in
// the global service table. Only components that implement
// ServiceProvider are registered; re-registering the same type
// overwrites the previous value with a warning.
//
// The service key is the type declared via AsService[T] when the
// component type registered one (checked by AssignableTo against the
// actual Provide() value, so a mismatch fails the node's activation
// instead of surfacing at lookup time); otherwise it is the dynamic
// type of the provided value (e.g. the web channel exposing Router
// without an AsService declaration).
func (k *Kernel) registerService(n *Node) error {
	sp, ok := n.component.(ServiceProvider)
	if !ok {
		return nil
	}
	svc := sp.Provide()
	if svc == nil {
		return nil
	}
	t := reflect.TypeOf(svc)
	if st := k.factories[n.Type].serviceType; st != nil {
		if !t.AssignableTo(st) {
			return fmt.Errorf("loong: service node %q provides %s, want %s", n.ID, t, st)
		}
		t = st
	}
	k.mu.Lock()
	if _, exists := k.services[t]; exists {
		slog.Warn("loong: service type already provided, overwriting", "type", t.String())
	}
	k.services[t] = svc
	k.mu.Unlock()
	return nil
}

// ensureActive activates a lazy node exactly once, running the full
// lifecycle (Build + Run) outside any kernel-wide lock so component
// code may itself call Get/TryGet for other lazy services without
// deadlocking. The per-node mutex serializes concurrent activations of
// the same node and caches the outcome for later callers.
func (k *Kernel) ensureActive(n *Node) error {
	n.actMu.Lock()
	defer n.actMu.Unlock()
	if n.actDone {
		return n.actErr
	}
	n.component = k.factories[n.Type].factory()
	sc := &Scope{Kernel: k, Node: n, Config: n.Config}
	if err := n.component.Build(sc); err != nil {
		n.actDone, n.actErr = true, err
		return err
	}
	if err := k.registerService(n); err != nil {
		n.actDone, n.actErr = true, err
		return err
	}
	if err := n.component.Run(sc); err != nil {
		n.actDone, n.actErr = true, err
		return err
	}
	n.actDone = true
	return nil
}

// Activate manually activates a lazy node at runtime — useful for a
// component declared lazy in the config tree that is needed on demand
// but does not provide a service. It is idempotent: activating an
// already active node is a no-op returning its cached outcome.
func (k *Kernel) Activate(id string) error {
	k.mu.Lock()
	n := k.idIndex[id]
	k.mu.Unlock()
	if n == nil {
		return fmt.Errorf("loong: unknown node %q", id)
	}
	return k.ensureActive(n)
}

func (k *Kernel) checkUniqueIDs(root *Node) error {
	seen := make(map[string]bool)
	return k.walk(root, func(n *Node) error {
		if seen[n.ID] {
			return fmt.Errorf("loong: duplicate node id %q", n.ID)
		}
		seen[n.ID] = true
		return nil
	})
}

func (k *Kernel) walk(n *Node, fn func(*Node) error) error {
	if err := fn(n); err != nil {
		return err
	}
	for _, c := range n.Children {
		if err := k.walk(c, fn); err != nil {
			return err
		}
	}
	return nil
}

// Shutdown stops the tree in reverse Run order (children before
// parents) so dependents stop before their dependencies. Lazy nodes
// that were never activated have no instance and are skipped. All Stop
// calls are attempted; errors are aggregated. Safe to call before
// Assemble (no-op) or after a failed assembly (stops built nodes).
func (k *Kernel) Shutdown() error {
	if k.root == nil {
		return nil
	}
	var nodes []*Node
	_ = k.walk(k.root, func(n *Node) error {
		nodes = append(nodes, n)
		return nil
	})
	return k.stopReverse(nodes)
}

// stopReverse calls Stop on the given nodes in reverse order and
// aggregates all errors. Nodes without an instance (never activated)
// are skipped.
func (k *Kernel) stopReverse(nodes []*Node) error {
	var errs []error
	for i := len(nodes) - 1; i >= 0; i-- {
		n := nodes[i]
		if n.component == nil {
			continue
		}
		if err := n.component.Stop(&Scope{Kernel: k, Node: n}); err != nil {
			errs = append(errs, fmt.Errorf("loong: stop %q: %w", n.ID, err))
		}
	}
	return errors.Join(errs...)
}

// emit routes an event from a node to its direct parent handler table.
// The root has no parent; unmatched event names are dropped.
func (k *Kernel) emit(from *Node, e Event) error {
	if from.parent == nil {
		return nil
	}
	if h, ok := from.parent.handlers[e.Name]; ok {
		return h(e)
	}
	return nil
}
