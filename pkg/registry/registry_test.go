// Validates: REQ-002.
// Per: ADR-0009.
// Discipline: C-14.

package registry

import (
	"errors"
	"strings"
	"testing"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	r := New(WithNormalizer[string, string](strings.ToLower))
	r.Register("Input", "input_component")

	if got, ok := r.Get("INPUT"); !ok || got != "input_component" {
		t.Fatalf("Get(INPUT) = %q, %v; want input_component, true", got, ok)
	}
	if got, ok := r.Get("missing"); ok || got != "" {
		t.Fatalf("Get(missing) = %q, %v; want empty, false", got, ok)
	}
}

func TestRegistryZeroValueIsUsable(t *testing.T) {
	var r Registry[string, int]

	r.Register("a", 1)
	if got, ok := r.Get("a"); !ok || got != 1 {
		t.Fatalf("zero-value registry Get(a) = %d, %v; want 1, true", got, ok)
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
	if got, ok := r.Get("a"); !ok || got != 1 {
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

func TestRegistrySeqUsesSnapshot(t *testing.T) {
	r := New[string, int]()
	r.Register("a", 1)
	r.Register("b", 2)

	seen := map[string]int{}
	for key, value := range r.Seq() {
		seen[key] = value
		r.Register("c", 3)
	}

	if len(seen) != 2 {
		t.Fatalf("Seq visited %d entries; want snapshot size 2", len(seen))
	}
	if seen["a"] != 1 || seen["b"] != 2 {
		t.Fatalf("Seq snapshot = %v; want a=1 and b=2", seen)
	}
	if got := r.Len(); got != 3 {
		t.Fatalf("Len after Seq callback registration = %d; want 3", got)
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
