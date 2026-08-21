package harness

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// ID identifies one harness run. It is a distinct named type, not a
// string and not interchangeable with session or sandbox IDs (ADR-Z-0001).
type ID struct {
	raw string
}

// ParseID returns an ID from a non-empty raw value.
func ParseID(raw string) (ID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ID{}, fmt.Errorf("harness id: empty")
	}
	return ID{raw: raw}, nil
}

// NewID returns a random harness run ID. The value is opaque; callers
// must not parse structure out of it.
func NewID() (ID, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ID{}, fmt.Errorf("harness id: %w", err)
	}
	return ParseID("hrn_" + hex.EncodeToString(b[:]))
}

// String returns the raw identifier. It is for logs and tests, not for
// mixing with other ID kinds.
func (id ID) String() string { return id.raw }

// IsZero reports whether id is the zero value.
func (id ID) IsZero() bool { return id.raw == "" }
