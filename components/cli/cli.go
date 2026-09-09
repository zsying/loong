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

// ErrUsage marks an incomplete invocation — a command group reached
// without a subcommand. Callers map it to an exit code (2 in
// examples/cli); an unknown command is a plain activation error.
var ErrUsage = errors.New("cli: usage error")

// Component is the cli master: it parses os.Args and activates the
// child command named by the first argument, passing the rest as the
// command's arguments via Scope.Activate.
//
// Its behavior is configured through the config block (see Config):
//
//	type: cli
//	config:
//	  default: tui      # bare invocation activates this child instead of usage
//	  pre: globals      # child activated once before the first command
//	  flags:            # global flags peeled anywhere in the argv
//	    verbose: { short: v, kind: bool }
//	    lang:    { kind: value }
//
// The pre node receives *Globals (peeled flags + remaining args); the
// command receives []string. Pre semantics: it runs once per process
// (ensureActive caches the activation) and only before an actual
// command — a usage request never triggers it. A pre failure aborts
// the whole activation chain (fail-fast); the pre id is not selectable
// as a command. Without a config block the master behaves exactly as
// before: args[0] is the command, bare invocation prints usage.
type Component struct {
	loong.Base
	cfg Config
}

// Build decodes and validates the config block so a misconfigured tree
// fails loudly at assembly instead of at command time.
func (c *Component) Build(ctx *loong.Scope) error {
	cfg, err := ctx.Config[Config]()
	if err != nil {
		return err
	}
	spellingOwner := map[string]string{}
	for name, spec := range cfg.Flags {
		switch spec.Kind {
		case "", "bool", "value":
		default:
			return fmt.Errorf("cli: flag %q has unknown kind %q (want bool or value)", name, spec.Kind)
		}
		for _, s := range flagSpellings(name, spec) {
			if owner, dup := spellingOwner[s]; dup {
				return fmt.Errorf("cli: flag spelling %q collision between %q and %q", s, owner, name)
			}
			spellingOwner[s] = name
		}
	}
	if cfg.Pre != "" && !hasChild(ctx.Node, cfg.Pre) {
		return fmt.Errorf("cli: pre %q is not a child of %q", cfg.Pre, ctx.Node.ID)
	}
	if cfg.Default != "" && !hasChild(ctx.Node, cfg.Default) {
		return fmt.Errorf("cli: default %q is not a child of %q", cfg.Default, ctx.Node.ID)
	}
	c.cfg = cfg
	return nil
}

func (c *Component) Run(ctx *loong.Scope) error {
	return c.masterRun(ctx, os.Args[1:])
}

// masterRun is the argv-driven core of the master, separated from Run
// so tests can drive it without touching the process argv.
func (c *Component) masterRun(ctx *loong.Scope, args []string) error {
	g, err := peel(args, c.cfg.Flags)
	if err != nil {
		return err
	}
	cmdID, rest := selectCommand(g.Rest)
	if cmdID == "" {
		if c.cfg.Default == "" {
			return usage(ctx, c.cfg.Pre)
		}
		cmdID, rest = c.cfg.Default, g.Rest
	} else if cmdID == c.cfg.Pre {
		return fmt.Errorf("cli: %q is the pre node, not a command", cmdID)
	}
	if c.cfg.Pre != "" {
		// Fail-fast: a pre failure aborts the whole activation chain —
		// a half-initialized environment is worse than a clean error.
		if err := ctx.Activate(c.cfg.Pre, g); err != nil {
			return err
		}
	}
	return ctx.Activate(cmdID, rest)
}

func init() {
	loong.RegisterComponent("cli", func() loong.Component { return &Component{} },
		loong.WithDesc("CLI master: parses the command line and activates the command"),
	)
}

// usage prints the command tree under the master and returns nil:
// invoking the CLI without a command is a help request, not an error
// (the process exits 0). The pre node is not a command and is excluded
// from the listing. Only an incomplete invocation — e.g. a cli.group
// reached without a subcommand — reports ErrUsage.
func usage(ctx *loong.Scope, pre string) error {
	fmt.Println("usage: <command>")
	var cmds []*loong.Node
	for _, c := range ctx.Node.Children {
		if c.ID != pre {
			cmds = append(cmds, c)
		}
	}
	printCommands(cmds, 1)
	return nil
}

func hasChild(n *loong.Node, id string) bool {
	for _, c := range n.Children {
		if c.ID == id {
			return true
		}
	}
	return false
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
