package session

import "fmt"

// ID is a session identifier. It is a distinct named type, not a
// string and not interchangeable with sandbox handle IDs.
type ID struct {
	raw string
}

// ParseID returns an ID from a non-empty raw value.
func ParseID(raw string) (ID, error) {
	if raw == "" {
		return ID{}, fmt.Errorf("session id: empty")
	}
	return ID{raw: raw}, nil
}

// String returns the raw identifier. It is for logs and tests, not
// for mixing with other ID kinds.
func (id ID) String() string { return id.raw }

// IsZero reports whether id is the zero value.
func (id ID) IsZero() bool { return id.raw == "" }
