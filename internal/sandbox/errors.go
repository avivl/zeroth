package sandbox

import "errors"

var (
	// ErrNotFound is returned when an id does not name a live sandbox.
	ErrNotFound = errors.New("sandbox: not found")
	// ErrKilled is returned when Exec (or another mutating call) runs
	// after Kill and before Stop. ExportTar still works in that window.
	ErrKilled = errors.New("sandbox: killed")
	// ErrStopped is returned after Stop. The overlay is gone.
	ErrStopped = errors.New("sandbox: stopped")
	// ErrInvalid is returned for empty argv, escaping tar paths, or
	// malformed env and egress rules.
	ErrInvalid = errors.New("sandbox: invalid")
)
