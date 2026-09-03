// Command hello is the v0.1 sample project. It demonstrates the loong
// kernel with log, user, auth and web components, plus a business
// component mounted under the web channel, started via loong.LoadAndRun
// and shut down gracefully on SIGINT/SIGTERM. The config tree root uses
// the kernel-provided "base" container, so no custom root component is
// declared here.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/zsying/loong/components/auth"
	_ "github.com/zsying/loong/components/log"
	_ "github.com/zsying/loong/components/user"
	"github.com/zsying/loong/components/web"
	_ "github.com/zsying/loong/components/web/account"

	"github.com/zsying/loong"
)

type greetConfig struct {
	Route string `yaml:"route"`
}

// greet is a business component mounted under the web channel. It
// registers an HTTP route through the parent's Router service — the
// registration surface hides the underlying mux, so business code only
// names a method, a path and a plain http handler.
type greet struct {
	loong.Base
	route string
}

func (g *greet) Build(ctx *loong.Scope) error {
	cfg, err := ctx.Config[greetConfig]()
	if err != nil {
		return err
	}
	g.Base.Build(ctx)
	g.route = cfg.Route
	// Missing services are errors, never silent skips — a greet mounted
	// outside a web channel would otherwise register nothing and run
	// without a trace.
	r := ctx.Get[*web.Router]()
	if r == nil {
		return fmt.Errorf("biz.greet: no web Router available (mount this component under a web node)")
	}
	r.Get(g.route, func(w http.ResponseWriter, req *http.Request) {
		web.WriteJSON(w, http.StatusOK, map[string]string{"msg": "greetings from biz component"})
	})
	return nil
}

func init() {
	loong.RegisterComponent("biz.greet", func() loong.Component { return &greet{} },
		loong.WithConfig[greetConfig](),
		loong.WithDesc("sample business component mounted under a web channel"),
	)
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: hello <config.yaml>")
	}
	// LoadAndRun loads the config tree, assembles a Kernel, runs the whole
	// tree, then (WithWait) blocks for SIGINT/SIGTERM and shuts down
	// gracefully. Without WithWait it returns the running Kernel.
	if _, err := loong.LoadAndRun(os.Args[1], loong.WithWait()); err != nil {
		log.Fatal(err)
	}
}
