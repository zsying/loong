package loong

import (
	"fmt"
	"reflect"
)

// lookup resolves a service of type T.
//
// With a non-empty id it targets that exact node, activating it on
// demand when needed. Without an id it returns the sole provider; when
// several providers exist it returns the closest one on the chain from
// the given node upward (the node itself included), following the
// parent-child convention that reused components are mounted under the
// provider they belong to. It reports an error when no provider
// exists, when the lookup is ambiguous and no ancestor matches, or
// when on-demand activation fails.
func (k *Kernel) lookup[T any](from *Node, id string) (T, error) {
	var zero T
	t := reflect.TypeOf((*T)(nil)).Elem()

	k.mu.Lock()
	provided := k.services[t] // nil or map[node id]value, active providers

	if id != "" {
		if v, ok := provided[id]; ok {
			k.mu.Unlock()
			return v.(T), nil
		}
		n := k.idIndex[id]
		if n == nil {
			k.mu.Unlock()
			return zero, fmt.Errorf("loong: unknown node %q", id)
		}
		if !k.nodeProvides(n, t) {
			k.mu.Unlock()
			return zero, fmt.Errorf("loong: node %q does not provide %s", id, t)
		}
		k.mu.Unlock()
		if err := k.ensureActive(n); err != nil {
			return zero, fmt.Errorf("loong: activate service node %q: %w", n.ID, err)
		}
		k.mu.Lock()
		v, ok := k.services[t][id]
		k.mu.Unlock()
		if !ok {
			return zero, fmt.Errorf("loong: node %q provides nothing of type %s", id, t)
		}
		return v.(T), nil
	}

	// No id: the sole active provider wins outright.
	if len(provided) == 1 {
		for _, v := range provided {
			k.mu.Unlock()
			return v.(T), nil
		}
	}
	// Several active providers: defer to the nearest ancestor.
	if len(provided) > 1 {
		if n := k.nearestProvider(from, t); n != nil {
			k.mu.Unlock()
			return provided[n.ID].(T), nil
		}
		k.mu.Unlock()
		return zero, k.ambiguousErr(t)
	}

	// Nothing active yet: lazily activate a declared provider.
	typeName, ok := k.serviceIdx[t]
	if !ok {
		k.mu.Unlock()
		return zero, fmt.Errorf("loong: no service of type %s", t)
	}
	candidates := k.nodesByType[typeName]
	if len(candidates) == 0 {
		k.mu.Unlock()
		return zero, fmt.Errorf("loong: no node of service type %q", typeName)
	}
	var target *Node
	if len(candidates) == 1 {
		target = candidates[0]
	} else {
		target = k.nearestProvider(from, t)
		if target == nil {
			k.mu.Unlock()
			return zero, k.ambiguousErr(t)
		}
	}
	k.mu.Unlock()

	if err := k.ensureActive(target); err != nil {
		return zero, fmt.Errorf("loong: activate service node %q: %w", target.ID, err)
	}
	k.mu.Lock()
	v, ok := k.services[t][target.ID]
	k.mu.Unlock()
	if !ok {
		return zero, fmt.Errorf("loong: service node %q provides nothing of type %s", target.ID, t)
	}
	return v.(T), nil
}

func (k *Kernel) ambiguousErr(t reflect.Type) error {
	return fmt.Errorf("loong: service %s has multiple providers; use GetFrom[T](id) to disambiguate", t)
}

// Get returns the best service match of type T for this node: the sole
// provider if unique, or the closest provider on the node's parent
// chain (itself included) when several exist. Returns the zero value
// when nothing matches (see TryGet for error reporting). The Go type
// itself is the service key — no string indirection.
func (s *Scope) Get[T any]() T {
	v, _ := s.TryGet[T]()
	return v
}

// TryGet is Get with error reporting: no provider, ambiguous lookup
// with no matching ancestor, or on-demand activation failure.
func (s *Scope) TryGet[T any]() (T, error) {
	return s.Kernel.lookup[T](s.Node, "")
}

// GetFrom returns the service of type T provided by the node with the
// given id, activating that node on demand if needed. It returns the
// zero value when the node is unknown or provides no such service (see
// TryGetFrom for errors).
func (s *Scope) GetFrom[T any](id string) T {
	v, _ := s.TryGetFrom[T](id)
	return v
}

// TryGetFrom is GetFrom with error reporting.
func (s *Scope) TryGetFrom[T any](id string) (T, error) {
	return s.Kernel.lookup[T](s.Node, id)
}
