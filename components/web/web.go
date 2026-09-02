// Package web provides the web channel component: an HTTP server whose
// lifecycle is a loong component, plus the *web.Router registration
// surface handed to mounted components. It knows nothing about business
// — session tokens (auth), account APIs (web.account), static hosting
// (web.static) and business endpoints are all optional components
// mounted below it. Routing stays on the stdlib ServeMux (Go 1.22
// method+path patterns) so handlers and middleware interoperate with
// the whole net/http ecosystem.
package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/zsying/loong"
)

// Config is the channel's own config block. Only server-level fields
// belong here; static dirs, session secrets and account endpoints live
// in the child components that provide them.
type Config struct {
	Listen string `yaml:"listen"`
	// Log enables per-request logging (default on, set false to turn
	// off). nil means on, so the zero Config still logs.
	Log *bool `yaml:"log,omitempty"`
}

// Web is the web channel component: it owns the Router, starts the HTTP
// server in Run and shuts it down gracefully in Stop.
type Web struct {
	loong.Base
	cfg    Config
	router *Router
	srv    *http.Server
}

func (w *Web) Build(scope *loong.Scope) error {
	cfg, err := scope.Config[Config]()
	if err != nil {
		return err
	}
	w.Base.Build(scope)
	w.cfg = cfg
	w.router = NewRouter()
	// RequestLog first so it wraps Recover and reports the final
	// status (including recovered panics), Recover outermost in
	// effect for panics in the chain below it.
	w.router.Use(RequestLog())
	w.router.Use(Recover())
	return nil
}

// Run starts serving. An empty listen address is a no-op — tests and
// embedded setups serve the Router directly instead.
func (w *Web) Run(*loong.Scope) error {
	if w.cfg.Listen == "" {
		return nil
	}
	slog.Info("web listening", "addr", w.cfg.Listen)
	for _, rt := range w.router.Routes() {
		slog.Info("web route", "method", routeMethod(rt.Method), "path", rt.Path)
	}
	w.srv = &http.Server{
		Addr:              w.cfg.Listen,
		Handler:           w.router,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := w.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("web server", "err", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the HTTP server with a 5s timeout.
func (w *Web) Stop(*loong.Scope) error {
	if w.srv == nil {
		return nil
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.srv.Shutdown(sctx)
}

func init() {
	loong.RegisterComponent("web", func() loong.Component { return &Web{} },
		loong.WithConfig[Config](),
		loong.WithService(func(c loong.Component) *Router { return c.(*Web).router }),
		loong.WithDesc("web channel: HTTP server with a Router registration surface"),
	)
}

// routeMethod renders the method label for startup output: catch-all
// registrations (empty method) are shown as "*".
func routeMethod(m string) string {
	if m == "" {
		return "*"
	}
	return m
}
