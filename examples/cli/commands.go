package main

import (
	"errors"
	"fmt"

	"github.com/zsying/loong"
	"github.com/zsying/loong/components/cli"
)

// Greet is a business command component: it reads the command context
// from the cli master (parent chain) and prints a greeting. It shows
// how a project's own command components plug into the same tree.
type Greet struct {
	loong.Base
}

func (g *Greet) Run(ctx *loong.Scope) error {
	cctx := ctx.Get[*cli.Context]()
	if cctx == nil {
		return errors.New("cli: context not mounted")
	}
	pos := cctx.Positional()
	name := "world"
	if len(pos) > 0 {
		name = pos[0]
	}
	if cctx.HasFlag("loud") {
		fmt.Fprintf(cctx.Out, "HELLO, %s!\n", name)
		return nil
	}
	fmt.Fprintf(cctx.Out, "hello, %s\n", name)
	return nil
}

func init() {
	loong.RegisterComponent("demo.greet", func() loong.Component { return &Greet{} },
		loong.WithDesc("demo business command: greet [name] [--loud]"),
	)
}

// ConfigShow prints the value of a demo config key (multi-level
// command demo: `config show <key>`).
type ConfigShow struct {
	loong.Base
}

func (c *ConfigShow) Run(ctx *loong.Scope) error {
	cctx := ctx.Get[*cli.Context]()
	if cctx == nil {
		return errors.New("cli: context not mounted")
	}
	pos := cctx.Positional()
	if len(pos) == 0 {
		return fmt.Errorf("config show: missing key (usage: config show <key>)")
	}
	// Demo store: echo back a placeholder value.
	fmt.Fprintf(cctx.Out, "%s = (unset)\n", pos[0])
	return nil
}

// ConfigSet sets a demo config key (multi-level command demo:
// `config set <key> <value>`).
type ConfigSet struct {
	loong.Base
}

func (c *ConfigSet) Run(ctx *loong.Scope) error {
	cctx := ctx.Get[*cli.Context]()
	if cctx == nil {
		return errors.New("cli: context not mounted")
	}
	pos := cctx.Positional()
	if len(pos) < 2 {
		return fmt.Errorf("config set: usage: config set <key> <value>")
	}
	fmt.Fprintf(cctx.Out, "%s = %s (saved)\n", pos[0], pos[1])
	return nil
}

func init() {
	loong.RegisterComponent("demo.config.show", func() loong.Component { return &ConfigShow{} },
		loong.WithDesc("demo multi-level command: show a config key"),
	)
	loong.RegisterComponent("demo.config.set", func() loong.Component { return &ConfigSet{} },
		loong.WithDesc("demo multi-level command: set a config key"),
	)
}
