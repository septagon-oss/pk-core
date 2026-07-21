// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package passhash — bcrypt.go provides the BcryptHasher implementation. Cost
// is enforced at construction time to prevent silent downgrades; the OWASP
// 2024+ minimum (12) is the package floor.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package passhash

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// Bcrypt cost bounds. MinCost reflects OWASP 2024+ baseline; raising it is
// always safe. Lowering below MinCost is rejected at NewBcrypt time.
const (
	MinCost     = 12
	DefaultCost = 12
	MaxCost     = bcrypt.MaxCost
)

// BcryptHasher hashes long passwords with bcrypt at a fixed cost.
type BcryptHasher struct {
	cost int
}

// NewBcrypt constructs a BcryptHasher. Returns an error if cost is outside
// [MinCost, MaxCost].
func NewBcrypt(cost int) (*BcryptHasher, error) {
	if cost < MinCost {
		return nil, fmt.Errorf("passhash.NewBcrypt: cost %d below MinCost %d", cost, MinCost)
	}
	if cost > MaxCost {
		return nil, fmt.Errorf("passhash.NewBcrypt: cost %d above MaxCost %d", cost, MaxCost)
	}
	return &BcryptHasher{cost: cost}, nil
}

// Cost returns the configured bcrypt cost.
func (b *BcryptHasher) Cost() int { return b.cost }

// Hash satisfies Hasher.
func (b *BcryptHasher) Hash(secret string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(secret), b.cost)
	if err != nil {
		return "", fmt.Errorf("passhash.Hash: %w", err)
	}
	return string(h), nil
}

// Verify satisfies Hasher.
func (b *BcryptHasher) Verify(secret, encoded string) error {
	err := bcrypt.CompareHashAndPassword([]byte(encoded), []byte(secret))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return ErrMismatch
	case errors.Is(err, bcrypt.ErrHashTooShort):
		return ErrMalformedHash
	}
	// Other bcrypt internal errors (e.g. ErrPasswordTooLong) are treated as
	// mismatch from the auth perspective — never leak the underlying cause.
	return ErrMismatch
}

// NeedsRehash reports whether encoded was produced with a cost below the
// current configured cost. Callers can use this to opportunistically rehash
// on successful login.
func (b *BcryptHasher) NeedsRehash(encoded string) bool {
	c, err := bcrypt.Cost([]byte(encoded))
	if err != nil {
		return false
	}
	return c < b.cost
}
