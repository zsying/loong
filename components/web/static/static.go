// Package static provides the optional "web.static" component: static
// file hosting with an optional SPA fallback, mounted on a parent web
// channel's Router. A pure API backend simply does not mount it.
package static

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/zsying/loong"
	"github.com/zsying/loong/components/web"
)

// Config is the component's own config block.
type Config struct {
	Dir string `yaml:"dir"`
	// SPA serves index.html for every path that does not map to an
	// existing file (single-page apps with client-side routing).
	SPA bool `yaml:"spa,omitempty"`
	// API is a path prefix whose unmatched requests stay 404 in SPA
	// mode instead of falling back to index.html — the fallback must
	// not mask routes registered by API components (e.g. "/api").
	// Empty disables the exclusion.
	API string `yaml:"api,omitempty"`
}

// Static is the static hosting component. It must be mounted under a
// web node: it registers the catch-all "/" on the parent's Router.
type Static struct {
	loong.Base
}

func (s *Static) Build(ctx *loong.Scope) error {
	cfg, err := ctx.Config[Config]()
	if err != nil {
		return err
	}
	if cfg.Dir == "" {
		return errors.New("web.static: missing dir (usage: dir: ./public)")
	}
	if _, err := os.Stat(cfg.Dir); err != nil {
		return fmt.Errorf("web.static: %w", err)
	}
	s.Base.Build(ctx)
	r := ctx.Get[*web.Router]()
	if r == nil {
		return errors.New("web.static: no web Router available (mount this component under a web node)")
	}
	r.Handle("", "/", handler(cfg.Dir, cfg.API, cfg.SPA))
	return nil
}

// handler serves files from dir; with SPA enabled it falls back to
// index.html when the path does not resolve to a regular file, except
// under apiPrefix where unmatched requests stay 404.
func handler(dir, apiPrefix string, spa bool) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	if !spa {
		return fs
	}
	root := http.Dir(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := root.Open(p); err == nil {
				info, err := f.Stat()
				_ = f.Close()
				if err == nil && !info.IsDir() {
					fs.ServeHTTP(w, r)
					return
				}
			}
		}
		if underPath(r.URL.Path, apiPrefix) {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

// underPath reports whether path equals prefix or sits below it as a
// path segment ("/api" matches "/api" and "/api/x", not "/apix").
func underPath(path, prefix string) bool {
	if prefix == "" {
		return false
	}
	return strings.HasPrefix(path, prefix) &&
		(len(path) == len(prefix) || path[len(prefix)] == '/')
}

func init() {
	loong.RegisterComponent("web.static", func() loong.Component { return &Static{} },
		loong.WithConfig[Config](),
		loong.WithDesc("static file hosting with optional SPA fallback (mount under web)"),
	)
}
