// Package cli provides the CLI building blocks for loong applications.
// It follows the same pattern as the web channel: a cli master
// component (type "cli", eager) exposes the command *Context as a
// service, and subcommand components (cli.list, cli.new, ...) mount
// under it as lazy nodes. Any loong application can mount these
// components and get a component-driven CLI; the master + subcommand
// shape is the template for building custom CLI applications.
//
// The package-level Go function is the one-line entry point for a
// standalone CLI: parse an embedded config tree, assemble, dispatch
// the command path from os.Args, shut down, and return the exit code.
package cli

import (
	"log/slog"
	"os"

	"github.com/zsying/loong"
)

// Component is the cli master: it builds the command Context from
// os.Args and exposes it as a service for mounted subcommands. Like
// the web channel it is eager — running at assembly — so its service
// is available to subcommands activated later.
type Component struct {
	loong.Base
	Ctx *Context
}

func (c *Component) Build(scope *loong.Scope) error {
	// The first argument is the command name, which the subcommand does
	// not need — it knows its own identity. Skipping it keeps the plain
	// standard-API path (Assemble + Activate) working with the same
	// Context semantics as cli.Go, whose dispatch overwrites the args
	// with the leaf command's own arguments anyway.
	c.Ctx = NewContext(os.Args[2:], os.Stdout, os.Stderr)
	return nil
}

func init() {
	loong.RegisterComponent("cli", func() loong.Component { return &Component{} },
		loong.WithService(func(c loong.Component) *Context { return c.(*Component).Ctx }),
		loong.Eager(), // master runs at assembly so Context is ready
		loong.WithDesc("CLI master: command context and subcommand mount point"),
	)
}

// Go runs a standalone CLI application from an embedded config tree:
// parse, assemble, dispatch the command path (os.Args[1:]) by
// activating the matching lazy nodes, shut down, and return the exit
// code. Command components and the cli master must be registered —
// import this package and any custom command component packages.
//
//	//go:embed cli.yaml
//	var cliYAML []byte
//
//	func main() { os.Exit(cli.Go(cliYAML)) }
func Go(yamlData []byte) int {
	root, err := loong.Parse(yamlData)
	if err != nil {
		slog.Error("parse config", "err", err)
		return 1
	}
	k := loong.New()
	if err := k.Assemble(root); err != nil {
		slog.Error("assemble", "err", err)
		return 1
	}
	code := dispatch(k, os.Args[1:])
	if err := k.Shutdown(); err != nil {
		slog.Error("shutdown", "err", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}
