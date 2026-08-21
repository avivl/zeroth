package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/avivl/zeroth/internal/store"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

const (
	driverName    = "sqlite"
	busyTimeoutMS = 5000
)

// Store is a SQLite backend. One file, WAL mode, path configurable.
type Store struct {
	db   *sql.DB
	path string
	fsys fs.FS
	dead atomic.Bool
}

// New opens (or creates) the database at path, enables WAL, and applies
// migrations. Path must be a filesystem file; :memory: cannot use WAL.
func New(path string) (*Store, error) {
	return open(path, migrationFS())
}

func migrationFS() fs.FS {
	sub, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		panic("store sqlite: embed migrations: " + err.Error())
	}
	return sub
}

func open(path string, fsys fs.FS) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("store sqlite open: empty path")
	}
	if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return nil, fmt.Errorf("store sqlite open: %q cannot use WAL", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store sqlite mkdir: %w", err)
	}

	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=" + url.QueryEscape(fmt.Sprintf("busy_timeout(%d)", busyTimeoutMS)) +
		"&_pragma=" + url.QueryEscape("journal_mode(WAL)") +
		"&_pragma=" + url.QueryEscape("synchronous(NORMAL)") +
		"&_pragma=" + url.QueryEscape("foreign_keys(ON)")

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("store sqlite open: %w", err)
	}
	// One writer. Concurrent sessions queue here; G6 measures that wait.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db, path: path, fsys: fsys}
	if err := db.Ping(); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("store sqlite ping: %w", err)
	}
	if err := s.migrate(context.Background(), fsys); err != nil {
		_ = s.Close()
		return nil, err
	}
	mode, err := journalMode(db)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	if mode != "wal" {
		_ = s.Close()
		return nil, fmt.Errorf("store sqlite open: journal_mode %q, want wal", mode)
	}
	return s, nil
}

func journalMode(db *sql.DB) (string, error) {
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return "", fmt.Errorf("store sqlite journal_mode: %w", err)
	}
	return strings.ToLower(mode), nil
}

// JournalMode returns the active journal mode. Tests assert WAL.
func (s *Store) JournalMode() (string, error) {
	if err := s.guard(); err != nil {
		return "", err
	}
	return journalMode(s.db)
}

// Name implements [store.Store].
func (*Store) Name() string { return "sqlite" }

// Close implements [store.Store]. It is idempotent.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	if s.dead.Swap(true) {
		return nil
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("store sqlite close: %w", err)
	}
	return nil
}

func (s *Store) guard() error {
	if s == nil || s.db == nil || s.dead.Load() {
		return store.ErrClosed
	}
	return nil
}

var _ store.Store = (*Store)(nil)
