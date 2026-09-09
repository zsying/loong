package loong

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// Kernel assembles and runs the component tree in a single process.
type Kernel struct {
	factories   map[string]regEntry
	services    map[reflect.Type]map[string]any // service type -> node id -> value
	serviceIdx  map[reflect.Type]string         // service type -> component type name
	nodesByType map[string][]*Node              // component type name -> registered nodes
	idIndex     map[string]*Node                // node id -> node
	root        *Node
	mu          sync.Mutex // guards services; activation happens outside it
}

// New returns a kernel seeded with all init()-registered factories.
func New() *Kernel {
	f := make(map[string]regEntry, len(factories))
	idx := make(map[reflect.Type]string)
	for name, e := range factories {
		f[name] = e
		for _, sd := range e.services {
			idx[sd.typ] = name
		}
	}
	return &Kernel{
		factories:   f,
		services:    make(map[reflect.Type]map[string]any),
		serviceIdx:  idx,
		nodesByType: make(map[string][]*Node),
		idIndex:     make(map[string]*Node),
	}
}

// Assemble registers the tree and activates every non-lazy node in two
// phases: Build (instantiate + wire, parent before child) then Run
// (start serving, parent before child). A node marked lazy in the
// config tree is registered only and activated on demand through
// Scope.Activate, Scope.Get/GetFrom or Kernel.Activate. If Build or
// Run fails, nodes already built are stopped in reverse order so
// acquired resources (db connections, servers) are released.
func (k *Kernel) Assemble(root *Node) error {
	k.root = root
	if err := k.indexNode(root, nil); err != nil {
		return err
	}
	if err := k.checkUniqueIDs(root); err != nil {
		return err
	}
	var built []*Node
	if err := k.walk(root, func(n *Node) error {
		if n.Lazy {
			return nil
		}
		if n.actDone {
			// Already activated (Built + Run) on demand via ensureActive
			// during another node's Build. Skip the Build re-run here so
			// the component is not instantiated, wired or registered twice;
			// still record it so a later failure stops it in reverse order.
			built = append(built, n)
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
		if n.Lazy {
			return nil
		}
		if n.actDone {
			// Already Run by ensureActive during the Build walk; do not
			// Run it a second time.
			return nil
		}
		if err := n.component.Run(&Scope{Kernel: k, Node: n}); err != nil {
			return errors.Join(fmt.Errorf("loong: run %q: %w", n.ID, err), k.stopReverse(built))
		}
		return nil
	})
}

// indexNode walks the tree, filling default ids, setting parent
// pointers, validating component types and building the by-type /
// by-id indexes. Nothing is instantiated here — this phase is cheap
// and always runs fully.
func (k *Kernel) indexNode(n *Node, parent *Node) error {
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
		if err := k.indexNode(c, n); err != nil {
			return err
		}
	}
	return nil
}

// buildNode instantiates one node and runs its Build phase, then
// registers the services declared via WithService. Run is deliberately
// kept separate so assembly can wire the whole tree before anything
// starts serving.
func (k *Kernel) buildNode(n *Node) error {
	n.component = k.factories[n.Type].factory()
	sc := &Scope{Kernel: k, Node: n, Raw: n.Config}
	if err := n.component.Build(sc); err != nil {
		return err
	}
	return k.registerService(n)
}

