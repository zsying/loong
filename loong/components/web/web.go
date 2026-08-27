// Package web provides the web channel: stdlib HTTP routing
// (Go 1.22 method+path patterns), static hosting and JWT auth.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/zsying/loong/components/user"
	"github.com/zsying/loong"
)

// Config is the component's own config block, decoded by the component.
type Config struct {
	Listen    string `yaml:"listen"`
	Static    string `yaml:"static,omitempty"`
	JWTSecret string `yaml:"jwt_secret"`
}

// Router is a capability service provided to child components so they
// can register HTTP routes on this channel.
type Router struct {
	mux *http.ServeMux
}

// Handle registers a handler for a Go 1.22 pattern like "GET /api/x".
func (r *Router) Handle(method, pattern string, h http.HandlerFunc) {
	r.mux.HandleFunc(method+" "+pattern, h)
}

// Web is the web channel component.
type Web struct {
	loong.Base
	cfg   Config
	mux   *http.ServeMux
	srv   *http.Server
	users *user.Service
}

func (w *Web) Register(reg *loong.Registry) error {
	// Demo subscription: react to events emitted by business children.
	reg.On("biz.greet.hello", func(e loong.Event) error {
		slog.Info("event received", "name", e.Name, "source", e.Source, "payload", e.Payload)
		return nil
	})
	return nil
}

func (w *Web) Build(ctx *loong.Scope) error {
	var cfg Config
	if err := ctx.Config.Decode(&cfg); err != nil {
		return err
	}
	w.cfg = cfg
	w.mux = http.NewServeMux()
	ctx.Kernel.Provide(&Router{mux: w.mux})
	w.users = loong.Get[*user.Service](ctx.Kernel)

	w.mux.HandleFunc("POST /api/auth/register", w.handleRegister)
	w.mux.HandleFunc("POST /api/auth/login", w.handleLogin)
	w.mux.HandleFunc("GET /api/me", w.requireAuth(w.handleMe))
	if cfg.Static != "" {
		w.mux.Handle("/", http.FileServer(http.Dir(cfg.Static)))
	}
	return nil
}

func (w *Web) Run(ctx *loong.Scope) error {
	slog.Info("web listening", "addr", w.cfg.Listen)
	w.srv = &http.Server{Addr: w.cfg.Listen, Handler: w.mux}
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

func (w *Web) handleRegister(rw http.ResponseWriter, r *http.Request) {
	if w.users == nil {
		http.Error(rw, "user service not mounted", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Nickname string `json:"nickname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(rw, "bad request", http.StatusBadRequest)
		return
	}
	u, err := w.users.Register(req.Username, req.Password, req.Nickname)
	if err != nil {
		if errors.Is(err, user.ErrUsernameTaken) {
			http.Error(rw, "username already taken", http.StatusConflict)
			return
		}
		slog.Error("register", "err", err)
		http.Error(rw, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"id": u.ID, "username": u.Username, "nickname": u.Nickname})
}

func (w *Web) handleLogin(rw http.ResponseWriter, r *http.Request) {
	if w.users == nil {
		http.Error(rw, "user service not mounted", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(rw, "bad request", http.StatusBadRequest)
		return
	}
	u, err := w.users.Authenticate(req.Username, req.Password)
	if err != nil {
		http.Error(rw, "invalid credentials", http.StatusUnauthorized)
		return
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": u.ID,
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(w.cfg.JWTSecret))
	if err != nil {
		http.Error(rw, "token error", http.StatusInternalServerError)
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"token": signed})
}

func (w *Web) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		claims := jwt.MapClaims{}
		_, err := jwt.ParseWithClaims(tok, claims, func(t *jwt.Token) (any, error) {
			// Reject any algorithm other than HMAC to prevent
			// algorithm-confusion attacks.
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("web: unexpected signing method %v", t.Header["alg"])
			}
			return []byte(w.cfg.JWTSecret), nil
		})
		if err != nil {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(rw, r)
	}
}

func (w *Web) handleMe(rw http.ResponseWriter, r *http.Request) {
	writeJSON(rw, http.StatusOK, map[string]any{"user": "authenticated"})
}

func writeJSON(rw http.ResponseWriter, status int, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(v)
}

func init() {
	loong.Register("web", func() loong.Component { return &Web{} })
}
