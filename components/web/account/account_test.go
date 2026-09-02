package account

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// newTestServer assembles a real tree — user (in-memory), auth, a web
// channel that is not listening — with web.account mounted, and returns
// the channel Router for direct requests.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	root := &loong.Node{
		Type: "base",
		Children: []*loong.Node{
			{Type: "user", ID: "users", Config: cfgNode(t, "db_path: \":memory:\"\n")},
			{Type: "auth", ID: "auth", Config: cfgNode(t, "secret: test-secret\n")},
			{Type: "web", ID: "main", Config: cfgNode(t, "listen: \"\"\n"), Children: []*loong.Node{
				{Type: "web.account", ID: "account"},
			}},
		},
	}
	k := loong.New()
	if err := k.Assemble(root); err != nil {
		t.Fatal(err)
	}
	comp, err := k.Component("account")
	if err != nil {
		t.Fatal(err)
	}
	r := comp.(*Account).Scope.Get[*web.Router]()
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, url, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

func get(t *testing.T, url, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

func TestAuthFlow(t *testing.T) {
	srv := newTestServer(t)

	// register succeeds and returns the user.
	code, body := postJSON(t, srv.URL+"/api/auth/register",
		`{"username":"john","password":"secret123","nickname":"John"}`)
	if code != http.StatusOK {
		t.Fatalf("register status = %d, body = %s", code, body)
	}

	// duplicate register is a 409, not a leaked internal error.
	code, _ = postJSON(t, srv.URL+"/api/auth/register",
		`{"username":"john","password":"secret123","nickname":"John"}`)
	if code != http.StatusConflict {
		t.Errorf("duplicate register status = %d, want 409", code)
	}

	// login returns a token.
	code, body = postJSON(t, srv.URL+"/api/auth/login",
		`{"username":"john","password":"secret123"}`)
	if code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", code, body)
	}
	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &loginResp); err != nil || loginResp.Token == "" {
		t.Fatalf("login body = %s, want token", body)
	}

	// protected endpoint: token accepted and the real user is returned,
	// missing token rejected.
	code, body = get(t, srv.URL+"/api/me", loginResp.Token)
	if code != http.StatusOK || !strings.Contains(body, "john") {
		t.Errorf("me with token = %d, body = %s", code, body)
	}
	if code, _ := get(t, srv.URL+"/api/me", ""); code != http.StatusUnauthorized {
		t.Errorf("me without token = %d, want 401", code)
	}

	// wrong password is a 401.
	if code, _ := postJSON(t, srv.URL+"/api/auth/login",
		`{"username":"john","password":"wrong"}`); code != http.StatusUnauthorized {
		t.Errorf("login with wrong password = %d, want 401", code)
	}
}
