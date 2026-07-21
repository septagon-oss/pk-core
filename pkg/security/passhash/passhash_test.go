// Validates: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

package passhash_test

// passhash_test.go provides SaltedHasherContract, the shared TDD harness
// reused by bcrypt_test.go and argon2_test.go to enforce the three behaviors
// every randomly-salted Hasher must satisfy: round-trip match, wrong-secret
// rejection, and per-call salt randomness. ShortSecretHasher is a
// deterministic Hasher and has its own contract test in short_secret_test.go.
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
