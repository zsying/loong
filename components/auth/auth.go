// Package auth provides session tokens (JWT) as a cross-channel
// component: issuing, verifying and guarding HTTP handlers. It depends
// only on net/http — no web component, no user component — so the same
// Service guards any channel (web today, miniprogram/wechat tomorrow),
// and swapping the token scheme never touches the channel component.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/zsying/loong"
)

// Config is the component's own config block.
type Config struct {
	Secret string `yaml:"secret"`
	// TTL is the token lifetime as a Go duration, default 24h.
	TTL string `yaml:"ttl,omitempty"`
}

// Service issues and verifies HMAC-signed JWTs carrying a subject id.
type Service struct {
	secret []byte
	ttl    time.Duration
}

// NewService builds a Service; empty secrets are rejected, non-positive
// TTL falls back to 24h.
func NewService(secret string, ttl time.Duration) (*Service, error) {
	if secret == "" {
		return nil, errors.New("auth: empty secret")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Service{secret: []byte(secret), ttl: ttl}, nil
}

// Issue signs a token for the given subject id.
func (s *Service) Issue(sub string) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   sub,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(s.secret)
}

// verify parses and validates a token, rejecting any algorithm other
// than HMAC to prevent algorithm-confusion attacks.
func (s *Service) verify(token string) (*jwt.RegisteredClaims, error) {
	claims := &jwt.RegisteredClaims{}
	if _, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("auth: unexpected signing method %v", t.Header["alg"])
		}
		return s.secret, nil
	}); err != nil {
		return nil, err
	}
	return claims, nil
}

type ctxKey struct{}

// Guard protects a handler: it requires an "Authorization: Bearer
// <token>" header, verifies the token and injects the subject id into
// the request context. Unauthorized requests get a 401 before next
// runs. Handlers read the id with Identity.
func (s *Service) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		claims, err := s.verify(token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, claims.Subject)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Identity returns the authenticated subject id that Guard injected.
func Identity(r *http.Request) (string, bool) {
	id, ok := r.Context().Value(ctxKey{}).(string)
	return id, ok
}

// Component is the auth component itself, exposing the Service to the
// tree.
type Component struct {
	loong.Base
	Service *Service
}

func (c *Component) Build(scope *loong.Scope) error {
	cfg, err := scope.Config[Config]()
	if err != nil {
		return err
	}
	c.Base.Build(scope)
	ttl := 24 * time.Hour
	if cfg.TTL != "" {
		if ttl, err = time.ParseDuration(cfg.TTL); err != nil {
			return fmt.Errorf("auth: bad ttl %q: %w", cfg.TTL, err)
		}
	}
	svc, err := NewService(cfg.Secret, ttl)
	if err != nil {
		return err
	}
	c.Service = svc
	return nil
}

func init() {
	loong.RegisterComponent("auth", func() loong.Component { return &Component{} },
		loong.WithConfig[Config](),
		loong.WithService(func(c loong.Component) *Service { return c.(*Component).Service }),
		loong.WithDesc("session tokens (JWT): issue, verify and a Guard middleware"),
	)
}
