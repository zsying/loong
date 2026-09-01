// Package cli provides the CLI building blocks for loong applications.
// It follows the same pattern as the web channel: a cli master
// component (type "cli", eager) parses the command line and activates
// the matching child command, and subcommand components (cli.list,
// cli.new, ...) mount under it as lazy nodes. The activation chain is
// pure framework — every level calls scope.Activate(child, args),
// passing arguments through scope.Args — so no dispatch logic lives
// outside the components. Any loong application can mount these
// components and get a component-driven CLI.
//
// The package-level Go function is the one-line entry point for a
// standalone CLI: parse an embedded config tree, assemble (which runs
// the command), shut down, and return the exit code.
package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/zsying/loong"
)

// ErrUsage marks a command-line usage error (unknown or incomplete
// invocation); callers map it to exit code 2.
var ErrUsage = errors.New("cli: usage error")

// Component is the cli master: it parses os.Args and activates the
// child command named by the first argument, passing the rest as the
// command's arguments via Scope.Activate.
type Component struct {
	loong.Base
}

func (c *Component) Run(ctx *loong.Scope) error {
	args := os.Args[1:]
	if len(args) == 0 {
		return usage(ctx)
	}
	return ctx.Activate(args[0], args[1:])
}

func init() {
	loong.RegisterComponent("cli", func() loong.Component { return &Component{} },
		loong.Eager(), // master runs at assembly so the command fires
		loong.WithDesc("CLI master: parses the command line and activates the command"),
	)
}

// usage prints the command tree under the master and returns ErrUsage.
func usage(ctx *loong.Scope) error {
	fmt.Println("usage: <command>")
	printCommands(ctx.Node.Children, 1)
	return ErrUsage
}

// printCommands renders a node's children with indentation, marking
// command groups with their subcommands.
func printCommands(cmds []*loong.Node, depth int) {
	indent := strings.Repeat("  ", depth)
	for _, c := range cmds {
		if len(c.Children) > 0 {
			fmt.Printf("%s%s <%s>\n", indent, c.ID, groupNames(c.Children))
			continue
		}
		fmt.Printf("%s%s\n", indent, c.ID)
	}
}

func groupNames(children []*loong.Node) string {
	names := make([]string, 0, len(children))
	for _, c := range children {
		names = append(names, c.ID)
	}
	return strings.Join(names, ", ")
}

// Go runs a standalone CLI application from an embedded config tree:
// parse, assemble (the eager master activates the command during
// assembly), shut down, and return the exit code. Command components
// and the cli master must be registered — import this package and any
// custom command component packages.
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
		// Assembly is where the command runs (master Run activates it);
		// an error here is the command's outcome.
		_ = k.Shutdown()
		if errors.Is(err, ErrUsage) {
			return 2
		}
		slog.Error("command failed", "err", err)
		return 1
	}
	if err := k.Shutdown(); err != nil {
		slog.Error("shutdown", "err", err)
		return 1
	}
	return 0
}
