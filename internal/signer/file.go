package signer

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/scrypt"
)

const (
	fileVersion = 1
	scryptN     = 1 << 15
	scryptR     = 8
	scryptP     = 1
	scryptKey   = 32
	scryptSalt  = 16
)

// NewFile returns a Signer that stores AES-256-GCM encrypted secrets at path.
// The wrapping key is derived from passphrase with scrypt. An empty
// passphrase is allowed for local single-player use; the file is still
// encrypted (unique salt) and created mode 0600.
func NewFile(path, passphrase string) (*Signer, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("signer file: empty path: %w", ErrInvalid)
	}
	fb := &fileBackend{path: path, pass: []byte(passphrase)}
	if err := fb.init(); err != nil {
		return nil, err
	}
	return &Signer{backend: fb}, nil
}

type fileBackend struct {
	path string
	pass []byte
	wrap []byte
	salt []byte
	keys map[string][]byte
}

const fileCanary = "zeroth"

type fileDoc struct {
	Version int               `json:"v"`
	KDF     string            `json:"kdf"`
	N       int               `json:"n"`
	R       int               `json:"r"`
	P       int               `json:"p"`
	Salt    string            `json:"salt"`
	Check   string            `json:"check"`
	Keys    map[string]string `json:"keys"`
}

func (f *fileBackend) init() error {
	f.keys = make(map[string][]byte)
	body, err := os.ReadFile(f.path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("signer file read %s: %w", f.path, err)
		}
		salt := make([]byte, scryptSalt)
		if _, err := rand.Read(salt); err != nil {
			return fmt.Errorf("signer file salt: %w", err)
		}
		wrap, err := derive(f.pass, salt)
		if err != nil {
			return err
		}
		f.salt = salt
		f.wrap = wrap
		return f.flush()
	}
	var doc fileDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("signer file parse %s: %w", f.path, err)
	}
	if doc.Version != fileVersion || doc.KDF != "scrypt" {
		return fmt.Errorf("signer file %s: unsupported version: %w", f.path, ErrInvalid)
	}
	salt, err := hex.DecodeString(doc.Salt)
	if err != nil || len(salt) != scryptSalt {
		return fmt.Errorf("signer file %s: bad salt: %w", f.path, ErrInvalid)
	}
	wrap, err := derive(f.pass, salt)
	if err != nil {
		return err
	}
	f.salt = salt
	f.wrap = wrap
	if doc.Check == "" {
		return fmt.Errorf("signer file %s: missing canary: %w", f.path, ErrInvalid)
	}
	plain, err := decryptSecret(wrap, doc.Check)
	if err != nil || string(plain) != fileCanary {
		return fmt.Errorf("signer file %s: %w (wrong passphrase?)", f.path, ErrInvalid)
	}
	for name, blob := range doc.Keys {
		priv, err := decryptSecret(wrap, blob)
		if err != nil {
			return fmt.Errorf("signer file %s: decrypt %s: %w (wrong passphrase?)", f.path, name, err)
		}
		f.keys[name] = priv
	}
	return nil
}

func (f *fileBackend) get(_ context.Context, name string) ([]byte, error) {
	priv, ok := f.keys[name]
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, ErrNotFound)
	}
	out := make([]byte, len(priv))
	copy(out, priv)
	return out, nil
}

func (f *fileBackend) put(_ context.Context, name string, priv []byte) error {
	out := make([]byte, len(priv))
	copy(out, priv)
	f.keys[name] = out
	if err := f.flush(); err != nil {
		return err
	}
	return nil
}

func (f *fileBackend) flush() error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return fmt.Errorf("signer file mkdir: %w", err)
	}
	check, err := encryptSecret(f.wrap, []byte(fileCanary))
	if err != nil {
		return fmt.Errorf("signer file canary: %w", err)
	}
	doc := fileDoc{
		Version: fileVersion,
		KDF:     "scrypt",
		N:       scryptN,
		R:       scryptR,
		P:       scryptP,
		Salt:    hex.EncodeToString(f.salt),
		Check:   check,
		Keys:    make(map[string]string, len(f.keys)),
	}
	for name, priv := range f.keys {
		blob, err := encryptSecret(f.wrap, priv)
		if err != nil {
			return fmt.Errorf("signer file encrypt %s: %w", name, err)
		}
		doc.Keys[name] = blob
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("signer file marshal: %w", err)
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("signer file write: %w", err)
	}
	if err := os.Rename(tmp, f.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("signer file rename: %w", err)
	}
	return nil
}

func derive(pass, salt []byte) ([]byte, error) {
	key, err := scrypt.Key(pass, salt, scryptN, scryptR, scryptP, scryptKey)
	if err != nil {
		return nil, fmt.Errorf("signer file kdf: %w", err)
	}
	return key, nil
}

func encryptSecret(wrap, priv []byte) (string, error) {
	block, err := aes.NewCipher(wrap)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, priv, nil)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return hex.EncodeToString(out), nil
}

func decryptSecret(wrap []byte, blob string) ([]byte, error) {
	raw, err := hex.DecodeString(blob)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	block, err := aes.NewCipher(wrap)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns+gcm.Overhead() {
		return nil, ErrInvalid
	}
	plain, err := gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return nil, err
	}
	return plain, nil
}
