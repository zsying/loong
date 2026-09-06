package web

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// CORSConfig configures the CORS middleware. Zero values are
// permissive: an empty Origins allows every origin ("*") and the
// method/header lists fall back to the defaults below.
type CORSConfig struct {
	// Origins lists the allowed origins. Empty allows all ("*");
	// a request from any other origin gets no CORS headers, so the
	// browser rejects it as it would without the middleware.
	Origins []string `yaml:"origins,omitempty"`
	// Methods is the allowed method list for preflight responses,
	// defaulting to the usual REST verbs plus HEAD and OPTIONS.
	Methods []string `yaml:"methods,omitempty"`
	// Headers is the allowed request header list for preflight
	// responses. Empty echoes the browser's requested headers.
	Headers []string `yaml:"headers,omitempty"`
	// Expose lists response headers scripts may read; empty adds no
	// Access-Control-Expose-Headers.
	Expose []string `yaml:"expose,omitempty"`
	// Credentials allows cookies and Authorization to cross origins.
	// With it the wildcard "*" is invalid, so the request's own
	// origin is echoed instead.
	Credentials bool `yaml:"credentials,omitempty"`
	// MaxAge is how long browsers may cache a preflight response, in
	// seconds. Zero omits the header (no caching).
	MaxAge int `yaml:"max_age,omitempty"`
}

var defaultCORSMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut,
	http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions,
}

// CORS returns a middleware answering cross-origin requests: preflight
// (OPTIONS with Access-Control-Request-Method) ends here with 204,
// ordinary requests get the access headers and continue. It is not
// enabled by default — a component opts in with r.Use(web.CORS(cfg)) —
// because which origins may call an API is a business decision.
func CORS(cfg CORSConfig) Middleware {
	methods := strings.Join(orDefault(cfg.Methods, defaultCORSMethods), ", ")
	headers := strings.Join(cfg.Headers, ", ")
	expose := strings.Join(cfg.Expose, ", ")
	maxAge := ""
	if cfg.MaxAge > 0 {
		maxAge = strconv.Itoa(cfg.MaxAge)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			// Same-origin and non-browser calls carry no Origin:
			// nothing to negotiate, leave the response untouched.
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			allowed, ok := allowOrigin(origin, cfg)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Set("Access-Control-Allow-Origin", allowed)
			// The header varies with the request, so caches must
			// not reuse one origin's answer for another.
			h.Add("Vary", "Origin")
			if cfg.Credentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			if maxAge != "" {
				h.Set("Access-Control-Max-Age", maxAge)
			}
			if expose != "" {
				h.Set("Access-Control-Expose-Headers", expose)
			}
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", methods)
				h.Set("Access-Control-Allow-Headers", orHeader(headers, r))
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// allowOrigin reports the origin value to send back, and whether this
// request is allowed at all. Credentials forbid the wildcard, so the
// origin is echoed in that case.
func allowOrigin(origin string, cfg CORSConfig) (string, bool) {
	if len(cfg.Origins) == 0 {
		if cfg.Credentials {
			return origin, true
		}
		return "*", true
	}
	if slices.Contains(cfg.Origins, "*") {
		return "*", true
	}
	if slices.Contains(cfg.Origins, origin) {
		return origin, true
	}
	return "", false
}

// orHeader returns the configured header list, or echoes the headers
// the browser asked for when none are configured.
func orHeader(headers string, r *http.Request) string {
	if headers != "" {
		return headers
	}
	return r.Header.Get("Access-Control-Request-Headers")
}

// orDefault returns v when non-empty, def otherwise.
func orDefault(v, def []string) []string {
	if len(v) > 0 {
		return v
	}
	return def
}
