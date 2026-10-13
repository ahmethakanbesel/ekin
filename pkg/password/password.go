// Package password hashes passwords with argon2id and verifies them in
// constant time. Hashes are self-describing PHC strings, so parameters
// can be raised later without invalidating stored hashes.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params are argon2id cost parameters.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
}

// Default is the OWASP minimum (19 MiB, 2 passes, 1 lane): about 30 ms on
// one VPS core and small enough not to squeeze the 4 GB host.
var Default = Params{Memory: 19 * 1024, Time: 2, Threads: 1}

const (
	saltLen = 16
	keyLen  = 32
)

// ErrMalformed means a stored hash is not a PHC argon2id string.
var ErrMalformed = errors.New("password: malformed hash")

// Hasher hashes with fixed parameters and verifies hashes made with any.
type Hasher struct {
	p Params
}

// New returns a Hasher with p.
func New(p Params) *Hasher { return &Hasher{p: p} }

// Hash returns the PHC string for plain.
func (h *Hasher) Hash(plain string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: salt: %w", err)
	}
	key := argon2.IDKey([]byte(plain), salt, h.p.Time, h.p.Memory, h.p.Threads, keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.p.Memory, h.p.Time, h.p.Threads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify reports whether plain matches encoded, using the parameters
// recorded in encoded.
func (h *Hasher) Verify(encoded, plain string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, ErrMalformed
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrMalformed
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return false, ErrMalformed
	}
	if p.Memory == 0 || p.Time == 0 || p.Threads == 0 || p.Memory > 1<<20 || p.Time > 16 {
		return false, ErrMalformed
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false, ErrMalformed
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > 64 {
		return false, ErrMalformed
	}
	got := argon2.IDKey([]byte(plain), salt, p.Time, p.Memory, p.Threads, uint32(len(want))) //nolint:gosec // len(want) <= 64, checked above
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
