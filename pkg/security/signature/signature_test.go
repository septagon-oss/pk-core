// Validates: REQ-SECURITY-001.
// Per: ADR-0029.
// Discipline: C-14.

package signature_test

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/security/signature"
)

// goodKey returns a 32-byte key suitable for NewHMACSigner.
func goodKey() []byte {
	k := make([]byte, signature.MinKeyLength)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

func TestNewHMACSignerRejectsShortKey(t *testing.T) {
	for _, n := range []int{0, 1, 15, signature.MinKeyLength - 1} {
		key := make([]byte, n)
		_, err := signature.NewHMACSigner(key)
		if err == nil {
			t.Errorf("NewHMACSigner(len=%d): err = nil, want ErrShortKey", n)
			continue
		}
		if !errors.Is(err, signature.ErrShortKey) {
			t.Errorf("NewHMACSigner(len=%d): err = %v, want errors.Is(err, ErrShortKey) == true", n, err)
		}
	}
}

func TestNewHMACSignerAcceptsExactMinKey(t *testing.T) {
	s, err := signature.NewHMACSigner(make([]byte, signature.MinKeyLength))
	if err != nil {
		t.Fatalf("NewHMACSigner(len=%d): %v", signature.MinKeyLength, err)
	}
	if s == nil {
		t.Fatal("signer is nil")
	}
}

func TestHMACSignerRoundTrip(t *testing.T) {
	s, err := signature.NewHMACSigner(goodKey())
	if err != nil {
		t.Fatalf("NewHMACSigner: %v", err)
	}
	payload := []byte("hello world")
	sig := s.Sign(payload)
	if sig == "" {
		t.Fatal("Sign returned empty string")
	}
	if !s.Verify(payload, sig) {
		t.Errorf("Verify returned false for matching payload+sig")
	}
}

func TestHMACSignerIsDeterministic(t *testing.T) {
	s, err := signature.NewHMACSigner(goodKey())
	if err != nil {
		t.Fatalf("NewHMACSigner: %v", err)
	}
	a := s.Sign([]byte("p"))
	b := s.Sign([]byte("p"))
	if a != b {
		t.Errorf("non-deterministic: Sign(\"p\") returned %q then %q", a, b)
	}
}

func TestHMACSignerRejectsTamperedPayload(t *testing.T) {
	s, err := signature.NewHMACSigner(goodKey())
	if err != nil {
		t.Fatalf("NewHMACSigner: %v", err)
	}
	sig := s.Sign([]byte("a"))
	if s.Verify([]byte("b"), sig) {
		t.Errorf("Verify accepted sig of \"a\" against payload \"b\"")
	}
}

func TestHMACSignerRejectsWrongKey(t *testing.T) {
	keyA := goodKey()
	keyB := goodKey()
	keyB[0] ^= 0xff

	a, err := signature.NewHMACSigner(keyA)
	if err != nil {
		t.Fatalf("NewHMACSigner(A): %v", err)
	}
	b, err := signature.NewHMACSigner(keyB)
	if err != nil {
		t.Fatalf("NewHMACSigner(B): %v", err)
	}

	sig := a.Sign([]byte("payload"))
	if b.Verify([]byte("payload"), sig) {
		t.Errorf("Verify with wrong key accepted A's signature")
	}
}

func TestHMACSignerRejectsMalformedSignature(t *testing.T) {
	s, err := signature.NewHMACSigner(goodKey())
	if err != nil {
		t.Fatalf("NewHMACSigner: %v", err)
	}
	cases := []string{
		"not-hex-bytes",
		"zzzz",
		"abc", // odd-length hex
		"",
		"deadbeefz", // looks hex until last char
	}
	for _, c := range cases {
		// Verify must not panic on malformed input and must return false.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Verify(%q) panicked: %v", c, r)
				}
			}()
			if s.Verify([]byte("payload"), c) {
				t.Errorf("Verify(%q) returned true, want false", c)
			}
		}()
	}
}

func TestHMACSignerRejectsTruncatedSignature(t *testing.T) {
	s, err := signature.NewHMACSigner(goodKey())
	if err != nil {
		t.Fatalf("NewHMACSigner: %v", err)
	}
	sig := s.Sign([]byte("payload"))
	// Cut to half the bytes — still valid hex, wrong length.
	short := sig[:len(sig)/2]
	if s.Verify([]byte("payload"), short) {
		t.Errorf("Verify accepted truncated signature %q", short)
	}
}

func TestHMACSignerKeyIsDefensivelyCopied(t *testing.T) {
	key := goodKey()
	saved := make([]byte, len(key))
	copy(saved, key)

	s, err := signature.NewHMACSigner(key)
	if err != nil {
		t.Fatalf("NewHMACSigner: %v", err)
	}
	sigBefore := s.Sign([]byte("payload"))

	// Mutate the caller's slice. If NewHMACSigner did not copy, the
	// signer's internal key state is now corrupt and Sign produces a
	// different output.
	for i := range key {
		key[i] = 0xaa
	}
	sigAfter := s.Sign([]byte("payload"))

	if sigBefore != sigAfter {
		t.Errorf("Sign output changed after caller mutated key slice: before=%q after=%q", sigBefore, sigAfter)
	}
	if !s.Verify([]byte("payload"), sigBefore) {
		t.Errorf("Verify failed for signature produced before key mutation — signer key was not copied")
	}
	if bytes.Equal(key, saved) {
		t.Fatal("test bug: key slice was not actually mutated")
	}
}

func TestHMACSignerOutputIsHex(t *testing.T) {
	s, err := signature.NewHMACSigner(goodKey())
	if err != nil {
		t.Fatalf("NewHMACSigner: %v", err)
	}
	sig := s.Sign([]byte("payload"))
	// SHA-256 produces 32 bytes, hex-encoded = 64 chars.
	if len(sig) != 64 {
		t.Errorf("signature length = %d, want 64 hex chars", len(sig))
	}
	const hexChars = "0123456789abcdef"
	for _, c := range sig {
		if !strings.ContainsRune(hexChars, c) {
			t.Errorf("signature contains non-hex rune %q", c)
			break
		}
	}
}

// TestHMACSignerIsConcurrencySafe drives many goroutines through Sign and
// Verify on a single signer to exercise the documented "safe for
// concurrent use" guarantee. Run with `go test -race`.
func TestHMACSignerIsConcurrencySafe(t *testing.T) {
	s, err := signature.NewHMACSigner(goodKey())
	if err != nil {
		t.Fatalf("NewHMACSigner: %v", err)
	}
	payload := []byte("payload")
	want := s.Sign(payload)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 1000 {
				if got := s.Sign(payload); got != want {
					t.Errorf("concurrent Sign drift: got %q want %q", got, want)
					return
				}
				if !s.Verify(payload, want) {
					t.Error("concurrent Verify returned false")
					return
				}
			}
		})
	}
	wg.Wait()
}
