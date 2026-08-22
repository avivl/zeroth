package signer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

var (
	// ErrNotFound is returned when a named key does not exist.
	ErrNotFound = errors.New("signer: key not found")
	// ErrExists is returned when Create is called for a name that already has a key.
	ErrExists = errors.New("signer: key exists")
	// ErrInvalid is returned for empty names or malformed keys and signatures.
	ErrInvalid = errors.New("signer: invalid")
)

// Service signs 32-byte messages for named agent keys. A KMS
// implementation can satisfy this without changing audit or the daemon.
type Service interface {
	// Create generates a secp256k1 keypair for name. The secret stays in
	// the backend; only the x-only public key is returned.
	Create(ctx context.Context, name string) (PublicKey, error)
	// PublicKey returns the current x-only public key for name.
	PublicKey(ctx context.Context, name string) (PublicKey, error)
	// Sign produces a BIP-340 Schnorr signature over a 32-byte message.
	Sign(ctx context.Context, name string, message []byte) (Signature, error)
	// Rotate replaces the secret for name and returns the new public key.
	// Historical signatures remain verifiable against the append-only
	// registry in the store; this backend does not keep old secrets.
	Rotate(ctx context.Context, name string) (PublicKey, error)
}

// backend persists 32-byte secrets keyed by agent name.
type backend interface {
	get(ctx context.Context, name string) ([]byte, error)
	put(ctx context.Context, name string, priv []byte) error
}

// Signer is a BIP-340 signer over a secret backend.
type Signer struct {
	backend backend
	mu      sync.Mutex
}

// NewMemory returns a Signer that keeps secrets in process memory.
// Tests and callers that do not need durability use this.
func NewMemory() *Signer {
	return &Signer{backend: newMemoryBackend()}
}

var _ Service = (*Signer)(nil)

