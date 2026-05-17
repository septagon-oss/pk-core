// Package signature — hmac.go provides HMACSigner: an HMAC-SHA256 Signer
// with hex-encoded output. The minimum accepted key length is 32 bytes
// (256 bits) per RFC 2104 §3, which is also the SHA-256 output size — keys
// shorter than the hash output reduce the effective security of the MAC.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
)

// MinKeyLength is the minimum HMAC key length we accept (32 bytes = 256
// bits = the SHA-256 output size). Shorter keys are rejected by
// NewHMACSigner because they reduce the effective security of the MAC
// below the algorithm's natural strength.
const MinKeyLength = 32

// ErrShortKey is returned by NewHMACSigner when the caller passes a key
// shorter than MinKeyLength. Tests and callers may use errors.Is to
// detect it.
var ErrShortKey = errors.New("signature: HMAC key too short")

// HMACSigner signs payloads with HMAC-SHA256 and hex-encodes the result.
// Safe for concurrent use: the key is read-only after construction and
// hmac.New allocates a fresh hash state per call.
type HMACSigner struct {
	key []byte
}

// NewHMACSigner constructs an HMACSigner. The key is copied defensively
// so subsequent mutation of the caller's slice cannot tamper with the
// signer's state. Returns ErrShortKey if key is shorter than MinKeyLength.
func NewHMACSigner(key []byte) (*HMACSigner, error) {
	if len(key) < MinKeyLength {
		return nil, fmt.Errorf("%w: got %d bytes, need >= %d", ErrShortKey, len(key), MinKeyLength)
	}
	cp := make([]byte, len(key))
	copy(cp, key)
	return &HMACSigner{key: cp}, nil
}

// Sign satisfies Signer. Output is the hex encoding of the HMAC-SHA256
// of payload under the signer's key.
func (s *HMACSigner) Sign(payload []byte) string {
	m := hmac.New(sha256.New, s.key)
	m.Write(payload)
	return hex.EncodeToString(m.Sum(nil))
}

// Verify satisfies Signer. Constant-time. Returns false (not an error)
// when signature is not valid hex — distinguishing malformed input from
// wrong signature would leak information about the encoding to an
// attacker.
func (s *HMACSigner) Verify(payload []byte, signature string) bool {
	got, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, s.key)
	m.Write(payload)
	return subtle.ConstantTimeCompare(got, m.Sum(nil)) == 1
}
