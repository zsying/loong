// Command cli demonstrates building a CLI as a loong component tree:
// a cli master (importing components/cli) plus platform components and
// custom business command components, all wired through a config tree.
// The activation chain is pure framework — the master parses the
// command line and activates the matching child via scope.Activate,
// arguments flow down through scope.Args, and command groups
// (cli.group) forward to their children. No dispatch code lives
// outside the components; main only starts the app, maps errors to
// exit codes, and shuts down (the command runs during assembly).
//
// Two entry styles are shown: an embedded config tree with the plain
// standard API (default), and LoadAndRun for a file-based tree — the
// only difference is where the tree comes from.
package main

import (
	_ "embed"
	"errors"
	"fmt"
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
	// Entry 1 — embedded config tree via the plain standard API.
	root, err := loong.Parse(cliYAML)
	if err != nil {
		os.Exit(1)
	}
	k := loong.New()
	if err := k.Assemble(root); err != nil { // the master runs the command here
		_ = k.Shutdown()
		os.Exit(exitCode(err))
	}
	if err := k.Shutdown(); err != nil {
		os.Exit(1)
	}

	// Entry 2 — the most general entry: config tree from a file (no
	// embed), LoadAndRun + error mapping. Swap the block above for:
	//
	//   k, err := loong.LoadAndRun("./cli.yaml")
	//   if err != nil {
	//       os.Exit(exitCode(err))
	//   }
	//   if err := k.Shutdown(); err != nil {
	//       os.Exit(1)
	//   }
}

// exitCode maps an error to a process exit code — an application-level
// choice: usage errors (cli.ErrUsage) are 2, everything else is 1.
// Every error is printed before exiting — usage errors must never
// exit silently.
func exitCode(err error) int {
	if errors.Is(err, cli.ErrUsage) {
		fmt.Fprintln(os.Stderr, err) // e.g. "cli config" without a subcommand
		return 2
	}
	log.Print(err)
	return 1
}
