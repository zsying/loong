// Command cli demonstrates building a CLI as a loong component tree:
// a cli master (importing components/cli) plus platform components and
// custom business command components, all wired through an embedded
// config tree. Any loong application can do the same — the master +
// subcommand shape is the template for CLI development.
package main

import (
	_ "embed"
	"os"

	"github.com/zsying/loong/components/cli"
	_ "github.com/zsying/loong/components/log"
	_ "github.com/zsying/loong/components/user"
	_ "github.com/zsying/loong/components/web"
)

//go:embed cli.yaml
var cliYAML []byte

func main() {
	os.Exit(cli.Go(cliYAML))
}
