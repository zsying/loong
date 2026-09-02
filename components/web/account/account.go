// Package account provides the optional "web.account" component: the
// standard account HTTP API — register / login / me — wiring the user
// and auth services onto a parent web channel's Router. Projects that
// authenticate differently (openid-only, external IdP) skip it and
// register their own endpoints instead.
package account

import (
	"errors"
	"net/http"
	"strconv"

	"log/slog"

	"github.com/zsying/loong"
	"github.com/zsying/loong/components/auth"
	"github.com/zsying/loong/components/user"
	"github.com/zsying/loong/components/web"
)

// Account is the account API component. It must be mounted under a web
// node with user and auth components mounted in the same tree.
type Account struct {
	loong.Base
	users *user.Service
	auth  *auth.Service
}

func (c *Account) Build(ctx *loong.Scope) error {
	c.Base.Build(ctx)
	c.users = ctx.Get[*user.Service]()
	c.auth = ctx.Get[*auth.Service]()
	if c.users == nil {
		return errors.New("web.account: user service not mounted (add a user component)")
	}
	if c.auth == nil {
		return errors.New("web.account: auth service not mounted (add an auth component)")
	}
	r := ctx.Get[*web.Router]()
	if r == nil {
		return errors.New("web.account: no web Router available (mount this component under a web node)")
	}
	r.Post("/api/auth/register", c.handleRegister)
	r.Post("/api/auth/login", c.handleLogin)
	r.Handle(http.MethodGet, "/api/me", c.auth.Guard(http.HandlerFunc(c.handleMe)))
	return nil
}

func (c *Account) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Nickname string `json:"nickname"`
	}
	if err := web.ReadJSON(r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	u, err := c.users.Register(req.Username, req.Password, req.Nickname)
	if err != nil {
		if errors.Is(err, user.ErrUsernameTaken) {
			http.Error(w, "username already taken", http.StatusConflict)
			return
		}
		slog.Error("account register", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"id": u.ID, "username": u.Username, "nickname": u.Nickname})
}

func (c *Account) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := web.ReadJSON(r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	u, err := c.users.Authenticate(req.Username, req.Password)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	token, err := c.auth.Issue(strconv.FormatInt(u.ID, 10))
	if err != nil {
		slog.Error("account login", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"token": token})
}

func (c *Account) handleMe(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.Identity(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	uid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	u, err := c.users.FindByID(uid)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"id": u.ID, "username": u.Username, "nickname": u.Nickname})
}

func init() {
	loong.RegisterComponent("web.account", func() loong.Component { return &Account{} },
		loong.WithDesc("account API: register / login / me over user + auth (mount under web)"),
	)
}
