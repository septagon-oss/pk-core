package passhash_test

// argon2_test.go validates Argon2idHasher contract conformance and
// parameter-floor rejection at construction.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/security/passhash"
)

func TestArgon2idContract(t *testing.T) {
	t.Parallel()
	// Use smaller-than-default params to keep the test suite fast while still
	// exercising the algorithm and PHC encoding.
	p := passhash.Argon2idDefaults()
	p.Memory = 8 * 1024 // 8 MiB floor
	p.Iterations = 1
	p.Parallelism = 1
	h, err := passhash.NewArgon2id(p)
	if err != nil {
		t.Fatalf("NewArgon2id: %v", err)
	}
	SaltedHasherContract(t, "argon2id", h, "correct horse battery staple")
}

func TestNewArgon2idRejectsLowMemory(t *testing.T) {
	t.Parallel()
	p := passhash.Argon2idDefaults()
	p.Memory = 4 * 1024
	if _, err := passhash.NewArgon2id(p); err == nil {
		t.Fatal("expected error for memory below floor")
	}
}

func TestArgon2idVerifyMalformedHash(t *testing.T) {
	t.Parallel()
	h, _ := passhash.NewArgon2id(passhash.Argon2idDefaults())
	if err := h.Verify("x", "not-a-phc-string"); !errors.Is(err, passhash.ErrMalformedHash) {
		t.Fatalf("got %v, want ErrMalformedHash", err)
	}
}
