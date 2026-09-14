// Package cli provides the CLI building blocks for loong applications.
// It follows the same pattern as the web channel: a cli master
// component (type "loong.cli", activated at assembly like any non-lazy
// node) parses the command line and activates the matching child
// command, and subcommand components (loong.cli.list, loong.cli.new, ...)
// mount under it as lazy nodes. The activation chain is pure framework —
// every level calls scope.Activate(child, args), passing arguments
// through scope.Args — so no dispatch logic lives outside the
// components. Any loong application can mount these components and
// get a component-driven CLI; see examples/cli for the complete
// template (embedded or file-based config tree, any entry style).
//
// Command names vs node ids: a node id is globally unique, but a
// command token is matched per parent — exactly against the child id,
// else against the id's tail (`config.show` answers `show`). Two
// groups can therefore each mount a `show` without colliding; a
// token claimed by two siblings fails the master's/group's Build.
package cli

import (
	"errors"
	"fmt"
	"io"
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
//	type: loong.cli
//	config:
//	  default: tui      # bare invocation activates this child instead of usage
//	  pre: globals      # child activated once before the first command
//	  flags:            # global flags peeled anywhere in the argv
//	    verbose: { short: v, kind: bool }
//	    lang:    { kind: value }
//
// The master node's own Desc is the program description printed at the
// top of the help screen.
//
// The pre node and the command both receive *Args — the peeled global
// flags plus the argv that is left — so a command can read a global flag
// that was peeled from its own argv. Pre semantics: it runs once per
// process (ensureActive caches the activation) and only before an actual
// command — a usage request never triggers it. A pre failure aborts
// the whole activation chain (fail-fast); the pre id is not selectable
// as a command. Without a config block the master behaves exactly as
// before: args[0] is the command, bare invocation prints usage.
//
// Help is built in: `-h` / `--help` anywhere in the argv, or a `help`
// command token, print the usage tree and exit 0 — no pre run, no
// command activation. The master yields both spellings when they are
// taken: a flag declared with the "h"/"help" spelling keeps the argv
// token, and a child command named "help" keeps the token.
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
	if err := checkCommandTokens(ctx.Node); err != nil {
		return err
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
	if c.helpRequested(args) {
		return usage(ctx, c.cfg.Pre)
	}
	g, err := Peel(args, c.cfg.Flags)
	if err != nil {
		return err
	}
	token, rest := selectCommand(g.Rest())
	// cmdID is the node id to activate: for an argv token, the child
	// the token names (exact id, else id tail); for a bare invocation
	// with a default configured, the default's own id.
	var cmdID string
	if token == "" {
		if c.cfg.Default == "" {
			return usage(ctx, c.cfg.Pre)
		}
		cmdID, rest = c.cfg.Default, g.Rest()
	} else if child := matchChild(ctx.Node, token); child == nil {
		// A help token with no command claiming it prints the usage
		// tree. Like every usage path, no pre runs and nothing
		// activates.
		if token == "help" {
			return usage(ctx, c.cfg.Pre)
		}
		return fmt.Errorf("cli: unknown command %q", token)
	} else if child.ID == c.cfg.Pre {
		return fmt.Errorf("cli: %q is the pre node, not a command", token)
	} else {
		cmdID = child.ID
	}
	if c.cfg.Pre != "" {
		// Fail-fast: a pre failure aborts the whole activation chain —
		// a half-initialized environment is worse than a clean error.
		if err := ctx.Activate(c.cfg.Pre, g); err != nil {
			return err
		}
	}
	// The command gets the peel result narrowed to its own argv, not the
	// bare tail: a global flag was removed from that tail, so dropping
	// the globals here would make -v invisible to the command that has to
	// honour it.
	return ctx.Activate(cmdID, g.Derive(rest))
}

// helpRequested reports whether the argv asks for help: the exact
// tokens "-h" or "--help" anywhere, unless the app's flag schema
// declares those spellings for itself (auto-yield — the master never
// steals a declared flag).
func (c *Component) helpRequested(args []string) bool {
	claimed := map[string]bool{}
	for name, spec := range c.cfg.Flags {
		for _, s := range flagSpellings(name, spec) {
			claimed[s] = true
		}
	}
	for _, a := range args {
		if (a == "-h" || a == "--help") && !claimed[a] {
			return true
		}
	}
	return false
}

func init() {
	loong.RegisterComponent("loong.cli", func() loong.Component { return &Component{} },
		loong.WithDesc("CLI master: parses the command line and activates the command"),
	)
}

// usage prints the program header, the command tree under the master
// and returns nil: invoking the CLI without a command is a help
// request, not an error (the process exits 0). The master node's own
// Desc, when set, is the program's one-line description — the header
// above the usage block. The pre node is not a command and is excluded
// from the listing. Only an incomplete invocation — e.g. a cli.group
// reached without a subcommand — reports ErrUsage.
func usage(ctx *loong.Scope, pre string) error {
	return writeUsage(os.Stdout, ctx.Node, pre, isTerminal(os.Stdout))
}

// writeUsage renders the help screen: the program description (the
// master node's Desc, when set), the usage line, and the two-column
// command listing — each section separated by a blank line, with a
// trailing blank line so the prompt picks up on a fresh row.
func writeUsage(w io.Writer, n *loong.Node, pre string, color bool) error {
	if n.Desc != "" {
		fmt.Fprintln(w, n.Desc)
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "usage: <command>")
	fmt.Fprintln(w)
	var cmds []*loong.Node
	for _, c := range n.Children {
		if c.ID != pre {
			cmds = append(cmds, c)
		}
	}
	writeCommands(w, cmds, color)
	fmt.Fprintln(w)
	return nil
}

// ANSI SGR sequences for the usage listing — bold for command names,
// gray for descriptions. Enabled only on an interactive stdout, same
// rule as the log component's console format.
const (
	ansiBold  = "\x1b[1m"
	ansiGray  = "\x1b[90m"
	ansiReset = "\x1b[0m"
)

// writeCommands renders the command list as a two-column table: the
// command name (plus its subcommands for groups) in bold, and the
// node's description — Node.Desc, falling back to the component type's
// WithDesc — in gray. Descriptions align in a second column.
//
// Only a group spells its children out, because only a group does
// anything with them: it activates the child its first argument names,
// so `daemon <start, stop>` is a true statement about the argv. Every
// other node's children are mounted components — a command that mounts
// a server under itself, say — and listing those would advertise
// commands that do not exist. Names shown are command names — id
// tails — not raw node ids.
func writeCommands(w io.Writer, cmds []*loong.Node, color bool) {
	heads := make([]string, len(cmds))
	descs := make([]string, len(cmds))
	width := 0
	for i, c := range cmds {
		head := commandName(c.ID)
		if c.Type == GroupType && len(c.Children) > 0 {
			head += " <" + strings.Join(commandNames(c), ", ") + ">"
		}
		heads[i] = head
		if len(head) > width {
			width = len(head)
		}
		descs[i] = c.Desc
		if descs[i] == "" {
			descs[i] = loong.Describe(c.Type)
		}
	}
	for i, head := range heads {
		if descs[i] == "" {
			line := "  " + head
			if color {
				line = "  " + ansiBold + head + ansiReset
			}
			fmt.Fprintln(w, line)
			continue
		}
		padded := head + strings.Repeat(" ", width-len(head))
		name, desc := padded+" ", descs[i]
		if color {
			name, desc = "  "+ansiBold+padded+ansiReset+" ", ansiGray+descs[i]+ansiReset
		} else {
			name = "  " + name
		}
		fmt.Fprintln(w, name+desc)
	}
}

// isTerminal reports whether f is an interactive terminal, not a
// redirected file (same detection as the log component).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func hasChild(n *loong.Node, id string) bool {
	for _, c := range n.Children {
		if c.ID == id {
			return true
		}
	}
	return false
}
