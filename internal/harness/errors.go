package harness

import "errors"

var (
	// ErrNotFound is returned when an id does not name a live run.
	ErrNotFound = errors.New("harness: not found")
	// ErrInvalid is returned for an empty prompt, missing workspace,
	// empty steer, or malformed env.
	ErrInvalid = errors.New("harness: invalid")
	// ErrStopped is returned after Stop. The subprocess is gone.
	ErrStopped = errors.New("harness: stopped")
)
