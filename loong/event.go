// Package loong provides the loong platform kernel: the component tree
// model, three-phase lifecycle, event routing and type-based service
// lookup. The kernel lives at the repository root, so importing the
// platform means importing the loong package itself.
package loong

// Event is emitted by a child node and routed to its direct parent.
// Payload is opaque: the parent component decodes it itself,
// following the same schema-per-component philosophy as the config tree.
type Event struct {
	Name    string
	Source  string
	Payload any
}

// Handler processes an event received from a child node.
type Handler func(e Event) error
