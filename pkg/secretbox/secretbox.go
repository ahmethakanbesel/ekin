// Package secretbox encrypts small secrets (OAuth refresh tokens, API
// tokens) at rest with AES-256-GCM, and signs short-lived state values.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Box seals and opens secrets with one key.
type Box struct {
	aead cipher.AEAD
	mac  []byte
}

// New derives the keys from secret (any length, at least 32 bytes of
// entropy recommended).
func New(secret []byte) (*Box, error) {
	if len(secret) < 16 {
		return nil, errors.New("secretbox: secret is too short")
	}
	enc := sha256.Sum256(append([]byte("kurgu-enc:"), secret...))
	mac := sha256.Sum256(append([]byte("kurgu-mac:"), secret...))
	block, err := aes.NewCipher(enc[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead, mac: mac[:]}, nil
}

// LoadOrCreate reads the secret from path, creating a random one (mode
// 0600) when the file does not exist.
func LoadOrCreate(path string) (*Box, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-chosen path
	if errors.Is(err, os.ErrNotExist) {
		buf := make([]byte, 32)
		_, _ = rand.Read(buf)
		raw = []byte(base64.RawURLEncoding.EncodeToString(buf))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return nil, fmt.Errorf("secretbox: write key: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("secretbox: read key: %w", err)
	}
	return New([]byte(strings.TrimSpace(string(raw))))
}

// Seal encrypts plain.
func (b *Box) Seal(plain string) string {
	nonce := make([]byte, b.aead.NonceSize())
	_, _ = rand.Read(nonce)
	return base64.RawURLEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), nil))
}

// Open decrypts what Seal produced.
func (b *Box) Open(sealed string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", errors.New("secretbox: malformed value")
	}
	plain, err := b.aead.Open(nil, raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():], nil)
	if err != nil {
		return "", errors.New("secretbox: cannot decrypt (was the key changed?)")
	}
	return string(plain), nil
}

// Sign returns value with an HMAC tag appended.
func (b *Box) Sign(value string) string {
	m := hmac.New(sha256.New, b.mac)
	m.Write([]byte(value))
	return value + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Verify returns the value of a signed string, or an error if the tag is
// wrong.
func (b *Box) Verify(signed string) (string, error) {
	i := strings.LastIndexByte(signed, '.')
	if i < 0 {
		return "", errors.New("secretbox: unsigned value")
	}
	value := signed[:i]
	if !hmac.Equal([]byte(b.Sign(value)), []byte(signed)) {
		return "", errors.New("secretbox: bad signature")
	}
	return value, nil
}
