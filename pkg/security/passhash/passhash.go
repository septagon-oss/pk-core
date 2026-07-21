// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package passhash provides PlatformKit's provider-neutral password hashing
// contract. Default implementations cover long passwords (bcrypt, argon2id)
// and short high-entropy secrets like 2FA backup codes (HMAC-SHA256 with a
// server-side pepper).
//
// passhash.go owns the Hasher interface and the bcrypt cost helpers shared
// by callers that want to introspect or modify the platform-wide cost.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package passhash

import "errors"

// Hasher is the provider-neutral password hashing contract.
//
// Implementations must be safe for concurrent use. Hash returns an encoded
// representation including any salt or algorithm parameters required by
// Verify; the format is implementation-defined.
type Hasher interface {
	// Hash produces an encoded representation of the secret. Each call must
	// produce a distinct value for randomly-salted hashers (bcrypt, argon2id);
	// for deterministic hashers (HMAC-based short-secret) repeated calls with
	// the same input produce the same output.
	Hash(secret string) (string, error)

	// Verify returns nil iff secret matches the encoded hash. Non-nil errors
	// indicate either an authentic mismatch or a malformed hash; callers MUST
	// NOT distinguish the two for authentication decisions.
	Verify(secret, encoded string) error
}

// ErrMismatch is returned by Verify when the secret does not match the hash.
// Callers should treat it as opaque; do NOT include it in user-visible error
// messages.
var ErrMismatch = errors.New("passhash: secret does not match hash")

// ErrMalformedHash is returned by Verify when the encoded hash cannot be
// parsed. Callers should treat it identically to ErrMismatch for
// authentication purposes to avoid leaking format errors to attackers.
var ErrMalformedHash = errors.New("passhash: malformed encoded hash")
