// Command cli demonstrates building a CLI as a loong component tree:
// a cli master (importing components/cli) plus platform components and
// custom business command components, all wired through an embedded
// config tree. The activation chain is pure framework — the master
// parses the command line and activates the matching child via
// scope.Activate, arguments flow down through scope.Args, and command
// groups (cli.group) forward to their children. No dispatch code
// lives outside the components.
//
// Three entry styles are shown: the packaged one-liner (cli.Go), the
// plain standard API, and LoadAndRun for a file-based tree. In all of
// them the command runs during assembly (the eager master activates
// it), so main only starts the app, maps errors to exit codes, and
// shuts down.
package main

import (
	_ "embed"
	"errors"
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
	// Way 1 — packaged one-liner: parse the embedded tree, assemble
	// (the master activates the command), shut down, exit code.
	//
	//   os.Exit(cli.Go(cliYAML))

	// Way 2 — plain standard API with an embedded tree (default here).
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

	// Way 3 — the most general entry: config tree from a file (no
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

// exitCode maps an error to a process exit code: usage errors (cli's
// ErrUsage) are 2, everything else is 1.
func exitCode(err error) int {
	if errors.Is(err, cli.ErrUsage) {
		return 2
	}
	log.Print(err)
	return 1
}
