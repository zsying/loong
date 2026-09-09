// Package loong provides the loong platform kernel: the component tree
// model, three-phase lifecycle (Build/Run/Stop), event routing and
// type-based service lookup. The kernel lives at the repository root,
// so importing the platform means importing the loong package itself.
package loong

import (
	"errors"
)

// Event is emitted by a child node and routed to its direct parent.
// Payload is opaque: the parent component decodes it itself,
// following the same schema-per-component philosophy as the config tree.
//
// Routing is deliberately single-hop: an event is visible only to the
// emitting node's direct parent, and unmatched event names are
// dropped. There is no bubbling — a grandparent never sees a child's
// event — preserving the locality contract that a subtree's events
// belong to its owner. A parent that needs to relay upward Emits its
// own event, explicitly. Emit (silent drop on no subscriber) suits
// notifications; MustEmit (error on no subscriber) suits hooks whose
// absence is a bug.
type Event struct {
	Name    string
	Source  string
	Payload any
}

// Handler processes an event received from a child node.
type Handler func(e Event) error

// ErrNoSubscriber is returned by MustEmit when no handler is
// subscribed to the event on the emitting node's direct parent (or the
// node is the root, which has no parent at all). Use it to turn the
// silent drop of Emit into a loud failure for hook-style events.
var ErrNoSubscriber = errors.New("loong: event has no subscriber")
