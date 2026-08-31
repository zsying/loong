// Command loong is the loong platform CLI. It is itself a loong
// application: the subcommands are components mounted in an embedded
// config tree (cli.yaml, shipped inline via go:embed), assembled but
// kept lazy, and the requested command is activated on demand. Adding
// a command means registering a new component type and a node in the
// tree — no changes to the dispatch code.
package main

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/zsying/loong"
	_ "github.com/zsying/loong/components/log"
	_ "github.com/zsying/loong/components/user"
	_ "github.com/zsying/loong/components/web"
)

//go:embed cli.yaml
var cliYAML []byte

// cli is the root container component of the CLI's own tree.
type cli struct {
	loong.Base
}

func init() {
	loong.RegisterComponent("cli", func() loong.Component { return &cli{} })
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]

	root, err := loong.Parse(cliYAML)
	if err != nil {
		fmt.Fprintln(os.Stderr, "loong: parse embedded config:", err)
		os.Exit(1)
	}
	k := loong.New()
	if err := k.Assemble(root); err != nil {
		fmt.Fprintln(os.Stderr, "loong:", err)
		os.Exit(1)
	}
	// The command components are lazy; activating the requested one
	// builds and runs only that command.
	if err := k.Activate(cmd); err != nil {
		fmt.Fprintln(os.Stderr, "loong:", err)
		_ = k.Shutdown()
		os.Exit(1)
	}
	_ = k.Shutdown()
}

func usage() {
	fmt.Fprint(os.Stderr, `loong — loong platform CLI (itself a loong component tree)

usage:
  loong list [--html]           list registered components (services, config, events)
  loong new <name> [-o dir]     scaffold a new component source file
  loong tree <config.yaml> [--html]  show a project config tree with services and config
`)
}
