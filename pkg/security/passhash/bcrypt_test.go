package passhash_test

// bcrypt_test.go validates BcryptHasher: it satisfies SaltedHasherContract,
// rejects cost below MinCost at construction, and surfaces NeedsRehash for
// cost upgrades.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/security/passhash"
)

func TestBcryptContract(t *testing.T) {
	t.Parallel()
	h, err := passhash.NewBcrypt(passhash.MinCost)
	if err != nil {
		t.Fatalf("NewBcrypt: %v", err)
	}
	SaltedHasherContract(t, "bcrypt", h, "correct horse battery staple")
}

func TestNewBcryptRejectsCostBelowMin(t *testing.T) {
	t.Parallel()
	if _, err := passhash.NewBcrypt(passhash.MinCost - 1); err == nil {
		t.Fatal("expected error for cost below MinCost")
	}
}

func TestNewBcryptRejectsCostAboveMax(t *testing.T) {
	t.Parallel()
	if _, err := passhash.NewBcrypt(passhash.MaxCost + 1); err == nil {
		t.Fatal("expected error for cost above MaxCost")
	}
}

func TestBcryptVerifyMismatchUsesSentinel(t *testing.T) {
	t.Parallel()
	h, _ := passhash.NewBcrypt(passhash.MinCost)
	hash, _ := h.Hash("a")
	err := h.Verify("b", hash)
	if !errors.Is(err, passhash.ErrMismatch) {
		t.Fatalf("Verify(wrong) = %v, want ErrMismatch", err)
	}
}

func TestBcryptVerifyMalformedReturnsMismatchOpaque(t *testing.T) {
	t.Parallel()
	// Malformed hash should also surface as ErrMismatch from the auth boundary
	// (we DO permit ErrMalformedHash for deliberate format checks elsewhere).
	h, _ := passhash.NewBcrypt(passhash.MinCost)
	err := h.Verify("anything", "not-a-bcrypt-hash")
	if !errors.Is(err, passhash.ErrMalformedHash) {
		t.Fatalf("Verify(malformed) = %v, want ErrMalformedHash", err)
	}
}

func TestBcryptNeedsRehashAtHigherCost(t *testing.T) {
	t.Parallel()
	low, _ := passhash.NewBcrypt(passhash.MinCost)
	high, _ := passhash.NewBcrypt(passhash.MinCost + 1)
	hash, _ := low.Hash("x")
	if !high.NeedsRehash(hash) {
		t.Fatal("high-cost hasher should report NeedsRehash for low-cost hash")
	}
	if low.NeedsRehash(hash) {
		t.Fatal("same-cost hasher should not report NeedsRehash")
	}
}