// Create implements Service.
func (s *Signer) Create(ctx context.Context, name string) (PublicKey, error) {
	if err := checkName(name); err != nil {
		return PublicKey{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.backend.get(ctx, name); err == nil {
		return PublicKey{}, fmt.Errorf("signer create %s: %w", name, ErrExists)
	} else if !errors.Is(err, ErrNotFound) {
		return PublicKey{}, fmt.Errorf("signer create %s: %w", name, err)
	}
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		return PublicKey{}, fmt.Errorf("signer create %s: %w", name, err)
	}
	if err := s.backend.put(ctx, name, priv.Serialize()); err != nil {
		return PublicKey{}, fmt.Errorf("signer create %s: %w", name, err)
	}
	return publicKeyOf(priv), nil
}

// PublicKey implements Service.
func (s *Signer) PublicKey(ctx context.Context, name string) (PublicKey, error) {
	if err := checkName(name); err != nil {
		return PublicKey{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	priv, err := s.load(ctx, name)
	if err != nil {
		return PublicKey{}, fmt.Errorf("signer public key %s: %w", name, err)
	}
	return publicKeyOf(priv), nil
}

// Sign implements Service. message must be 32 bytes (a SHA-256 digest).
func (s *Signer) Sign(ctx context.Context, name string, message []byte) (Signature, error) {
	if err := checkName(name); err != nil {
		return Signature{}, err
	}
	if len(message) != sha256.Size {
		return Signature{}, fmt.Errorf("signer sign %s: message must be %d bytes: %w", name, sha256.Size, ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	priv, err := s.load(ctx, name)
	if err != nil {
		return Signature{}, fmt.Errorf("signer sign %s: %w", name, err)
	}
	sig, err := schnorr.Sign(priv, message)
	if err != nil {
		return Signature{}, fmt.Errorf("signer sign %s: %w", name, err)
	}
	var out Signature
	copy(out[:], sig.Serialize())
	return out, nil
}

// Rotate implements Service.
func (s *Signer) Rotate(ctx context.Context, name string) (PublicKey, error) {
	if err := checkName(name); err != nil {
		return PublicKey{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.backend.get(ctx, name); err != nil {
		return PublicKey{}, fmt.Errorf("signer rotate %s: %w", name, err)
	}
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		return PublicKey{}, fmt.Errorf("signer rotate %s: %w", name, err)
	}
	if err := s.backend.put(ctx, name, priv.Serialize()); err != nil {
		return PublicKey{}, fmt.Errorf("signer rotate %s: %w", name, err)
	}
	return publicKeyOf(priv), nil
}

func (s *Signer) load(ctx context.Context, name string) (*btcec.PrivateKey, error) {
	raw, err := s.backend.get(ctx, name)
	if err != nil {
		return nil, err
	}
	priv, _ := btcec.PrivKeyFromBytes(raw)
	if priv == nil {
		return nil, fmt.Errorf("malformed secret: %w", ErrInvalid)
	}
	return priv, nil
}

func checkName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("signer: empty name: %w", ErrInvalid)
	}
	return nil
}

func publicKeyOf(priv *btcec.PrivateKey) PublicKey {
	var out PublicKey
	copy(out[:], schnorr.SerializePubKey(priv.PubKey()))
	return out
}

// PublicKey is a BIP-340 x-only secp256k1 public key (32 bytes).
type PublicKey [32]byte

// ParsePublicKey decodes a hex-encoded x-only public key.
func ParsePublicKey(raw string) (PublicKey, error) {
	b, err := decodeHex(raw, 32)
	if err != nil {
		return PublicKey{}, fmt.Errorf("signer public key: %w", err)
	}
	var pk PublicKey
	copy(pk[:], b)
	return pk, nil
}

// Hex returns the lowercase hex encoding.
func (pk PublicKey) Hex() string { return hex.EncodeToString(pk[:]) }

// Bytes returns a copy of the 32-byte x-only key.
func (pk PublicKey) Bytes() []byte { return append([]byte(nil), pk[:]...) }

// IsZero reports whether pk is the empty key.
func (pk PublicKey) IsZero() bool {
	var z PublicKey
	return pk == z
}

// Signature is a 64-byte BIP-340 Schnorr signature.
type Signature [64]byte

// ParseSignature decodes a hex-encoded BIP-340 signature.
func ParseSignature(raw string) (Signature, error) {
	b, err := decodeHex(raw, 64)
	if err != nil {
		return Signature{}, fmt.Errorf("signer signature: %w", err)
	}
	var sig Signature
	copy(sig[:], b)
	return sig, nil
}

// Hex returns the lowercase hex encoding.
func (sig Signature) Hex() string { return hex.EncodeToString(sig[:]) }

// Bytes returns a copy of the 64-byte signature.
func (sig Signature) Bytes() []byte { return append([]byte(nil), sig[:]...) }

// Verify reports whether sig is a BIP-340 signature of a 32-byte message
// under pub. It does not need the daemon or the secret.
func Verify(pub PublicKey, message []byte, sig Signature) error {
	if pub.IsZero() {
		return fmt.Errorf("signer verify: empty public key: %w", ErrInvalid)
	}
	if len(message) != sha256.Size {
		return fmt.Errorf("signer verify: message must be %d bytes: %w", sha256.Size, ErrInvalid)
	}
	pk, err := schnorr.ParsePubKey(pub[:])
	if err != nil {
		return fmt.Errorf("signer verify: public key: %w", err)
	}
	parsed, err := schnorr.ParseSignature(sig[:])
	if err != nil {
		return fmt.Errorf("signer verify: signature: %w", err)
	}
	if !parsed.Verify(message, pk) {
		return fmt.Errorf("signer verify: %w", errBadSignature)
	}
	return nil
}

var errBadSignature = errors.New("signer: bad signature")

func decodeHex(raw string, want int) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrInvalid
	}
	b, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if len(b) != want {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrInvalid, len(b), want)
	}
	return b, nil
}
