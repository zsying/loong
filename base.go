package loong

import (
	"fmt"
	"log/slog"
)

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

func (b *Base) Build(scope *Scope) error { b.Scope = scope; return nil }
func (b *Base) Run(*Scope) error         { return nil }
func (b *Base) Stop(*Scope) error        { return nil }

// Emit sends an event upward to the direct parent node.
// It returns an error instead of panicking when the component
// overrode Build without setting b.Scope.
func (b *Base) Emit(name string, payload any) error {
	if b.Scope == nil {
		return fmt.Errorf("loong: Base.Scope is nil; call b.Base.Build(ctx) in your Build override")
	}
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

// Container is the generic root/container component: it embeds Base
// and has no behavior beyond the no-op lifecycle. It is registered as
// type "base" (see the package init below), so a config tree root
// needs no custom component when it carries no logic of its own —
// `type: base` in the yaml replaces the boilerplate "type app struct
// { loong.Base }" every project used to declare.
type Container struct {
	Base
}

func init() {
	RegisterComponent("base", func() Component { return &Container{} },
		WithDesc("generic container/root with a no-op lifecycle"),
	)
}

// Func is a Component implemented by a single Run function. Build and
// Stop are no-ops, so it suits command nodes that act only when run and
// need no build-time wiring or teardown — the loong analogue of a React
// function component: the component is just what it does.
//
//	loong.RegisterFunc("owlet.version", func(ctx *loong.Scope) error {
//	    return printVersion()
//	}, loong.WithDesc("print version information"))
//
// The run function receives the scope directly, so it can call
// ctx.Config / ctx.Emit / ctx.Activate without holding a Base. Because
// Func is stateless, RegisterFunc's factory returns a fresh value per
// node, which is safe for several nodes of the same type. Components
// that decode config or register services at build time keep a struct
// embedding Base.
type Func func(ctx *Scope) error

func (f Func) Build(*Scope) error { return nil }
func (f Func) Run(ctx *Scope) error { return f(ctx) }
func (f Func) Stop(*Scope) error   { return nil }
