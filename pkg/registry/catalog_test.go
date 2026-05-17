package registry

// catalog_test.go validates declarative registry catalog conflict handling,
// diagnostics, ordering, and lookup behavior.
//
// ADR: ADR-0005 (no silent failures), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"strings"
	"testing"
)

func TestCatalogBuilderRejectsDuplicateKeys(t *testing.T) {
	_, err := NewCatalogBuilder[string, int]().
		Add("route.users", 1, "user_management").
		Add("route.users", 2, "admin_management").
		Build()
	if err == nil {
		t.Fatal("Build should reject duplicate keys")
	}
	var diagnostic DiagnosticError
	if !errors.As(err, &diagnostic) {
		t.Fatalf("duplicate error type = %T; want DiagnosticError", err)
	}
	if diagnostic.Diagnostics[0].Code != CodeDuplicateKey {
		t.Fatalf("diagnostic code = %q; want %q", diagnostic.Diagnostics[0].Code, CodeDuplicateKey)
	}
	if !strings.Contains(err.Error(), "user_management") || !strings.Contains(err.Error(), "admin_management") {
		t.Fatalf("duplicate error should name sources, got %v", err)
	}
}

func TestCatalogBuilderConflictPolicies(t *testing.T) {
	first := NewCatalogBuilder[string, int](
		WithCatalogConflictPolicy[string, int](ConflictFirstWins),
	).Add("key", 1, "first").Add("key", 2, "second").MustBuild()
	if got, _ := first.Lookup("key"); got != 1 {
		t.Fatalf("ConflictFirstWins value = %d; want 1", got)
	}

	last := NewCatalogBuilder[string, int](
		WithCatalogConflictPolicy[string, int](ConflictLastWins),
	).Add("key", 1, "first").Add("key", 2, "second").MustBuild()
	if got, _ := last.Lookup("key"); got != 2 {
		t.Fatalf("ConflictLastWins value = %d; want 2", got)
	}
	if source := last.Source("key"); source != "second" {
		t.Fatalf("Source = %q; want second", source)
	}
}

func TestCatalogBuilderNormalizesValidatesAndSorts(t *testing.T) {
	catalog, err := NewCatalogBuilder[string, int](
		WithCatalogSpec[string, int](Spec{
			ID:           "core.routes",
			Owner:        OwnerCore,
			Contribution: "Route",
			Key:          "route ID",
		}),
		WithCatalogNormalizer[string, int](strings.ToLower),
		WithCatalogKeyLess[string, int](func(a, b string) bool { return a < b }),
		WithCatalogKeyValidator[string, int](func(key string) error {
			if strings.TrimSpace(key) == "" {
				return errors.New("blank key")
			}
			return nil
		}),
		WithCatalogValueValidator[string, int](func(value int) error {
			if value <= 0 {
				return errors.New("must be positive")
			}
			return nil
		}),
	).Add("B", 2, "beta").Add("A", 1, "alpha").Build()
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}

	keys := catalog.Keys()
	if strings.Join(keys, ",") != "a,b" {
		t.Fatalf("Keys = %v; want [a b]", keys)
	}
	if got, ok := catalog.Lookup("a"); !ok || got != 1 {
		t.Fatalf("Lookup(a) = %d, %v; want 1, true", got, ok)
	}
	if got, ok := catalog.Lookup("A"); !ok || got != 1 {
		t.Fatalf("Lookup(A) = %d, %v; want normalized 1, true", got, ok)
	}
	if source := catalog.Source("A"); source != "alpha" {
		t.Fatalf("Source(A) = %q; want alpha", source)
	}
	spec, ok := catalog.Spec()
	if !ok {
		t.Fatal("Spec should be present")
	}
	if spec.ID != "core.routes" || spec.Owner != OwnerCore {
		t.Fatalf("Spec = %#v; want normalized core.routes/core", spec)
	}
}

func TestCatalogBuilderValidationErrors(t *testing.T) {
	_, err := NewCatalogBuilder[string, int](
		WithCatalogKeyValidator[string, int](func(key string) error {
			return errors.New("bad key")
		}),
	).Add("bad", 1, "source").Build()
	if err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("key validation error = %v; want bad key", err)
	}

	_, err = NewCatalogBuilder[string, int](
		WithCatalogValueValidator[string, int](func(value int) error {
			return errors.New("bad value")
		}),
	).Add("key", 1, "source").Build()
	if err == nil || !strings.Contains(err.Error(), "bad value") {
		t.Fatalf("value validation error = %v; want bad value", err)
	}

	_, err = NewCatalogBuilder[string, int](
		WithCatalogConflictPolicy[string, int](ConflictPolicy(99)),
	).Build()
	if err == nil || !strings.Contains(err.Error(), "unknown conflict policy") {
		t.Fatalf("conflict policy error = %v; want unknown conflict policy", err)
	}

	_, err = NewCatalogBuilder[string, int](
		WithCatalogSpec[string, int](Spec{ID: "bad spec"}),
	).Build()
	if err == nil || !strings.Contains(err.Error(), "must not contain whitespace") {
		t.Fatalf("spec validation error = %v; want whitespace failure", err)
	}
}

func TestCatalogCopies(t *testing.T) {
	catalog := NewCatalogBuilder[string, int]().
		Add("a", 1, "source").
		MustBuild()

	all := catalog.All()
	all["b"] = 2
	if catalog.Len() != 1 {
		t.Fatalf("All should return a copy; Len = %d", catalog.Len())
	}

	keys := catalog.Keys()
	keys[0] = "mutated"
	if got := catalog.Keys()[0]; got != "a" {
		t.Fatalf("Keys should return a copy; got %q", got)
	}
}
