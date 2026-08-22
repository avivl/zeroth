package tracker

import "errors"

var (
	// ErrNotFound is returned when a key does not name an issue.
	ErrNotFound = errors.New("tracker: not found")
	// ErrInvalid is returned for an empty key, empty comment, or malformed artifact.
	ErrInvalid = errors.New("tracker: invalid")
	// ErrUnavailable is returned when the remote tracker cannot be reached
	// after retries (resilience already applied by the driver).
	ErrUnavailable = errors.New("tracker: unavailable")
)
