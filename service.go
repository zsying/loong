package loong

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
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
func (k *Kernel) lookup[T any](from *Node, id string) (out T, err error) {
	var zero T
	t := reflect.TypeOf((*T)(nil)).Elem()

	// A resolution that returned a value is a dependency: record who
	// asked, so the tree can say who uses what (Consumers). A lookup that
	// found nothing is not one, so it is deliberately not recorded.
	//
	// This runs after the return value is set, and every path below
	// releases k.mu before returning, so taking it again here is safe.
	defer func() {
		if err == nil {
			k.noteConsumer(from, t)
		}
	}()

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
		if err := k.ensureActive(n, nil); err != nil {
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

	// Nothing active yet: activate a declared provider on demand. The
	// choice is made exactly as it is among active providers — the sole
	// candidate, otherwise the closest one on the chain from the caller
	// upward. Which component type declared the service is irrelevant;
	// candidates are the mounted nodes, whatever declared them.
	candidates := k.providersOf(t)
	if len(candidates) == 0 {
		k.mu.Unlock()
		return zero, k.noProviderErr(t)
	}
	var target *Node
	if len(candidates) == 1 {
		target = candidates[0]
	} else if target = k.nearestProvider(from, t); target == nil {
		k.mu.Unlock()
		return zero, k.ambiguousErr(t)
	}
	k.mu.Unlock()

	if err := k.ensureActive(target, nil); err != nil {
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

// noProviderErr explains an empty lookup. The two cases are different
// mistakes: nothing at all declares the service, or the component types
// that do have no node mounted in this tree — the second is a tree that
// forgot a provider, so the message names what could be mounted.
func (k *Kernel) noProviderErr(t reflect.Type) error {
	var types []string
	for name, e := range k.factories {
		for _, sd := range e.services {
			if sd.typ == t {
				types = append(types, name)
				break
			}
		}
	}
	if len(types) == 0 {
		return fmt.Errorf("loong: no service of type %s", t)
	}
	sort.Strings(types)
	return fmt.Errorf("loong: no node provides %s; component types declaring it: %s",
		t, strings.Join(types, ", "))
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

// Providers returns the ids of the nodes that can serve T — exactly the
// ids GetFrom accepts — in tree declaration order. It is the discovery
// half of GetFrom: with two providers of the same type mounted (two web
// instances, say), this is how a caller learns what there is to name.
//
// The answer describes the tree, not the runtime: a lazy node is listed
// before anything has activated it, and asking activates nothing. An
// empty result means no mounted node declares T; that is an answer, not
// an error — TryGet is where a lookup reports it.
func (k *Kernel) Providers[T any]() []string {
	t := reflect.TypeOf((*T)(nil)).Elem()
	k.mu.Lock()
	defer k.mu.Unlock()
	var ids []string
	for _, n := range k.providersOf(t) {
		ids = append(ids, n.ID)
	}
	return ids
}

// Consumers returns the ids of the nodes that have resolved a service
// of type T, in tree order — the mirror of Providers, which lists who
// can serve it. The two are the halves of a service dependency: what
// the tree declares, and what the run actually used.
//
// It is a trace, not a declaration. A node appears once it has really
// resolved T, so a lazy node that was never needed is absent, and a
// lookup that found nothing records nothing: the answer describes how
// far the program got, which is what makes it useful for diagnosing a
// wiring mistake and unusable as a catalogue (Providers is that).
func (k *Kernel) Consumers[T any]() []string {
	t := reflect.TypeOf((*T)(nil)).Elem()
	k.mu.Lock()
	defer k.mu.Unlock()
	seen := k.consumers[t]
	if len(seen) == 0 {
		return nil
	}
	var out []string
	if k.root != nil {
		_ = k.walk(k.root, func(n *Node) error {
			if seen[n.ID] {
				out = append(out, n.ID)
			}
			return nil
		})
	}
	return out
}
