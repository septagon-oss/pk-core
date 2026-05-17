package passhash_test

// passhash_test.go validates the Hasher contract shared by all randomly
// salted implementations (bcrypt, argon2id). The non-salted ShortSecret
// hasher has its own contract test in short_secret_test.go.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"testing"

	"github.com/septagon-oss/pk-core/pkg/security/passhash"
)

// SaltedHasherContract runs the three behavioral checks every randomly-salted
// Hasher must satisfy: round-trip match, wrong-secret rejection, and per-call
// salt randomness.
func SaltedHasherContract(t *testing.T, name string, h passhash.Hasher, secret string) {
	t.Helper()
	t.Run(name+"/HashThenVerifySucceeds", func(t *testing.T) {
		t.Parallel()
		hash, err := h.Hash(secret)
		if err != nil {
			t.Fatalf("Hash: %v", err)
		}
		if err := h.Verify(secret, hash); err != nil {
			t.Fatalf("Verify(correct): %v", err)
		}
	})
	t.Run(name+"/VerifyFailsForWrongSecret", func(t *testing.T) {
		t.Parallel()
		hash, err := h.Hash(secret)
		if err != nil {
			t.Fatalf("Hash: %v", err)
		}
		if err := h.Verify(secret+"x", hash); err == nil {
			t.Fatal("Verify(wrong) should fail")
		}
	})
	t.Run(name+"/HashIsSalted", func(t *testing.T) {
		t.Parallel()
		a, err := h.Hash(secret)
		if err != nil {
			t.Fatalf("Hash a: %v", err)
		}
		b, err := h.Hash(secret)
		if err != nil {
			t.Fatalf("Hash b: %v", err)
		}
		if a == b {
			t.Fatalf("two hashes of same secret should differ (salt missing)")
		}
	})
}
