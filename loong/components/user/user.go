// Package user provides the user system: accounts, password
// authentication and a minimal SQLite-backed store
// (modernc.org/sqlite, pure Go, no cgo).
package user

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"github.com/zsying/loong"
)

// Config is the component's own config block, decoded by the component.
type Config struct {
	DBPath string `yaml:"db_path"`
}

// User is the core user record. OpenID is reserved for wechat channels
// (miniprogram / official account); unionid bridging is a future
// extension component.
type User struct {
	ID        int64
	Username  string
	Password  string // bcrypt hash
	Nickname  string
	OpenID    string
	CreatedAt time.Time
}

// Service exposes user operations to other components via the kernel's
// type-based service lookup.
type Service struct {
	db *sql.DB
}

// Close releases the underlying database.
func (s *Service) Close() error { return s.db.Close() }

// Register creates an account with a username and password.
func (s *Service) Register(username, password, nickname string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, nickname, created_at) VALUES (?, ?, ?, ?)`,
		username, string(hash), nickname, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.FindByID(id)
}

// Authenticate verifies a username/password pair.
func (s *Service) Authenticate(username, password string) (*User, error) {
	u, err := s.FindByUsername(username)
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)) != nil {
		return nil, errors.New("user: invalid password")
	}
	return u, nil
}

// FindByID returns the user with the given id.
func (s *Service) FindByID(id int64) (*User, error) {
	return s.scanRow(s.db.QueryRow(
		`SELECT id, username, password_hash, nickname, openid, created_at FROM users WHERE id = ?`, id))
}

// FindByUsername returns the user with the given username.
func (s *Service) FindByUsername(username string) (*User, error) {
	return s.scanRow(s.db.QueryRow(
		`SELECT id, username, password_hash, nickname, openid, created_at FROM users WHERE username = ?`, username))
}

// FindByOpenID returns the user bound to a wechat openid, if any.
func (s *Service) FindByOpenID(openid string) (*User, error) {
	return s.scanRow(s.db.QueryRow(
		`SELECT id, username, password_hash, nickname, openid, created_at FROM users WHERE openid = ?`, openid))
}

// CreateWithOpenID creates (or returns) the user bound to a wechat
// openid — the entry point for miniprogram / official account login.
func (s *Service) CreateWithOpenID(openid, nickname string) (*User, error) {
	if u, err := s.FindByOpenID(openid); err == nil {
		return u, nil
	}
	res, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, nickname, openid, created_at) VALUES (?, '', ?, ?, ?)`,
		"wx_"+openid, nickname, openid, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.FindByID(id)
}

func (s *Service) scanRow(row *sql.Row) (*User, error) {
	var (
		u       User
		pass    string
		oid     sql.NullString
		created string
	)
	err := row.Scan(&u.ID, &u.Username, &pass, &u.Nickname, &oid, &created)
	if err != nil {
		return nil, err
	}
	u.Password = pass
	u.OpenID = oid.String
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &u, nil
}

// Component is the user system component itself.
type Component struct {
	loong.Base
	Service *Service
}

func (c *Component) Build(ctx *loong.Scope) error {
	var cfg Config
	if err := ctx.Config.Decode(&cfg); err != nil {
		return err
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "loong.db"
	}
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		password_hash TEXT,
		nickname TEXT,
		openid TEXT,
		created_at TEXT)`); err != nil {
		return fmt.Errorf("user: init schema: %w", err)
	}
	c.Service = &Service{db: db}
	ctx.Kernel.Provide(c.Service)
	return nil
}

// Stop closes the underlying database.
func (c *Component) Stop(*loong.Scope) error {
	if c.Service != nil {
		return c.Service.Close()
	}
	return nil
}

func init() {
	loong.Register("user", func() loong.Component { return &Component{} })
}
