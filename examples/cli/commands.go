package main

import (
	"fmt"
	"log/slog"

	"github.com/zsying/loong"
	"github.com/zsying/loong/components/cli"
)

// Greet is a business command component. It reads its arguments from the
// *cli.Args payload — injected by the cli master via scope.Activate — and
// prints a greeting, logging through the mounted log component. This
// is how a project's own command components plug into the tree: no
// dispatch or argument plumbing to write.
type Greet struct {
	loong.Base
}

func (g *Greet) Run(ctx *loong.Scope) error {
	args, err := cli.ArgsOf(ctx)
	if err != nil {
		return err
	}
	name := "world"
	if first, ok := args.Arg(0); ok {
		name = first
	}
	loud := args.Bool("l", "loud")
	slog.Info("greet", "name", name, "loud", loud)
	if loud {
		fmt.Printf("HELLO, %s!\n", name)
		return nil
	}
	fmt.Printf("hello, %s\n", name)
	return nil
}

// ConfigShow prints a demo config key (multi-level command demo:
// `config show <key>` — the group forwards the leaf args).
type ConfigShow struct {
	loong.Base
}

func (c *ConfigShow) Run(ctx *loong.Scope) error {
	args, err := cli.ArgsOf(ctx)
	if err != nil {
		return err
	}
	key, ok := args.Arg(0)
	if !ok {
		return fmt.Errorf("config show: missing key (usage: config show <key>)")
	}
	fmt.Printf("%s = (unset)\n", key)
	return nil
}

// ConfigSet sets a demo config key (multi-level command demo:
// `config set <key> <value>`).
type ConfigSet struct {
	loong.Base
}

func (c *ConfigSet) Run(ctx *loong.Scope) error {
	args, err := cli.ArgsOf(ctx)
	if err != nil {
		return err
	}
	key, ok := args.Arg(0)
	if !ok {
		return fmt.Errorf("config set: usage: config set <key> <value>")
	}
	value, ok := args.Arg(1)
	if !ok {
		return fmt.Errorf("config set: usage: config set <key> <value>")
	}
	fmt.Printf("%s = %s (saved)\n", key, value)
	return nil
}

func init() {
	loong.RegisterComponent("demo.greet", func() loong.Component { return &Greet{} },
		loong.WithDesc("demo business command: greet [name] [--loud|-l]"),
	)

	loong.RegisterComponent("demo.config.show", func() loong.Component { return &ConfigShow{} },
		loong.WithDesc("demo multi-level command: show a config key"),
	)
	loong.RegisterComponent("demo.config.set", func() loong.Component { return &ConfigSet{} },
		loong.WithDesc("demo multi-level command: set a config key"),
	)
}
