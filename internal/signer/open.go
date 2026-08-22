package signer

import (
	"fmt"
	"strings"
)

// Options select a stage-1 secret backend. A later KMS backend is a
// different Service implementation, not a new field here.
type Options struct {
	// KeyFile is the encrypted file path. Used when Keyring is false, or
	// when the OS keychain is unavailable.
	KeyFile string
	// Passphrase wraps KeyFile. Empty is allowed for local use.
	Passphrase string
	// Keyring requests the OS keychain. If Set fails later, callers should
	// fall back to KeyFile; Open itself only constructs the backend.
	Keyring bool
	// Service is the keychain service name. Empty means "zeroth".
	Service string
	// Memory keeps secrets in process. Tests use this.
	Memory bool
}

// Open constructs a Signer from opts. Memory wins when set. Keyring is
// constructed when requested; the caller is responsible for falling back
// to a file if the keychain is missing at first use.
func Open(opts Options) (*Signer, error) {
	if opts.Memory {
		return NewMemory(), nil
	}
	if opts.Keyring {
		return NewKeyring(opts.Service)
	}
	path := strings.TrimSpace(opts.KeyFile)
	if path == "" {
		return nil, fmt.Errorf("signer open: empty key file: %w", ErrInvalid)
	}
	return NewFile(path, opts.Passphrase)
}
