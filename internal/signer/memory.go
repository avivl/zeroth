package signer

import (
	"context"
	"fmt"
)

type memoryBackend struct {
	keys map[string][]byte
}

func newMemoryBackend() *memoryBackend {
	return &memoryBackend{keys: make(map[string][]byte)}
}

func (m *memoryBackend) get(_ context.Context, name string) ([]byte, error) {
	priv, ok := m.keys[name]
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, ErrNotFound)
	}
	out := make([]byte, len(priv))
	copy(out, priv)
	return out, nil
}

func (m *memoryBackend) put(_ context.Context, name string, priv []byte) error {
	out := make([]byte, len(priv))
	copy(out, priv)
	m.keys[name] = out
	return nil
}
