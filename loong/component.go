package loong

import "gopkg.in/yaml.v3"

// Component is the four-phase lifecycle interface every loong
// component implements: Register (declare event subscriptions),
// Build (decode config, wire dependencies), Run (start serving),
// Stop (graceful shutdown, called in reverse Run order).
// Most components embed Base instead of implementing all phases.
type Component interface {
	Register(reg *Registry) error
	Build(ctx *Scope) error
	Run(ctx *Scope) error
	Stop(ctx *Scope) error
}

// Registry is the handle available during the Register phase.
// A component uses it to subscribe to events emitted by its children.
type Registry struct {
	kernel *Kernel
	node   *Node
}

// On subscribes to an event name emitted by any child node.
// Unregistered events are silently dropped.
func (r *Registry) On(name string, h Handler) {
	if r.node.handlers == nil {
		r.node.handlers = make(map[string]Handler)
	}
	r.node.handlers[name] = h
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
