package signer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMemorySignVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	s := NewMemory()
	ctx := t.Context()
	pub, err := s.Create(ctx, "a_default")
	if err != nil {
		t.Fatal(err)
	}
	if pub.IsZero() {
		t.Fatal("empty public key")
	}
	msg := sha256.Sum256([]byte("zeroth-audit-v1"))
	sig, err := s.Sign(ctx, "a_default", msg[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, msg[:], sig); err != nil {
		t.Fatalf("verify: %v", err)
	}
	msg[0] ^= 1
	if err := Verify(pub, msg[:], sig); err == nil {
		t.Fatal("tampered message verified")
	}
}

func TestCreateRejectsDuplicate(t *testing.T) {
	t.Parallel()
	s := NewMemory()
	ctx := t.Context()
	if _, err := s.Create(ctx, "agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, "agent"); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestRotateChangesKeyAndOldSignatureStillVerifies(t *testing.T) {
	t.Parallel()
	s := NewMemory()
	ctx := t.Context()
	oldPub, err := s.Create(ctx, "agent")
	if err != nil {
		t.Fatal(err)
	}
	msg := sha256.Sum256([]byte("before"))
	oldSig, err := s.Sign(ctx, "agent", msg[:])
	if err != nil {
		t.Fatal(err)
	}
	newPub, err := s.Rotate(ctx, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(oldPub[:], newPub[:]) {
		t.Fatal("rotate returned the same public key")
	}
	if err := Verify(oldPub, msg[:], oldSig); err != nil {
		t.Fatalf("historical signature: %v", err)
	}
	newSig, err := s.Sign(ctx, "agent", msg[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(newPub, msg[:], newSig); err != nil {
		t.Fatal(err)
	}
	if err := Verify(newPub, msg[:], oldSig); err == nil {
		t.Fatal("old signature verified under new key")
	}
}

func TestSignRequires32ByteMessage(t *testing.T) {
	t.Parallel()
	s := NewMemory()
	ctx := t.Context()
	if _, err := s.Create(ctx, "agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sign(ctx, "agent", []byte("short")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short message: %v", err)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	t.Parallel()
	if _, err := ParsePublicKey("zz"); err == nil {
		t.Fatal("bad hex pubkey")
	}
	if _, err := ParsePublicKey("aa"); err == nil {
		t.Fatal("short pubkey")
	}
	if _, err := ParseSignature(""); err == nil {
		t.Fatal("empty sig")
	}
}

func TestFileBackendRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "zeroth.keys")
	ctx := t.Context()
	s, err := NewFile(path, "passphrase")
	if err != nil {
		t.Fatal(err)
	}
	pub, err := s.Create(ctx, "a_1")
	if err != nil {
		t.Fatal(err)
	}
	msg := sha256.Sum256([]byte("file"))
	sig, err := s.Sign(ctx, "a_1", msg[:])
	if err != nil {
		t.Fatal(err)
	}
	s2, err := NewFile(path, "passphrase")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.PublicKey(ctx, "a_1")
	if err != nil {
		t.Fatal(err)
	}
	if got != pub {
		t.Fatalf("pubkey %s vs %s", got.Hex(), pub.Hex())
	}
	if err := Verify(got, msg[:], sig); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFile(path, "wrong"); err == nil {
		t.Fatal("wrong passphrase opened file")
	}
}

func TestFileModeIsPrivate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "zeroth.keys")
	if _, err := NewFile(path, "x"); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o, want 0600", st.Mode().Perm())
	}
}

func TestHexRoundTrip(t *testing.T) {
	t.Parallel()
	s := NewMemory()
	pub, err := s.Create(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParsePublicKey(pub.Hex())
	if err != nil || parsed != pub {
		t.Fatalf("parse pubkey: %v %s", err, parsed.Hex())
	}
	msg := sha256.Sum256([]byte("h"))
	sig, err := s.Sign(t.Context(), "a", msg[:])
	if err != nil {
		t.Fatal(err)
	}
	ps, err := ParseSignature(sig.Hex())
	if err != nil || ps != sig {
		t.Fatalf("parse sig: %v", err)
	}
	if hex.DecodedLen(len(pub.Hex())) != 32 {
		t.Fatal("pubkey hex length")
	}
}

func TestOpenMemoryAndFile(t *testing.T) {
	t.Parallel()
	if _, err := Open(Options{Memory: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Options{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty open: %v", err)
	}
	path := filepath.Join(t.TempDir(), "k")
	s, err := Open(Options{KeyFile: path, Passphrase: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(t.Context(), "n"); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyNameRejected(t *testing.T) {
	t.Parallel()
	s := NewMemory()
	if _, err := s.Create(t.Context(), "  "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty name: %v", err)
	}
}
