package loong

import (
	"errors"
	"fmt"
	"log/slog"
	"reflect"
)

// Kernel assembles and runs the component tree in a single process.
type Kernel struct {
	factories map[string]func() Component
	services  map[reflect.Type]any
	root      *Node
}

// New returns a kernel seeded with all init()-registered factories.
func New() *Kernel {
	f := make(map[string]func() Component, len(factories))
	for name, fn := range factories {
		f[name] = fn
	}
	return &Kernel{factories: f, services: make(map[reflect.Type]any)}
}

// Provide registers a service instance, keyed by its Go type.
// Re-registering the same type (e.g. when a component type is mounted
// multiple times) overwrites the previous value with a warning; the
// convention is that only one instance of a type provides services.
func (k *Kernel) Provide(service any) *Kernel {
	t := reflect.TypeOf(service)
	if _, exists := k.services[t]; exists {
		slog.Warn("loong: service type already provided, overwriting", "type", t.String())
	}
	k.services[t] = service
	return k
}

// Assemble instantiates the tree from a loaded config root and runs
// the four phases over the whole tree in order: Register, Build, Run.
// Run is intentionally last so every component is fully built before
// any of them starts serving (wire first, fire later). If Build or Run
// fails, components already built are stopped in reverse order so
// acquired resources (db connections, servers) are released.
func (k *Kernel) Assemble(root *Node) error {
	k.root = root
	if err := k.instantiate(root, nil); err != nil {
		return err
	}
	if err := k.checkUniqueIDs(root); err != nil {
		return err
	}
	if err := k.walk(root, func(n *Node) error {
		if err := n.component.Register(&Registry{kernel: k, node: n}); err != nil {
			return fmt.Errorf("loong: register %q: %w", n.ID, err)
		}
		return nil
	}); err != nil {
		return err
	}
	var built []*Node
	if err := k.walk(root, func(n *Node) error {
		if err := n.component.Build(&Scope{Kernel: k, Node: n, Config: n.Config}); err != nil {
			return errors.Join(fmt.Errorf("loong: build %q: %w", n.ID, err), k.stopReverse(built))
		}
		built = append(built, n)
		return nil
	}); err != nil {
		return err
	}
	return k.walk(root, func(n *Node) error {
		if err := n.component.Run(&Scope{Kernel: k, Node: n}); err != nil {
			return errors.Join(fmt.Errorf("loong: run %q: %w", n.ID, err), k.stopReverse(built))
		}
		return nil
	})
}

func (k *Kernel) instantiate(n *Node, parent *Node) error {
	n.parent = parent
	if n.ID == "" {
		n.ID = n.Type
	}
	f, ok := k.factories[n.Type]
	if !ok {
		return fmt.Errorf("loong: unknown component type %q (node %q)", n.Type, n.ID)
	}
	n.component = f()
	for _, c := range n.Children {
		if err := k.instantiate(c, n); err != nil {
			return err
		}
	}
	return nil
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
// parents) so dependents stop before their dependencies. All Stop
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
// aggregates all errors.
func (k *Kernel) stopReverse(nodes []*Node) error {
	var errs []error
	for i := len(nodes) - 1; i >= 0; i-- {
		n := nodes[i]
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
