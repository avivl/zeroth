package plan

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// ID is a plan identifier. It is a distinct named type, not a string
// and not interchangeable with other ID kinds (ADR-Z-0001).
type ID struct {
	raw string
}

// ParseID returns an ID from a non-empty raw value.
func ParseID(raw string) (ID, error) {
	if raw == "" {
		return ID{}, fmt.Errorf("plan id: empty")
	}
	return ID{raw: raw}, nil
}

// NewID returns a random plan ID. The value is opaque; callers must not
// parse structure out of it.
func NewID() (ID, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ID{}, fmt.Errorf("plan id: %w", err)
	}
	return ParseID("p_" + hex.EncodeToString(b[:]))
}

// String returns the raw identifier. It is for logs, the store, and the
// API, not for mixing with other ID kinds.
func (id ID) String() string { return id.raw }

// IsZero reports whether id is the zero value.
func (id ID) IsZero() bool { return id.raw == "" }
