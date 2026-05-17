package architecture_test

// observability_deps_test.go enforces that the observability sub-packages do
// not import anything outside the Go standard library and pk-core itself.
// External adapters belong downstream; the OSS kernel stays dependency-free.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"go/build"
	"strings"
	"testing"
)

func TestObservabilityHasNoExternalDeps(t *testing.T) {
	t.Parallel()
	packages := []string{
		"github.com/septagon-oss/pk-core/pkg/observability",
		"github.com/septagon-oss/pk-core/pkg/observability/logger",
		"github.com/septagon-oss/pk-core/pkg/observability/metrics",
		"github.com/septagon-oss/pk-core/pkg/observability/tracing",
		"github.com/septagon-oss/pk-core/pkg/observability/health",
		"github.com/septagon-oss/pk-core/pkg/observability/guardrail",
	}
	for _, p := range packages {
		pkg, err := build.Default.Import(p, "", 0)
		if err != nil {
			t.Fatalf("import %s: %v", p, err)
		}
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, "github.com/septagon-oss/pk-core") {
				continue
			}
			if isStdLib(imp) {
				continue
			}
			t.Errorf("%s imports forbidden external dependency %q", p, imp)
		}
	}
}

func isStdLib(importPath string) bool {
	// Standard library packages have no dot in their first path segment.
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}
