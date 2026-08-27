package loong

import "log/slog"

// Base is an embeddable skeleton component. Embedding it satisfies
// the Component interface with no-op phases and provides common
// helpers, so a component only overrides the phases it cares about.
//
//	type Greet struct {
//	    Base
//	}
//
// Components that override Build should set b.Scope = ctx (or call
// b.Base.Build(ctx)) so Base helpers work.
type Base struct {
	Scope *Scope
}

func (b *Base) Register(*Registry) error { return nil }
func (b *Base) Build(ctx *Scope) error   { b.Scope = ctx; return nil }
func (b *Base) Run(*Scope) error         { return nil }
func (b *Base) Stop(*Scope) error        { return nil }

// Emit sends an event upward to the direct parent node.
func (b *Base) Emit(name string, payload any) error {
	return b.Scope.Emit(name, payload)
}

// Logger returns a slog logger tagged with the component id.
func (b *Base) Logger() *slog.Logger {
	id := "?"
	if b.Scope != nil && b.Scope.Node != nil {
		id = b.Scope.Node.ID
	}
	return slog.With("component", id)
}
