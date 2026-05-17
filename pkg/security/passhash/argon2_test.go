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

func TestNewArgon2idRejectsAllFloors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mod  func(p *passhash.Argon2idParams)
	}{
		{"zero iterations", func(p *passhash.Argon2idParams) { p.Iterations = 0 }},
		{"zero parallelism", func(p *passhash.Argon2idParams) { p.Parallelism = 0 }},
		{"salt below floor", func(p *passhash.Argon2idParams) { p.SaltLength = 7 }},
		{"key below floor", func(p *passhash.Argon2idParams) { p.KeyLength = 15 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := passhash.Argon2idDefaults()
			tc.mod(&p)
			if _, err := passhash.NewArgon2id(p); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestArgon2idVerifyRejectsHostileInputs(t *testing.T) {
	t.Parallel()
	h, _ := passhash.NewArgon2id(passhash.Argon2idDefaults())
	// First produce a valid hash to splice from.
	validHash, err := h.Hash("x")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	_ = validHash // sanity reference; specific malformed payloads built below

	cases := []struct {
		name    string
		encoded string
	}{
		{"empty", ""},
		{"not a phc string", "not-phc"},
		{"wrong version", "$argon2id$v=18$m=65536,t=3,p=4$YWJjZGVmZ2g$AAAAAAAAAAAAAAAAAAAAAA"},
		{"trailing junk on version", "$argon2id$v=19extra$m=65536,t=3,p=4$YWJjZGVmZ2g$AAAAAAAAAAAAAAAAAAAAAA"},
		{"trailing junk on params", "$argon2id$v=19$m=65536,t=3,p=4,extra$YWJjZGVmZ2g$AAAAAAAAAAAAAAAAAAAAAA"},
		{"zero iterations", "$argon2id$v=19$m=65536,t=0,p=4$YWJjZGVmZ2g$AAAAAAAAAAAAAAAAAAAAAA"},
		{"zero parallelism", "$argon2id$v=19$m=65536,t=3,p=0$YWJjZGVmZ2g$AAAAAAAAAAAAAAAAAAAAAA"},
		{"memory below par*8", "$argon2id$v=19$m=1,t=3,p=4$YWJjZGVmZ2g$AAAAAAAAAAAAAAAAAAAAAA"},
		{"salt too short", "$argon2id$v=19$m=65536,t=3,p=4$YWE$AAAAAAAAAAAAAAAAAAAAAA"},
		{"key too short", "$argon2id$v=19$m=65536,t=3,p=4$YWJjZGVmZ2g$AAAA"},
		{"bad b64 salt", "$argon2id$v=19$m=65536,t=3,p=4$!!!$AAAAAAAAAAAAAAAAAAAAAA"},
		{"bad b64 key", "$argon2id$v=19$m=65536,t=3,p=4$YWJjZGVmZ2g$!!!"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Must not panic. Must return ErrMalformedHash.
			err := h.Verify("x", tc.encoded)
			if !errors.Is(err, passhash.ErrMalformedHash) {
				t.Fatalf("Verify(%s) = %v, want ErrMalformedHash", tc.name, err)
			}
		})
	}
}
