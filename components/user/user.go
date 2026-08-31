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

// ErrUsernameTaken is returned when registering an existing username.
var ErrUsernameTaken = errors.New("user: username already taken")

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

// OpenService opens (and initializes) a user store at the given
// database path. ":memory:" is supported for tests.
func OpenService(dbPath string) (*Service, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		password_hash TEXT,
		nickname TEXT,
		openid TEXT,
		created_at TEXT)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("user: init schema: %w", err)
	}
	return &Service{db: db}, nil
}

// Register creates an account with a username and password.
func (s *Service) Register(username, password, nickname string) (*User, error) {
	if _, err := s.FindByUsername(username); err == nil {
		return nil, ErrUsernameTaken
	}
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
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("user: last insert id: %w", err)
	}
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
// INSERT OR IGNORE makes concurrent logins with the same openid safe.
func (s *Service) CreateWithOpenID(openid, nickname string) (*User, error) {
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO users (username, password_hash, nickname, openid, created_at) VALUES (?, '', ?, ?, ?)`,
		"wx_"+openid, nickname, openid, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		id, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("user: last insert id: %w", err)
		}
		return s.FindByID(id)
	}
	// Rows affected 0: the record was created concurrently; return it.
	return s.FindByOpenID(openid)
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
	createdAt, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return nil, fmt.Errorf("user: parse created_at %q: %w", created, err)
	}
	u.CreatedAt = createdAt
	return &u, nil
}

// Component is the user system component itself. It is registered
// with WithService so the node is activated lazily on the first
// Get[*user.Service]() instead of during assembly.
type Component struct {
	loong.Base
	Service *Service
}

func (c *Component) Build(scope *loong.Scope) error {
	cfg, err := scope.Config[Config]()
	if err != nil {
		return err
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "loong.db"
	}
	svc, err := OpenService(cfg.DBPath)
	if err != nil {
		return err
	}
	c.Service = svc
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
	loong.RegisterComponent("user", func() loong.Component { return &Component{} },
		loong.WithConfig[Config](),
		loong.WithService(func(c loong.Component) *Service { return c.(*Component).Service }),
		loong.WithDesc("user system: accounts, password auth, openid login"),
	)
}
