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
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/zsying/loong"
)

// Config is the channel's own config block. Only server-level fields
// belong here; static dirs, session secrets and account endpoints live
// in the child components that provide them.
type Config struct {
	Listen string `yaml:"listen"`
	// TLS turns the listener into HTTPS. It is optional: an absent
	// block serves plain HTTP, so a project behind a reverse proxy
	// (or on a trusted network) never has to think about it.
	TLS *TLS `yaml:"tls,omitempty"`
	// Log enables per-request logging (default on, set false to turn
	// off). nil means on, so the zero Config still logs.
	Log *bool `yaml:"log,omitempty"`
}

// TLS holds the certificate files for HTTPS. Both are required
// whenever the block is present.
type TLS struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

// Web is the web channel component: it owns the Router, starts the HTTP
// server in Run and shuts it down gracefully in Stop.
type Web struct {
	loong.Base
	cfg    Config
	router *Router
	srv    *http.Server
	ln     net.Listener
}

func (w *Web) Build(scope *loong.Scope) error {
	cfg, err := scope.Config[Config]()
	if err != nil {
		return err
	}
	w.Base.Build(scope)
	w.cfg = cfg
	w.router = NewRouter()
	// RequestLog is registered first so it wraps Recover and reports
	// the final status — including the 500 a recovered panic turns
	// into. Recover sits inside it and catches panics from the
	// handlers below.
	w.router.Use(RequestLog())
	w.router.Use(Recover())
	return nil
}

// Run starts serving. An empty listen address is a no-op — tests and
// embedded setups serve the Router directly instead. Binding is
// synchronous: a busy port or missing permission fails assembly
// (Run returns the error and already-built nodes are stopped) instead
// of silently running a dead server.
func (w *Web) Run(*loong.Scope) error {
	// Route conflicts surface as assembly errors: the stdlib mux
	// panics on a duplicate pattern, which would take the whole
	// process down instead of failing the component.
	if err := w.router.Err(); err != nil {
		return err
	}
	if w.cfg.Listen == "" {
		return nil
	}
	ln, err := net.Listen("tcp", w.cfg.Listen)
	if err != nil {
		return fmt.Errorf("web: listen %s: %w", w.cfg.Listen, err)
	}
	if w.cfg.TLS != nil {
		slog.Info("web listening (https)", "addr", ln.Addr().String())
	} else {
		slog.Info("web listening", "addr", ln.Addr().String())
	}
	for _, rt := range w.router.Routes() {
		slog.Info("web route", "method", routeMethod(rt.Method), "path", rt.Path)
	}
	w.srv = &http.Server{
		Handler:           w.router,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		// ReadTimeout / WriteTimeout are deliberately unset: the
		// channel must not assume a business response time (SSE,
		// uploads, long polls are the business's choice). The two
		// header/idle limits above only guard slow readers between
		// requests and slowloris-style header stalls.
	}
	// Load the certificate pair here, not inside ServeTLS: a missing
	// or broken file must fail assembly rather than leave a listener
	// that silently serves nothing.
	if w.cfg.TLS != nil {
		if w.cfg.TLS.Cert == "" || w.cfg.TLS.Key == "" {
			_ = ln.Close()
			return errors.New("web: tls requires both cert and key")
		}
		cert, err := tls.LoadX509KeyPair(w.cfg.TLS.Cert, w.cfg.TLS.Key)
		if err != nil {
			_ = ln.Close()
			return fmt.Errorf("web: tls: %w", err)
		}
		w.srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	}
	go func() {
		var err error
		if w.srv.TLSConfig != nil {
			// The pair is already loaded, so ServeTLS only serves.
			err = w.srv.ServeTLS(ln, "", "")
		} else {
			err = w.srv.Serve(ln)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("web server", "err", err)
		}
	}()
	w.ln = ln
	return nil
}

// Addr returns the bound listener address once Run has bound the
// socket, nil otherwise — useful for tests and for resolving ":0".
func (w *Web) Addr() net.Addr {
	if w.ln == nil {
		return nil
	}
	return w.ln.Addr()
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
