package loong

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Component is the three-phase lifecycle interface every loong
// component implements: Build (decode config, wire dependencies,
// subscribe to child events), Run (start serving),
// Stop (graceful shutdown, called in reverse Run order).
// Most components embed Base instead of implementing all phases.
type Component interface {
	Build(ctx *Scope) error
	Run(ctx *Scope) error
	Stop(ctx *Scope) error
}

// Scope carries the node and its raw config block during Build/Run.
// Named Scope (not Context) to avoid clashing with the standard
// library context.Context. Config is opaque to the kernel; the
// component decodes it into its own struct (schema-per-component).
type Scope struct {
	Kernel *Kernel
	Node   *Node
	Config yaml.Node
}

// Emit sends an event upward to the direct parent node.
// The component never holds a reference to its parent.
func (s *Scope) Emit(name string, payload any) error {
	return s.Kernel.emit(s.Node, Event{Name: name, Source: s.Node.ID, Payload: payload})
}

// On subscribes to an event name emitted by any child node.
// Subscription happens during Build, after the node is created but
// before Run, so handlers are in place before any event fires.
// Unregistered events are silently dropped.
func (s *Scope) On(name string, h Handler) {
	if s.Node.handlers == nil {
		s.Node.handlers = make(map[string]Handler)
	}
	s.Node.handlers[name] = h
}

// OnTyped subscribes to an event and type-asserts its payload into T,
// giving subscribers a compile-time-typed handler while the kernel
// keeps Event.Payload opaque. Events are dispatched by string name, so
// generics cannot reach into the routing table itself; this wrapper
// moves the assertion to the subscription side where the type is known.
// It returns an error from the wrapped handler when the payload type
// does not match T. Declared as a method on Scope (generic methods are
// supported from Go 1.27) so callers write scope.OnTyped[T](name, h).
func (s *Scope) OnTyped[T any](name string, h func(T) error) {
	s.On(name, func(e Event) error {
		p, ok := e.Payload.(T)
		if !ok {
			return fmt.Errorf("loong: event %q payload is %T, want %T", e.Name, e.Payload, *new(T))
		}
		return h(p)
	})
}