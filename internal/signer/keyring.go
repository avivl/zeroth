package signer

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	osk "github.com/zalando/go-keyring"
)

const defaultKeyringService = "zeroth"

// NewKeyring returns a Signer that stores secrets in the OS keychain
// (macOS Keychain, libsecret, or the Windows credential manager).
func NewKeyring(service string) (*Signer, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		service = defaultKeyringService
	}
	return &Signer{backend: &keyringBackend{service: service}}, nil
}

type keyringBackend struct {
	service string
}

func (k *keyringBackend) get(_ context.Context, name string) ([]byte, error) {
	raw, err := osk.Get(k.service, keyringUser(name))
	if err != nil {
		if err == osk.ErrNotFound {
			return nil, fmt.Errorf("%s: %w", name, ErrNotFound)
		}
		return nil, fmt.Errorf("signer keyring get %s: %w", name, err)
	}
	b, err := hex.DecodeString(raw)
	if err != nil || len(b) != 32 {
		return nil, fmt.Errorf("signer keyring get %s: %w", name, ErrInvalid)
	}
	return b, nil
}

func (k *keyringBackend) put(_ context.Context, name string, priv []byte) error {
	if err := osk.Set(k.service, keyringUser(name), hex.EncodeToString(priv)); err != nil {
		return fmt.Errorf("signer keyring put %s: %w", name, err)
	}
	return nil
}

func keyringUser(name string) string {
	return "agent:" + name
}
