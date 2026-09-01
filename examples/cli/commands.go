package main

import (
	"fmt"
	"log/slog"

	"github.com/zsying/loong"
	"github.com/zsying/loong/components/cli"
)

// Greet is a business command component. It reads its arguments from
// scope.Args — injected by the cli master via scope.Activate — and
// prints a greeting, logging through the mounted log component. This
// is how a project's own command components plug into the tree: no
// dispatch or argument plumbing to write.
type Greet struct {
	loong.Base
}

func (g *Greet) Run(ctx *loong.Scope) error {
	args, _ := ctx.Args.([]string)
	name := "world"
	if pos := cli.Positional(args); len(pos) > 0 {
		name = pos[0]
	}
	loud := cli.HasFlag(args, "loud")
	slog.Info("greet", "name", name, "loud", loud)
	if loud {
		fmt.Printf("HELLO, %s!\n", name)
		return nil
	}
	fmt.Printf("hello, %s\n", name)
	return nil
}

func init() {
	loong.RegisterComponent("demo.greet", func() loong.Component { return &Greet{} },
		loong.WithDesc("demo business command: greet [name] [--loud]"),
	)
}

// ConfigShow prints a demo config key (multi-level command demo:
// `config show <key>` — the group forwards the leaf args).
type ConfigShow struct {
	loong.Base
}

func (c *ConfigShow) Run(ctx *loong.Scope) error {
	args, _ := ctx.Args.([]string)
	pos := cli.Positional(args)
	if len(pos) == 0 {
		return fmt.Errorf("config show: missing key (usage: config show <key>)")
	}
	fmt.Printf("%s = (unset)\n", pos[0])
	return nil
}

// ConfigSet sets a demo config key (multi-level command demo:
// `config set <key> <value>`).
type ConfigSet struct {
	loong.Base
}

func (c *ConfigSet) Run(ctx *loong.Scope) error {
	args, _ := ctx.Args.([]string)
	pos := cli.Positional(args)
	if len(pos) < 2 {
		return fmt.Errorf("config set: usage: config set <key> <value>")
	}
	fmt.Printf("%s = %s (saved)\n", pos[0], pos[1])
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
