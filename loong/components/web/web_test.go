package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zsying/loong/components/user"
)

// newTestServer wires a Web component with an in-memory user store.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	svc, err := user.OpenService(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	w := &Web{cfg: Config{JWTSecret: "test-secret"}, users: svc}
	w.mux = http.NewServeMux()
	w.mux.HandleFunc("POST /api/auth/register", w.handleRegister)
	w.mux.HandleFunc("POST /api/auth/login", w.handleLogin)
	w.mux.HandleFunc("GET /api/me", w.requireAuth(w.handleMe))
	srv := httptest.NewServer(w.mux)
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

func get(t *testing.T, url, token string) int {
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
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
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

	// protected endpoint: token accepted, missing token rejected.
	if code := get(t, srv.URL+"/api/me", loginResp.Token); code != http.StatusOK {
		t.Errorf("me with token = %d, want 200", code)
	}
	if code := get(t, srv.URL+"/api/me", ""); code != http.StatusUnauthorized {
		t.Errorf("me without token = %d, want 401", code)
	}

	// wrong password is a 401.
	if code, _ := postJSON(t, srv.URL+"/api/auth/login",
		`{"username":"john","password":"wrong"}`); code != http.StatusUnauthorized {
		t.Errorf("login with wrong password = %d, want 401", code)
	}
}
