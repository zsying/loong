package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zsying/loong"
)

func cfgNode(t *testing.T, s string) yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatal(err)
	}
	return *doc.Content[0]
}

// echoComp is a test business component registering one route on the
// parent web Router, mirroring how account/static/biz components mount.
type echoComp struct {
	loong.Base
}

func (c *echoComp) Build(ctx *loong.Scope) error {
	c.Base.Build(ctx)
	if r := ctx.Get[*Router](); r != nil {
		r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("pong")) })
	}
	return nil
}

func init() {
	loong.RegisterComponent("test.echo", func() loong.Component { return &echoComp{} })
}

func getReq(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, rdr)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRouterVerbsAndPaths(t *testing.T) {
	r := NewRouter()
	r.Get("/hello", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("get")) })
	r.Post("/hello", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("post")) })
	// Go 1.22 path values flow through unchanged.
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(req.PathValue("id")))
	})

	if rec := getReq(t, r, http.MethodGet, "/hello", nil); rec.Code != 200 || rec.Body.String() != "get" {
		t.Errorf("GET /hello = %d %q", rec.Code, rec.Body.String())
	}
	if rec := getReq(t, r, http.MethodPost, "/hello", nil); rec.Code != 200 || rec.Body.String() != "post" {
		t.Errorf("POST /hello = %d %q", rec.Code, rec.Body.String())
	}
	if rec := getReq(t, r, http.MethodGet, "/users/42", nil); rec.Code != 200 || rec.Body.String() != "42" {
		t.Errorf("GET /users/42 = %d %q", rec.Code, rec.Body.String())
	}
}

func TestRouterGroup(t *testing.T) {
	r := NewRouter()
	// guard is a group middleware marking requests as authorized.
	guard := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get("X-Token") == "" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
	admin := r.Group("/admin", guard)
	admin.Get("/users", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	r.Get("/open", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("open")) })

	// Grouped handler mounted under the prefix and guarded.
	if rec := getReq(t, r, http.MethodGet, "/admin/users", nil); rec.Code != http.StatusForbidden {
		t.Errorf("guarded without token = %d, want 403", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req.Header.Set("X-Token", "t")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Errorf("guarded with token = %d %q", rec.Code, rec.Body.String())
	}
	// Ungrouped route is not guarded.
	if rec := getReq(t, r, http.MethodGet, "/open", nil); rec.Code != 200 || rec.Body.String() != "open" {
		t.Errorf("open route = %d %q", rec.Code, rec.Body.String())
	}
}

func TestRouterUseServerWide(t *testing.T) {
	r := NewRouter()
	var seen []string
	mark := func(tag string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				seen = append(seen, tag)
				next.ServeHTTP(w, req)
			})
		}
	}
	r.Use(mark("a"), mark("b"))
	r.Get("/x", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) })
	if rec := getReq(t, r, http.MethodGet, "/x", nil); rec.Code != 200 {
		t.Fatalf("GET /x = %d", rec.Code)
	}
	// Outermost middleware runs first.
	if len(seen) != 2 || seen[0] != "a" || seen[1] != "b" {
		t.Errorf("middleware order = %v, want [a b]", seen)
	}
}

func TestRecoverMiddleware(t *testing.T) {
	r := NewRouter()
	r.Use(Recover())
	r.Get("/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })
	r.Get("/ok", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	if rec := getReq(t, r, http.MethodGet, "/panic", nil); rec.Code != http.StatusInternalServerError {
		t.Errorf("panic = %d, want 500", rec.Code)
	}
	if rec := getReq(t, r, http.MethodGet, "/ok", nil); rec.Code != 200 {
		t.Errorf("ok = %d, want 200", rec.Code)
	}
}

// TestMiddlewareKeepsStreaming pins that the Recover/RequestLog
// wrappers stay transparent: handlers must still be able to flush
// (SSE, chunked downloads) and to hijack the connection (WebSocket).
func TestMiddlewareKeepsStreaming(t *testing.T) {
	r := NewRouter()
	r.Use(RequestLog(), Recover())
	r.Get("/stream", func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("a"))
		f.Flush()
		_, _ = w.Write([]byte("b"))
	})
	rec := getReq(t, r, http.MethodGet, "/stream", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stream = %d", rec.Code)
	}
	if rec.Body.String() != "ab" {
		t.Errorf("body = %q, want ab", rec.Body.String())
	}
	if !rec.Flushed {
		t.Error("handler flushed but the middleware dropped it")
	}
}

