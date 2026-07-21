// Validates: REQ-002.
// Per: ADR-0009.
// Discipline: C-14.

package registry_test

// example_test.go provides runnable godoc examples for building immutable
// declarative registry catalogs.
//
// ADR: ADR-0005 (no silent failures), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"fmt"

	"github.com/septagon-oss/pk-core/pkg/registry"
)

// ExampleNewCatalogBuilder builds an immutable catalog keyed by string and
// looks a contribution back up. Keys are emitted in deterministic order.
func ExampleNewCatalogBuilder() {
	catalog, err := registry.NewCatalogBuilder[string, int](
		registry.WithCatalogKeyLess[string, int](func(a, b string) bool { return a < b }),
	).
		Add("beta", 2, "example").
		Add("alpha", 1, "example").
		Build()
	if err != nil {
		fmt.Println("build error:", err)
		return
	}

	fmt.Println(catalog.Keys())
	if value, ok := catalog.Lookup("alpha"); ok {
		fmt.Println("alpha =", value)
	}
	// Output:
	// [alpha beta]
	// alpha = 1
}
