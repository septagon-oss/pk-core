// Package passhash — argon2.go provides Argon2idHasher. Parameters follow
// OWASP 2024+ guidance (memory 64MiB, iterations 3, parallelism 4, key length
// 32, salt length 16). Encoded format is the standard PHC string format:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package passhash

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2idParams configures the argon2id algorithm. Defaults are OWASP 2024+.
type Argon2idParams struct {
	Memory      uint32 // KiB
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// Argon2idDefaults returns the recommended baseline parameters.
func Argon2idDefaults() Argon2idParams {
	return Argon2idParams{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 4,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// Argon2idHasher hashes long passwords with argon2id at fixed parameters.
type Argon2idHasher struct {
	params Argon2idParams
}

// NewArgon2id constructs an Argon2idHasher.
func NewArgon2id(p Argon2idParams) (*Argon2idHasher, error) {
	if p.Memory < 8*1024 {
		return nil, fmt.Errorf("passhash.NewArgon2id: memory %d KiB below 8 MiB floor", p.Memory)
	}
	if p.Iterations < 1 {
		return nil, errors.New("passhash.NewArgon2id: iterations must be >= 1")
	}
	if p.Parallelism < 1 {
		return nil, errors.New("passhash.NewArgon2id: parallelism must be >= 1")
	}
	if p.SaltLength < 8 {
		return nil, fmt.Errorf("passhash.NewArgon2id: saltLength %d below 8-byte floor", p.SaltLength)
	}
	if p.KeyLength < 16 {
		return nil, fmt.Errorf("passhash.NewArgon2id: keyLength %d below 16-byte floor", p.KeyLength)
	}
	return &Argon2idHasher{params: p}, nil
}

// Hash satisfies Hasher.
func (a *Argon2idHasher) Hash(secret string) (string, error) {
	salt := make([]byte, a.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("passhash.argon2id.Hash: read salt: %w", err)
	}
	hash := argon2.IDKey([]byte(secret), salt, a.params.Iterations, a.params.Memory, a.params.Parallelism, a.params.KeyLength)
	b64salt := base64.RawStdEncoding.EncodeToString(salt)
	b64hash := base64.RawStdEncoding.EncodeToString(hash)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, a.params.Memory, a.params.Iterations, a.params.Parallelism, b64salt, b64hash), nil
}

// Verify satisfies Hasher.
func (a *Argon2idHasher) Verify(secret, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return ErrMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return ErrMalformedHash
	}
	var memory, iters uint32
	var par uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iters, &par); err != nil {
		return ErrMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return ErrMalformedHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return ErrMalformedHash
	}
	got := argon2.IDKey([]byte(secret), salt, iters, memory, par, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}
