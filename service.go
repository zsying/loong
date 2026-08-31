package loong

import (
	"fmt"
	"reflect"
)

// ServiceProvider is an optional interface. When a component instance
// implements it, the kernel calls Provide() right after the node's
// Build succeeds and registers the returned value as a service keyed
// by its Go type, making it available to Kernel.Get[T]() and
// Kernel.TryGet[T](). A component type registered with the AsService[T]
// option is activated lazily: its node is instantiated, built and run
// on the first lookup instead of during assembly. A non-lazy component
// (e.g. the web channel) can still implement ServiceProvider — its
// capability becomes visible to lookups as soon as it is built.
type ServiceProvider interface {
	Provide() any
}

// TryGet returns the service registered for type T. If T was declared
// via AsService[T]() and its node is not active yet, the node is
// activated on demand — instantiated, built and run — and its provided
// value is returned. It reports an error when no such service exists,
// the node is unknown, or on-demand activation fails.
func (k *Kernel) TryGet[T any]() (T, error) {
	var zero T
	t := reflect.TypeOf((*T)(nil)).Elem()

	k.mu.Lock()
	if v, ok := k.services[t]; ok {
		k.mu.Unlock()
		return v.(T), nil
	}
	typeName, ok := k.serviceIdx[t]
	if !ok {
		k.mu.Unlock()
		return zero, fmt.Errorf("loong: no service of type %s", t)
	}
	nodes := k.nodesByType[typeName]
	if len(nodes) == 0 {
		k.mu.Unlock()
		return zero, fmt.Errorf("loong: no node of service type %q", typeName)
	}
	n := nodes[0]
	k.mu.Unlock()

	if err := k.ensureActive(n); err != nil {
		return zero, fmt.Errorf("loong: activate service node %q: %w", n.ID, err)
	}

	k.mu.Lock()
	v, ok := k.services[t]
	k.mu.Unlock()
	if !ok {
		return zero, fmt.Errorf("loong: service node %q active but provides nothing of type %s", n.ID, t)
	}
	return v.(T), nil
}

// Get returns the service registered for type T, or the zero value if
// no such service is provided (activation failures and unknown types
// are silent; use TryGet for error reporting). The Go type itself is
// the service key — no string indirection. Declared as a method on
// Kernel (generic methods are supported from Go 1.27); call k.Get[T]().
func (k *Kernel) Get[T any]() T {
	v, _ := k.TryGet[T]()
	return v
}
