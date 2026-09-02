package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	s, err := NewService("test-secret", 0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func guardedReq(t *testing.T, s *Service, token string) *httptest.ResponseRecorder {
	t.Helper()
	h := s.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := Identity(r)
		_, _ = w.Write([]byte("id=" + id))
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGuardFlow(t *testing.T) {
	s := newTestService(t)
	tok, err := s.Issue("42")
	if err != nil {
		t.Fatal(err)
	}

	if rec := guardedReq(t, s, tok); rec.Code != http.StatusOK || rec.Body.String() != "id=42" {
		t.Errorf("guarded with token = %d %q, want 200 id=42", rec.Code, rec.Body.String())
	}
	if rec := guardedReq(t, s, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("without token = %d, want 401", rec.Code)
	}
	if rec := guardedReq(t, s, "garbage.token.here"); rec.Code != http.StatusUnauthorized {
		t.Errorf("garbage token = %d, want 401", rec.Code)
	}
	if rec := guardedReq(t, s, tok+"x"); rec.Code != http.StatusUnauthorized {
		t.Errorf("tampered token = %d, want 401", rec.Code)
	}

	// A token signed with a different secret must not verify.
	other, err := NewService("other-secret", 0)
	if err != nil {
		t.Fatal(err)
	}
	otherTok, err := other.Issue("42")
	if err != nil {
		t.Fatal(err)
	}
	if rec := guardedReq(t, s, otherTok); rec.Code != http.StatusUnauthorized {
		t.Errorf("token from other secret = %d, want 401", rec.Code)
	}
}

func TestExpiredToken(t *testing.T) {
	// Issue with a sub-nanosecond TTL, then verify with a same-secret
	// default service: the token must be rejected as expired.
	short, err := NewService("test-secret", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	tok, err := short.Issue("1")
	if err != nil {
		t.Fatal(err)
	}
	s := newTestService(t)
	if rec := guardedReq(t, s, tok); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired token = %d, want 401", rec.Code)
	}
}