// registerService evaluates every WithService accessor declared by the
// node's component type and stores the values in the service table,
// keyed by type and node id, so multiple providers of the same type
// (e.g. two web instances) coexist. A nil value fails the activation —
// the accessor returned before the component was ready.
func (k *Kernel) registerService(n *Node) error {
	decls := k.factories[n.Type].services
	if len(decls) == 0 {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, sd := range decls {
		v := sd.get(n.component)
		if isNilValue(v) {
			return fmt.Errorf("loong: node %q provides nil service for %s", n.ID, sd.typ)
		}
		m := k.services[sd.typ]
		if m == nil {
			m = make(map[string]any)
			k.services[sd.typ] = m
		}
		if _, exists := m[n.ID]; exists {
			// Unreachable today (a node is built exactly once), but fail
			// loudly if the activation bookkeeping ever regresses — a
			// silent overwrite here would corrupt the service table.
			k.mu.Unlock()
			return fmt.Errorf("loong: node %q already provides a service for %s", n.ID, sd.typ)
		}
		m[n.ID] = v
	}
	return nil
}

// ensureActive activates a lazy node exactly once, running the full
// lifecycle (Build + Run) outside any kernel-wide lock so component
// code may itself call Get/GetFrom for other lazy services without
// deadlocking. The per-node mutex serializes concurrent activations of
// the same node and caches the outcome for later callers. args is
// exposed to the component through Scope.Args (nil for startup and
// service activation) — the value a parent passes via Scope.Activate.
func (k *Kernel) ensureActive(n *Node, args any) error {
	n.actMu.Lock()
	defer n.actMu.Unlock()
	if n.actDone {
		return n.actErr
	}
	n.component = k.factories[n.Type].factory()
	sc := &Scope{Kernel: k, Node: n, Raw: n.Config, Args: args}
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
	return k.ensureActive(n, nil)
}

// NodeInfo is the instance-level view of one mounted node, returned
// by Kernel.Node. It powers tooling such as the cli component's
// multi-level command dispatch and help rendering (children recurse).
type NodeInfo struct {
	ID       string
	Type     string
	Lazy     bool
	Children []*NodeInfo // child nodes, in declaration order
}

// Node returns instance metadata for the node with the given id, or
// an error when the id is unknown. It never activates the node.
func (k *Kernel) Node(id string) (*NodeInfo, error) {
	k.mu.Lock()
	n := k.idIndex[id]
	k.mu.Unlock()
	if n == nil {
		return nil, fmt.Errorf("loong: unknown node %q", id)
	}
	return k.nodeInfoOf(n), nil
}

// Component returns the active component instance of the node with
// the given id, or an error when the id is unknown or the node has
// not been activated yet. Tooling (e.g. the cli master's dispatch)
// uses it to reach into a mounted component.
func (k *Kernel) Component(id string) (Component, error) {
	k.mu.Lock()
	n := k.idIndex[id]
	k.mu.Unlock()
	if n == nil {
		return nil, fmt.Errorf("loong: unknown node %q", id)
	}
	if n.component == nil {
		return nil, fmt.Errorf("loong: node %q not activated", id)
	}
	return n.component, nil
}

// Root returns instance metadata for the assembled tree's root node.
func (k *Kernel) Root() (*NodeInfo, error) {
	if k.root == nil {
		return nil, fmt.Errorf("loong: no tree assembled")
	}
	return k.nodeInfoOf(k.root), nil
}

// nodeInfoOf renders a node (and recursively its children) into
// public metadata.
func (k *Kernel) nodeInfoOf(n *Node) *NodeInfo {
	info := &NodeInfo{ID: n.ID, Type: n.Type, Lazy: n.Lazy}
	for _, c := range n.Children {
		info.Children = append(info.Children, k.nodeInfoOf(c))
	}
	return info
}

// nearestProvider returns the closest node on the chain from the given
// node upward (itself included) whose component type declares service
// type t, or nil when the chain holds no such node.
func (k *Kernel) nearestProvider(from *Node, t reflect.Type) *Node {
	for n := from; n != nil; n = n.parent {
		if k.nodeProvides(n, t) {
			return n
		}
	}
	return nil
}

// nodeProvides reports whether the node's component type declares
// service type t via WithService.
func (k *Kernel) nodeProvides(n *Node, t reflect.Type) bool {
	for _, sd := range k.factories[n.Type].services {
		if sd.typ == t {
			return true
		}
	}
	return false
}

// isNilValue reports whether v is nil, including a typed nil boxed
// into an interface (e.g. a nil *T stored in an any).
func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	}
	return false
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
// The root has no parent; unmatched event names are dropped. Routing
// is deliberately single-hop — see Event for the design contract.
func (k *Kernel) emit(from *Node, e Event) error {
	if from.parent == nil {
		return nil
	}
	if h, ok := from.parent.handlers[e.Name]; ok {
		return h(e)
	}
	return nil
}

// emitStrict is the MustEmit backing: an event without a subscriber on
// the direct parent (or emitted at the root, which has no parent) is a
// loud ErrNoSubscriber instead of a silent drop.
func (k *Kernel) emitStrict(from *Node, e Event) error {
	if from.parent == nil {
		return fmt.Errorf("%w: %q emitted at the root", ErrNoSubscriber, e.Name)
	}
	if h, ok := from.parent.handlers[e.Name]; ok {
		return h(e)
	}
	return fmt.Errorf("%w: %q from %q on parent %q", ErrNoSubscriber, e.Name, from.ID, from.parent.ID)
}
