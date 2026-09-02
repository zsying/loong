package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
