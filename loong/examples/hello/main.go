// Command hello is the v0.1 sample project. It demonstrates the loong
// kernel with log, user and web components, plus a business component
// mounted under the web channel, and graceful shutdown on SIGINT.
package main

import (
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

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
	if r := ctx.Kernel.Get[*web.Router](); r != nil {
		r.Handle("GET", g.route, func(rw http.ResponseWriter, req *http.Request) {
			_ = g.Emit("biz.greet.hello", map[string]string{"msg": "greetings from biz"})
			_, _ = rw.Write([]byte("greetings from biz component\n"))
		})
	}
	return nil
}

func init() {
	loong.RegisterComponent("app", func() loong.Component { return &app{} })
	loong.RegisterComponent("biz.greet", func() loong.Component { return &greet{} })
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: hello <config.yaml>")
	}
	root, err := loong.LoadTree(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	k := loong.New()
	if err := k.Assemble(root); err != nil {
		log.Fatal(err)
	}
	slog.Info("hello running, Ctrl+C to stop")
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	slog.Info("shutting down")
	if err := k.Shutdown(); err != nil {
		slog.Error("shutdown", "err", err)
	}
}
