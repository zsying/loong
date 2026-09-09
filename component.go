package loong

import (
	"bytes"
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
// library context.Context. Raw is the opaque config node; components
// decode it into their own struct via Config[T] (schema-per-component).
// Args holds the value passed to Scope.Activate when this node was
// activated by its parent (nil for startup or service activation) —
// the mechanism by which a parent defines how its children activate.
type Scope struct {
	Kernel *Kernel
	Node   *Node
	Raw    yaml.Node
	Args   any
}

// Activate activates one of this node's direct children by id,
// passing args to it through the child's Scope.Args. It is the
// parent-defined activation primitive: the parent decides which child
// runs and what it receives (e.g. a CLI master activating the command
// named by the first argument). The child id must be a direct child;
// returns an error otherwise. Activation stays idempotent per node —
// args apply on first activation.
func (s *Scope) Activate(id string, args any) error {
	for _, c := range s.Node.Children {
		if c.ID == id {
			return s.Kernel.ensureActive(c, args)
		}
	}
	return fmt.Errorf("loong: node %q has no child %q", s.Node.ID, id)
}

// Emit sends an event upward to the direct parent node.
// The component never holds a reference to its parent. Routing is
// single-hop and lenient: when the parent has no handler for the name
// the event is silently dropped (see Event for the design contract) —
// the right behavior for notifications. For hook-style events whose
// missing subscriber is a bug, use MustEmit.
func (s *Scope) Emit(name string, payload any) error {
	return s.Kernel.emit(s.Node, Event{Name: name, Source: s.Node.ID, Payload: payload})
}

// MustEmit is the strict Emit: it fails with ErrNoSubscriber when the
// direct parent has no handler for the event name (or when the node is
// the root, which has no parent to receive events at all). Use it for
// hook-style events — a master asking its app-level parent to apply
// global flags, for instance — where a silently dropped event would
// surface later as a baffling no-op.
func (s *Scope) MustEmit(name string, payload any) error {
	return s.Kernel.emitStrict(s.Node, Event{Name: name, Source: s.Node.ID, Payload: payload})
}

// On subscribes to an event name emitted by any child node.
// Subscription happens during Build, after the node is created but
// before Run, so handlers are in place before any event fires.
// Unregistered events are silently dropped by Emit; MustEmit turns
// that drop into an error for hook-style events.
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

// Config decodes the node's config block into T and returns it. An
// absent config block yields the zero value. Decoding is strict: keys
// not present in T's yaml tags fail with an error naming the unknown
// field, so typos in the config tree surface at activation instead of
// being silently ignored. Errors carry the node id for context.
func (s *Scope) Config[T any]() (T, error) {
	var cfg T
	if s.Raw.IsZero() {
		return cfg, nil
	}
	data, err := yaml.Marshal(&s.Raw)
	if err != nil {
		return cfg, fmt.Errorf("loong: re-encode config for node %q: %w", s.Node.ID, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("loong: decode config for node %q: %w", s.Node.ID, err)
	}
	return cfg, nil
}
