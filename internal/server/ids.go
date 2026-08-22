package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/avivl/zeroth/internal/store"
)

func newPrefixedID(prefix string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("id: %w", err)
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

func newApprovalID() (store.ApprovalID, error) {
	raw, err := newPrefixedID("ap_")
	if err != nil {
		return store.ApprovalID{}, err
	}
	return store.ParseApprovalID(raw)
}

func newCheckpointID() (store.CheckpointID, error) {
	raw, err := newPrefixedID("ck_")
	if err != nil {
		return store.CheckpointID{}, err
	}
	return store.ParseCheckpointID(raw)
}

func newGrantID() (store.GrantID, error) {
	raw, err := newPrefixedID("g_")
	if err != nil {
		return store.GrantID{}, err
	}
	return store.ParseGrantID(raw)
}
