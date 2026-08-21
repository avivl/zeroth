package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

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

// NewID returns a random session ID. The value is opaque; callers
// must not parse structure out of it.
func NewID() (ID, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ID{}, fmt.Errorf("session id: %w", err)
	}
	return ParseID("s_" + hex.EncodeToString(b[:]))
}

// String returns the raw identifier. It is for logs and tests, not
// for mixing with other ID kinds.
func (id ID) String() string { return id.raw }

// IsZero reports whether id is the zero value.
func (id ID) IsZero() bool { return id.raw == "" }
