// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package passhash — short_secret.go hashes short, high-entropy values like
// 2FA backup codes or recovery codes using HMAC-SHA256 with a server-side
// pepper. bcrypt and argon2id are the wrong tool for these inputs because
// their work factor is targeted at human-memorable low-entropy passwords;
// short codes are already high-entropy and need only constant-time equality
// and a pepper so the database alone cannot reveal them.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package passhash

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
)

// ShortSecretHasher hashes short codes with HMAC-SHA256(pepper, secret).
// Output is hex-encoded for storage. Two calls with the same secret produce
// the same hash (deterministic) — that is required because backup codes are
// stored once and looked up by hash on later verification.
type ShortSecretHasher struct {
	pepper []byte
}

// NewShortSecret constructs a ShortSecretHasher. The pepper must be at least
// 32 bytes; callers should generate it with crypto/rand and persist it in
// platform secrets, NOT in the database.
func NewShortSecret(pepper []byte) (*ShortSecretHasher, error) {
	if len(pepper) < 32 {
		return nil, errors.New("passhash.NewShortSecret: pepper must be at least 32 bytes")
	}
	cp := make([]byte, len(pepper))
	copy(cp, pepper)
	return &ShortSecretHasher{pepper: cp}, nil
}

// Hash satisfies Hasher. Deterministic; same input → same output.
func (s *ShortSecretHasher) Hash(secret string) (string, error) {
	m := hmac.New(sha256.New, s.pepper)
	m.Write([]byte(secret))
	return hex.EncodeToString(m.Sum(nil)), nil
}

// Verify satisfies Hasher. Constant-time comparison against the encoded hash.
func (s *ShortSecretHasher) Verify(secret, encoded string) error {
	want, err := hex.DecodeString(encoded)
	if err != nil {
		return ErrMalformedHash
	}
	m := hmac.New(sha256.New, s.pepper)
	m.Write([]byte(secret))
	if subtle.ConstantTimeCompare(m.Sum(nil), want) != 1 {
		return ErrMismatch
	}
	return nil
}
