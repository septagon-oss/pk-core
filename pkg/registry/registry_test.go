package registry

// registry_test.go validates the generic registry's lookup, fallback,
// duplicate, snapshot, and nil-safety behavior.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"strings"
	"testing"
)

func TestRegistryRegisterGetAndFallback(t *testing.T) {
	r := New(
		WithNormalizer[string, string](strings.ToLower),
		WithDefault[string, string]("fallback"),
	)
	r.Register("Input", "input_component")

	if got := r.MustGet("INPUT"); got != "input_component" {
		t.Fatalf("MustGet(INPUT) = %q; want input_component", got)
	}
	if got := r.MustGet("missing"); got != "fallback" {
		t.Fatalf("MustGet(missing) = %q; want fallback", got)
	}
}

func TestRegistryZeroValueIsUsable(t *testing.T) {
	var r Registry[string, int]

	r.Register("a", 1)
	if got := r.MustGet("a"); got != 1 {
		t.Fatalf("zero-value registry MustGet(a) = %d; want 1", got)
	}
	if err := r.RegisterIfAbsent("b", 2); err != nil {
		t.Fatalf("zero-value registry RegisterIfAbsent error = %v", err)
	}
	if got := r.Len(); got != 2 {
		t.Fatalf("zero-value registry Len = %d; want 2", got)
	}
}

func TestRegistryRegisterIfAbsent(t *testing.T) {
	r := New[string, int]()
	if err := r.RegisterIfAbsent("a", 1); err != nil {
		t.Fatalf("RegisterIfAbsent first call error = %v", err)
	}
	if err := r.RegisterIfAbsent("a", 2); err == nil {
		t.Fatal("RegisterIfAbsent duplicate should fail")
	} else {
		var duplicate ErrAlreadyRegistered[string]
		if !errors.As(err, &duplicate) {
			t.Fatalf("duplicate error type = %T; want ErrAlreadyRegistered", err)
		}
	}
	if got := r.MustGet("a"); got != 1 {
		t.Fatalf("duplicate registration changed value to %d; want 1", got)
	}
}

func TestRegistryDeleteAndCopies(t *testing.T) {
	r := New[string, int]()
	r.Register("a", 1)
	r.Register("b", 2)

	all := r.All()
	all["c"] = 3
	if r.Len() != 2 {
		t.Fatalf("All should return a copy; Len = %d", r.Len())
	}
	if !r.Delete("a") {
		t.Fatal("Delete(a) should report true")
	}
	if r.Has("a") {
		t.Fatal("Delete(a) should remove key")
	}
	if r.Delete("missing") {
		t.Fatal("Delete(missing) should report false")
	}
}

func TestRegistryRangeUsesSnapshot(t *testing.T) {
	r := New[string, int]()
	r.Register("a", 1)

	called := 0
	r.Range(func(key string, value int) bool {
		called++
		r.Register("b", 2)
		return true
	})

	if called != 1 {
		t.Fatalf("Range callback count = %d; want 1 snapshot entry", called)
	}
	if got := r.Len(); got != 2 {
		t.Fatalf("Len after callback registration = %d; want 2", got)
	}
}

func TestSortedKeys(t *testing.T) {
	r := New[string, int]()
	r.Register("c", 3)
	r.Register("a", 1)
	r.Register("b", 2)

	keys := SortedKeys(r, func(a, b string) bool { return a < b })
	if strings.Join(keys, ",") != "a,b,c" {
		t.Fatalf("SortedKeys = %v; want [a b c]", keys)
	}
}

func TestIsNil(t *testing.T) {
	var p *int
	if !IsNil(p) {
		t.Fatal("typed nil pointer should be nil")
	}
	if IsNil(1) {
		t.Fatal("int should not be nil")
	}
}
