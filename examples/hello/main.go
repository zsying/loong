// Command hello is the v0.1 sample project. It demonstrates the loong
// kernel with log, user and web components, plus a business component
// mounted under the web channel, started via loong.LoadAndRun and shut
// down gracefully on SIGINT/SIGTERM.
package main

import (
	"log"
	"net/http"
	"os"

	_ "github.com/zsying/loong/components/log"
	_ "github.com/zsying/loong/components/user"
	"github.com/zsying/loong/components/web"

	"github.com/zsying/loong"
)

// app is the root container component.
type app struct {
	loong.Base
}

type greetConfig struct {
	Route string `yaml:"route"`
}

// greet is a business component mounted under the web channel. It
// registers an HTTP route through the parent's Router service and
// emits an event upward on each request. Embedding loong.Base keeps
// the three other phases as no-ops.
type greet struct {
	loong.Base
	route string
}

func (g *greet) Build(ctx *loong.Scope) error {
	var cfg greetConfig
	if err := ctx.Config.Decode(&cfg); err != nil {
		return err
	}
	g.Base.Build(ctx)
	g.route = cfg.Route
	if r := ctx.Get[*web.Router](); r != nil {
		r.Handle("GET", g.route, func(rw http.ResponseWriter, req *http.Request) {
			_ = g.Emit("biz.greet.hello", map[string]string{"msg": "greetings from biz"})
			_, _ = rw.Write([]byte("greetings from biz component\n"))
		})
	}
	return nil
}

func init() {
	loong.RegisterComponent("app", func() loong.Component { return &app{} })
	loong.RegisterComponent("biz.greet", func() loong.Component { return &greet{} },
		loong.WithEvents("biz.greet.hello"),
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
