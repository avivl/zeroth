// Package sqlite is the SQLite-backed [store.Store].
//
// One file, WAL mode, path configurable. Schema changes go through Up and
// Down migrations; a migration without a Down is not done.
package sqlite
