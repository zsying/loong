// Command cli demonstrates building a CLI as a loong component tree:
// a cli master (importing components/cli) plus platform components and
// custom business command components, all wired through an embedded
// config tree. Any loong application can do the same — the master +
// subcommand shape is the template for CLI development.
//
// Two equivalent ways to drive it are shown below: the packaged
// one-liner (cli.Go) and the plain standard-API version (same steps,
// written out). cli.Go is just the standard API plus CLI-domain logic
// (multi-level dispatch, exit codes, argument injection); the plain
// version is what you write when the command dispatch stays trivial.
package main

import (
	_ "embed"
	"log"
	"os"

	"github.com/zsying/loong"
	"github.com/zsying/loong/components/cli" // registers the cli master + subcommands
	_ "github.com/zsying/loong/components/log"
	_ "github.com/zsying/loong/components/user"
	_ "github.com/zsying/loong/components/web"
)

//go:embed cli.yaml
var cliYAML []byte

func main() {
	// Way 1 — packaged template: parse the embedded tree, assemble,
	// dispatch the command path from os.Args (multi-level included),
	// shut down, and return a normalized exit code.
	//
	//   os.Exit(cli.Go(cliYAML))

	// Way 2 — plain standard API (equivalent for a single command;
	// multi-level paths like "config show" need Dispatch, which lives
	// in components/cli — the standard API has no notion of command
	// paths). Every step is a core loong call; the command nodes are
	// lazy, so assembly only activates the eager cli master, and
	// Activate runs exactly the requested command.
	// if len(os.Args) < 2 {
	// 	os.Exit(2)
	// }
	// root, err := loong.Parse(cliYAML)
	// if err != nil {
	// 	os.Exit(1)
	// }
	// k := loong.New()
	// if err := k.Assemble(root); err != nil {
	// 	os.Exit(1)
	// }
	// if err := k.Activate(os.Args[1]); err != nil {
	// 	_ = k.Shutdown()
	// 	os.Exit(1)
	// }
	// _ = k.Shutdown()

	// Way 3 — the most general loong entry: LoadAndRun reads the
	// config tree from a file (no embed), assembles it and runs the
	// eager cli master; cli.Dispatch then drives the requested command
	// (multi-level included) with a normalized exit code. Use this
	// when the tree lives beside the binary rather than embedded.
	k, err := loong.LoadAndRun("./cli.yaml")
	if err != nil {
		log.Fatal(err)
	}
	code := cli.Dispatch(k, os.Args[1:])
	if err := k.Shutdown(); err != nil && code == 0 {
		code = 1
	}
	os.Exit(code)
}
