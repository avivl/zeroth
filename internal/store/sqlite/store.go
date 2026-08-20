package sqlite

import "github.com/avivl/zeroth/internal/store"

// Store is a SQLite backend. The skeleton implements only the port identity.
type Store struct{}

// New returns a SQLite store.
func New() *Store {
	return &Store{}
}

// Name implements [store.Store].
func (*Store) Name() string { return "sqlite" }

// Close implements [store.Store].
func (*Store) Close() error { return nil }

var _ store.Store = (*Store)(nil)
