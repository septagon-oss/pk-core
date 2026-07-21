// Validates: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

package passhash_test

// short_secret_test.go validates ShortSecretHasher: round-trip, deterministic
// output for same input, mismatch detection, pepper-length floor.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/security/passhash"
)

func TestShortSecretRoundTrip(t *testing.T) {
	t.Parallel()
	pepper := make([]byte, 32)
	for i := range pepper {
		pepper[i] = byte(i)
	}
	h, err := passhash.NewShortSecret(pepper)
	if err != nil {
		t.Fatalf("NewShortSecret: %v", err)
	}
	hash, err := h.Hash("BACKUP-12345")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if err := h.Verify("BACKUP-12345", hash); err != nil {
		t.Fatalf("Verify(correct): %v", err)
	}
}

func TestShortSecretIsDeterministic(t *testing.T) {
	t.Parallel()
	pepper := make([]byte, 32)
	h, _ := passhash.NewShortSecret(pepper)
	a, _ := h.Hash("BACKUP-12345")
	b, _ := h.Hash("BACKUP-12345")
	if a != b {
		t.Fatalf("ShortSecret must be deterministic; got %q vs %q", a, b)
	}
}

func TestShortSecretRejectsWrongSecret(t *testing.T) {
	t.Parallel()
	pepper := make([]byte, 32)
	h, _ := passhash.NewShortSecret(pepper)
	hash, _ := h.Hash("BACKUP-12345")
	if err := h.Verify("BACKUP-12346", hash); !errors.Is(err, passhash.ErrMismatch) {
		t.Fatalf("Verify(wrong) = %v, want ErrMismatch", err)
	}
}

func TestShortSecretRejectsShortPepper(t *testing.T) {
	t.Parallel()
	pepper := make([]byte, 16)
	if _, err := passhash.NewShortSecret(pepper); err == nil {
		t.Fatal("expected error for pepper < 32 bytes")
	}
}

func TestShortSecretVerifyMalformed(t *testing.T) {
	t.Parallel()
	pepper := make([]byte, 32)
	h, _ := passhash.NewShortSecret(pepper)
	if err := h.Verify("x", "not-hex-bytes!!"); !errors.Is(err, passhash.ErrMalformedHash) {
		t.Fatalf("Verify(malformed) = %v, want ErrMalformedHash", err)
	}
}
