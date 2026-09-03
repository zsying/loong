package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zsying/loong"
	"github.com/zsying/loong/components/web"
)

func cfgNode(t *testing.T, s string) yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatal(err)
	}
	return *doc.Content[0]
}

// assemble mounts a web channel (not listening) with a web.static child
// and returns the root Router for direct requests.
func assemble(t *testing.T, dir, prefix, api string, spa bool) *web.Router {
	t.Helper()
	spaY := "false"
	if spa {
		spaY = "true"
	}
	root := &loong.Node{
		Type: "base",
		Children: []*loong.Node{
			{Type: "web", ID: "main", Config: cfgNode(t, "listen: \"\"\n"), Children: []*loong.Node{
				{Type: "web.static", ID: "static", Config: cfgNode(t, "dir: "+strconv.Quote(dir)+"\nprefix: "+strconv.Quote(prefix)+"\nspa: "+spaY+"\napi: "+strconv.Quote(api)+"\n")},
			}},
		},
	}
	k := loong.New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	comp, err := k.Component("static")
	if err != nil {
		t.Fatal(err)
	}
	return comp.(*Static).Scope.Get[*web.Router]()
}

func TestStaticHosting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := assemble(t, dir, "", "", false)

	if rec := get(t, r, "/hello.txt"); rec.Code != http.StatusOK || rec.Body.String() != "hi" {
		t.Errorf("GET /hello.txt = %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(t, r, "/missing.txt"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /missing.txt = %d, want 404", rec.Code)
	}
}

func TestStaticPrefixHosting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := assemble(t, dir, "/assets", "", false)

	if rec := get(t, r, "/assets/hello.txt"); rec.Code != http.StatusOK || rec.Body.String() != "hi" {
		t.Errorf("GET /assets/hello.txt = %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(t, r, "/hello.txt"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /hello.txt outside prefix = %d, want 404", rec.Code)
	}
}

func TestStaticSPAFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := assemble(t, dir, "", "", true)

	if rec := get(t, r, "/some/deep/route"); rec.Code != http.StatusOK || rec.Body.String() != "<html>app</html>" {
		t.Errorf("SPA fallback = %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(t, r, "/"); rec.Code != http.StatusOK || rec.Body.String() != "<html>app</html>" {
		t.Errorf("GET / = %d %q", rec.Code, rec.Body.String())
	}
}

func TestStaticSPAApiNotMasked(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := assemble(t, dir, "", "/api", true)

	// Unmatched API requests must stay 404 — the SPA fallback must not
	// return index.html for them. The prefix is matched per segment:
	// "/api" and "/api/x" are excluded, "/apix" is not.
	if rec := get(t, r, "/api/unknown"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/unknown = %d, want 404", rec.Code)
	}
	if rec := get(t, r, "/api"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /api = %d, want 404", rec.Code)
	}
	if rec := get(t, r, "/apix/route"); rec.Code != http.StatusOK {
		t.Errorf("GET /apix/route = %d, want SPA fallback 200", rec.Code)
	}
	if rec := get(t, r, "/some/deep/route"); rec.Code != http.StatusOK || rec.Body.String() != "<html>app</html>" {
		t.Errorf("SPA fallback = %d %q", rec.Code, rec.Body.String())
	}
}

func get(t *testing.T, r *web.Router, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
