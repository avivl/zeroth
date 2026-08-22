package memory

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/avivl/zeroth/internal/store"
)

// NewFactID returns a random notebook fact id.
func NewFactID() (store.MemoryID, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return store.MemoryID{}, fmt.Errorf("memory fact id: %w", err)
	}
	return store.ParseMemoryID("m_" + hex.EncodeToString(b[:]))
}

// NewProposalID returns a random proposal id.
func NewProposalID() (store.MemoryProposalID, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return store.MemoryProposalID{}, fmt.Errorf("memory proposal id: %w", err)
	}
	return store.ParseMemoryProposalID("mp_" + hex.EncodeToString(b[:]))
}
