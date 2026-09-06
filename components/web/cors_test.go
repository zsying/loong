package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// req builds a request with the given headers against the router.
func corsReq(t *testing.T, r *Router, method string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/x", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func corsRouter(cfg CORSConfig) *Router {
	r := NewRouter()
	r.Use(CORS(cfg))
	r.Get("/api/x", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) })
	return r
}

func TestCORSPreflight(t *testing.T) {
	r := corsRouter(CORSConfig{
		Origins: []string{"https://a.test"},
		Methods: []string{"GET", "PUT"},
		Headers: []string{"Content-Type"},
		MaxAge:  600,
	})
	rec := corsReq(t, r, http.MethodOptions, map[string]string{
		"Origin":                         "https://a.test",
		"Access-Control-Request-Method":  "PUT",
		"Access-Control-Request-Headers": "X-Token",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204", rec.Code)
	}
	h := rec.Header()
	if got := h.Get("Access-Control-Allow-Origin"); got != "https://a.test" {
		t.Errorf("allow-origin = %q", got)
	}
	if got := h.Get("Access-Control-Allow-Methods"); got != "GET, PUT" {
		t.Errorf("allow-methods = %q, want the configured list", got)
	}
	if got := h.Get("Access-Control-Allow-Headers"); got != "Content-Type" {
		t.Errorf("allow-headers = %q, want the configured list", got)
	}
	if got := h.Get("Access-Control-Max-Age"); got != "600" {
		t.Errorf("max-age = %q", got)
	}
	if got := h.Get("Vary"); !strings.Contains(got, "Origin") {
		t.Errorf("vary = %q, want Origin", got)
	}
	// Preflight must not reach the handler.
	if rec.Body.Len() != 0 {
		t.Errorf("preflight body = %q, want empty", rec.Body.String())
	}
}

func TestCORSPreflightEchoesRequestedHeaders(t *testing.T) {
	r := corsRouter(CORSConfig{})
	rec := corsReq(t, r, http.MethodOptions, map[string]string{
		"Origin":                         "https://any.test",
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "X-Token, X-Trace",
	})
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "X-Token, X-Trace" {
		t.Errorf("allow-headers = %q, want the requested headers echoed", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodDelete) {
		t.Errorf("allow-methods = %q, want the default verbs", got)
	}
}

func TestCORSAllowedAndDenied(t *testing.T) {
	r := corsRouter(CORSConfig{Origins: []string{"https://a.test"}})

	rec := corsReq(t, r, http.MethodGet, map[string]string{"Origin": "https://a.test"})
	if rec.Code != http.StatusOK || rec.Body.String() != "x" {
		t.Fatalf("allowed request = %d %q, want 200 x", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://a.test" {
		t.Errorf("allow-origin = %q", got)
	}
	// A foreign origin gets no CORS headers — the browser rejects it,
	// exactly as it would without the middleware.
	rec = corsReq(t, r, http.MethodGet, map[string]string{"Origin": "https://b.test"})
	if rec.Code != http.StatusOK {
		t.Fatalf("denied request = %d, want the handler to still run", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("denied origin allow-origin = %q, want none", got)
	}
	// Same-origin and server-side calls carry no Origin header.
	rec = corsReq(t, r, http.MethodGet, nil)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("no-origin allow-origin = %q, want none", got)
	}
}

func TestCORSCredentialsEchoesOrigin(t *testing.T) {
	r := corsRouter(CORSConfig{Credentials: true})
	rec := corsReq(t, r, http.MethodGet, map[string]string{"Origin": "https://a.test"})
	// "*" is invalid together with credentials, so the origin is echoed.
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://a.test" {
		t.Errorf("allow-origin with credentials = %q, want the echoed origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("allow-credentials = %q, want true", got)
	}
}

func TestCORSDefaultAllowsAllOrigins(t *testing.T) {
	r := corsRouter(CORSConfig{})
	rec := corsReq(t, r, http.MethodGet, map[string]string{"Origin": "https://any.test"})
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("allow-origin = %q, want *", got)
	}
}