// TestMiddlewareKeepsHijack runs the upgrade on a real server, since
// only a real connection supports hijacking.
func TestMiddlewareKeepsHijack(t *testing.T) {
	r := NewRouter()
	r.Use(RequestLog(), Recover())
	done := make(chan error, 1)
	r.Get("/upgrade", func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			done <- errors.New("no hijacker")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\n\r\n")
		_ = buf.Flush()
		done <- nil
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	c, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("GET /upgrade HTTP/1.1\r\nHost: x\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("hijack through middleware: %v", err)
	}
}

func TestRecorderUnwrap(t *testing.T) {
	inner := httptest.NewRecorder()
	wrapped := &responseRecorder{ResponseWriter: inner}
	if got := wrapped.Unwrap(); got != http.ResponseWriter(inner) {
		t.Fatalf("Unwrap = %v, want the wrapped writer", got)
	}
}

func TestRouterRoutes(t *testing.T) {
	r := NewRouter()
	r.Get("/hello", func(w http.ResponseWriter, _ *http.Request) {})
	r.Group("/admin").Get("/users", func(w http.ResponseWriter, _ *http.Request) {})
	r.Handle("", "/", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	got := r.Routes()
	want := []Route{{Method: "GET", Path: "/hello"}, {Method: "GET", Path: "/admin/users"}, {Method: "", Path: "/"}}
	if len(got) != len(want) {
		t.Fatalf("Routes = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Routes[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if routeMethod("") != "*" || routeMethod("GET") != "GET" {
		t.Errorf("routeMethod wrong: %q %q", routeMethod(""), routeMethod("GET"))
	}
}

// TestGroupMiddlewareNotDoubled pins the server-wide vs group rule:
// a server-wide Use middleware must run exactly once on group routes.
// (Copying root middlewares into groups made them run twice.)
func TestGroupMiddlewareNotDoubled(t *testing.T) {
	r := NewRouter()
	var server, group int
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			server++
			next.ServeHTTP(w, req)
		})
	})
	admin := r.Group("/admin", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			group++
			next.ServeHTTP(w, req)
		})
	})
	admin.Get("/users", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })

	rec := getReq(t, r, http.MethodGet, "/admin/users", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/users = %d", rec.Code)
	}
	if server != 1 || group != 1 {
		t.Errorf("server middleware = %d, group middleware = %d; want 1 and 1", server, group)
	}
}

// TestWebListenFailFast verifies that an occupied port fails assembly
// instead of silently running a dead server.
func TestWebListenFailFast(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	root := &loong.Node{
		Type:     "base",
		Children: []*loong.Node{{Type: "web", ID: "main", Config: cfgNode(t, "listen: "+strconv.Quote(addr)+"\n")}},
	}
	k := loong.New()
	if err := k.Assemble(root); err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("Assemble with busy port = %v, want listen error", err)
	}
}

// TestWebComponentRun covers the full channel lifecycle through a real
// socket: assemble with listen 127.0.0.1:0, hit an endpoint registered
// by a child component over HTTP, then shut down gracefully.
func TestWebComponentRun(t *testing.T) {
	root := &loong.Node{
		Type: "base",
		Children: []*loong.Node{
			{Type: "web", ID: "main", Config: cfgNode(t, "listen: 127.0.0.1:0\n"), Children: []*loong.Node{
				{Type: "test.echo", ID: "echo"},
			}},
		},
	}
	k := loong.New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Shutdown() })

	comp, err := k.Component("main")
	if err != nil {
		t.Fatal(err)
	}
	addr := comp.(*Web).Addr()
	if addr == nil {
		t.Fatal("Addr() = nil, server did not bind")
	}
	resp, err := http.Get("http://" + addr.String() + "/ping")
	if err != nil {
		t.Fatalf("GET /ping: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "pong" {
		t.Errorf("GET /ping = %d %q, want 200 pong", resp.StatusCode, body)
	}

	// Graceful shutdown: after Stop the endpoint refuses connections.
	if err := k.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if _, err := http.Get("http://" + addr.String() + "/ping"); err == nil {
		t.Error("GET /ping after Shutdown succeeded, want connection error")
	}
}

func TestJSONHelpers(t *testing.T) {
	r := NewRouter()
	r.Post("/echo", func(w http.ResponseWriter, req *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		if err := ReadJSON(req, &in); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"name": in.Name})
	})
	rec := getReq(t, r, http.MethodPost, "/echo", map[string]string{"name": "john"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /echo = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type = %q", ct)
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out["name"] != "john" {
		t.Errorf("body = %s, want {\"name\":\"john\"}", rec.Body.String())
	}
	if rec := getReq(t, r, http.MethodPost, "/echo", "not-json"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad json = %d, want 400", rec.Code)
	}
}
