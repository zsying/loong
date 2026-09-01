package cli

import (
	"github.com/zsying/loong"
)

// Group is a lazy container for command groups: it holds subcommand
// nodes in the tree (e.g. `config` with `show`/`set` children) and
// has no logic of its own. Activating it is a no-op; dispatch walks
// into its children.
type Group struct {
	loong.Base
}

func init() {
	loong.RegisterComponent("cli.group", func() loong.Component { return &Group{} },
		loong.WithDesc("CLI command group container"),
	)
}
