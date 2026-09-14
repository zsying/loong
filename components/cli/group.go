package cli

import (
	"fmt"
	"strings"

	"github.com/zsying/loong"
)

// GroupType is the component type of the generic command group. It is
// the one command shape whose children are commands, which is what the
// usage listing reads it for: a group renders its children inline
// (`daemon <start, stop>`), every other node's children are mounted
// components and stay out of the listing.
const GroupType = "loong.cli.group"

// Group is a lazy container for command groups: it holds subcommand
// nodes in the tree (e.g. `config` with `show`/`set` children). Its
// Run is the generic group behavior — the first argument names a
// child command to activate, the rest are passed down — so a group
// needs no custom code, only this one component type.
type Group struct {
	loong.Base
}

// Build validates the group's command names: two children must not
// answer the same argv token (e.g. `config.show` and `cache.show`
// mounted under one group both claim `show`). The check lives here
// and in the master's Build so a collision fails at assembly, not the
// first time the user types the command.
func (g *Group) Build(ctx *loong.Scope) error {
	return checkCommandTokens(ctx.Node)
}

func (g *Group) Run(ctx *loong.Scope) error {
	args, err := ArgsOf(ctx)
	if err != nil {
		return err
	}
	// One payload type end to end: whatever the parent handed down is an
	// *Args, so the group forwards it unchanged instead of unpacking the
	// pre-node shape from the command shape.
	rest := args.Rest()
	if len(rest) == 0 {
		return fmt.Errorf("%w: %s requires one of [%s]", ErrUsage, ctx.Node.ID, strings.Join(commandNames(ctx.Node), ", "))
	}
	child := matchChild(ctx.Node, rest[0])
	if child == nil {
		return fmt.Errorf("%w: %s has no command %q (one of [%s])", ErrUsage, ctx.Node.ID, rest[0], strings.Join(commandNames(ctx.Node), ", "))
	}
	return ctx.Activate(child.ID, args.Derive(rest[1:]))
}

func init() {
	loong.RegisterComponent(GroupType, func() loong.Component { return &Group{} },
		loong.WithDesc("CLI command group container (activates its child command)"),
	)
}

// commandName is the name a node answers to on the command line: the
// tail of its id (the segment after the last dot). Node ids are
// globally unique, so two groups can each mount a `show` as
// `config.show` and `cache.show`; the tail is what disambiguates them
// per group. A single-segment id is its own name.
func commandName(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		return id[i+1:]
	}
	return id
}

// matchChild returns the child of n that the argv token names: an
// exact id match wins (so a token may always spell the full id), and
// otherwise the token matches the child's command name — the id's
// tail. Build-time token checks guarantee at most one child matches.
func matchChild(n *loong.Node, token string) *loong.Node {
	var byTail *loong.Node
	for _, c := range n.Children {
		if c.ID == token {
			return c
		}
		if commandName(c.ID) == token && byTail == nil {
			byTail = c
		}
	}
	return byTail
}

// checkCommandTokens verifies that no two children of n answer the
// same argv token — a collision between an exact id and another
// child's tail counts too (children `show` and `config.show` under one
// group both claim `show`).
func checkCommandTokens(n *loong.Node) error {
	owner := make(map[string]string)
	for _, c := range n.Children {
		for _, tok := range []string{c.ID, commandName(c.ID)} {
			if prev, dup := owner[tok]; dup && prev != c.ID {
				return fmt.Errorf("cli: command token %q of %q collides with %q under %q", tok, c.ID, prev, n.ID)
			}
			owner[tok] = c.ID
		}
	}
	return nil
}

func commandNames(n *loong.Node) []string {
	names := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		names = append(names, commandName(c.ID))
	}
	return names
}
