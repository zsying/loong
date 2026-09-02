// Package cli provides the CLI building blocks for loong applications.
// It follows the same pattern as the web channel: a cli master
// component (type "cli", activated at assembly like any non-lazy
// node) parses the command line and activates the matching child
// command, and subcommand components (cli.list, cli.new, ...) mount
// under it as lazy nodes. The activation chain is pure framework —
// every level calls scope.Activate(child, args), passing arguments
// through scope.Args — so no dispatch logic lives outside the
// components. Any loong application can mount these components and
// get a component-driven CLI; see examples/cli for the complete
// template (embedded or file-based config tree, any entry style).
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zsying/loong"
)

// ErrUsage marks a command-line usage error (unknown or incomplete
// invocation); callers map it to an exit code (2 in examples/cli).
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
