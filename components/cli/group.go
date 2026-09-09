package cli

import (
	"fmt"
	"strings"

	"github.com/zsying/loong"
)

// Group is a lazy container for command groups: it holds subcommand
// nodes in the tree (e.g. `config` with `show`/`set` children). Its
// Run is the generic group behavior — the first argument names a
// child command to activate, the rest are passed down — so a group
// needs no custom code, only this one component type.
type Group struct {
	loong.Base
}

func (g *Group) Run(ctx *loong.Scope) error {
	args, _ := ctx.Args.([]string)
	// Dual Args contract: a master with flags peeling may hand down
	// *Globals — the group unpacks Rest so either form works.
	if gl, ok := ctx.Args.(*Globals); ok {
		args = gl.Rest
	}
	if len(args) == 0 {
		return fmt.Errorf("%w: %s requires one of [%s]", ErrUsage, ctx.Node.ID, strings.Join(childIDs(ctx.Node), ", "))
	}
	return ctx.Activate(args[0], args[1:])
}

func init() {
	loong.RegisterComponent("cli.group", func() loong.Component { return &Group{} },
		loong.WithDesc("CLI command group container (activates its child command)"),
	)
}

func childIDs(n *loong.Node) []string {
	ids := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		ids = append(ids, c.ID)
	}
	return ids
}
