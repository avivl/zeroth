package store

import "errors"

var (
	// ErrNotFound is returned when a Get or Update names a missing row.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict is returned when a Create hits a unique constraint.
	ErrConflict = errors.New("store: conflict")
	// ErrInvalid is returned for empty batches, bad cursors, or missing fields.
	ErrInvalid = errors.New("store: invalid")
	// ErrClosed is returned after Close.
	ErrClosed = errors.New("store: closed")
)
