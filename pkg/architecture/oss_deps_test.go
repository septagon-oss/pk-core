// Validates: REQ-002.
// Per: ADR-0009.
// Discipline: C-14.

package architecture_test

// oss_deps_test.go is the central allowed-deps fitness gate for pk-core.
//
// Rule: every package under pk-core/pkg/ may import:
//   - the Go standard library
//   - any other pk-core package
//   - any module in the AllowedExternalDeps whitelist below
//
// Anything else is a v0.1.0 contract violation. The whitelist exists because a
// handful of cryptographic and crypto-adjacent operations (bcrypt, argon2id)
// cannot be implemented from stdlib alone, and `golang.org/x/crypto` is the
// Go-team-maintained, security-audited canonical source.
//
// To add an entry: extend AllowedExternalDeps with the import path and a
// one-line comment documenting WHY pk-core needs it. The bar is "this cannot
// be reasonably implemented from stdlib AND is widely vendored in the Go
// ecosystem AND is maintained by a trusted upstream."
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"go/build"
	"strings"
	"testing"
)

// AllowedExternalDeps enumerates the import paths pk-core packages may use
// outside the standard library. Keys are exact package paths.
var AllowedExternalDeps = map[string]string{
	"golang.org/x/crypto/bcrypt": "bcrypt password hashing; cannot be implemented from stdlib alone; Go-team maintained",
	"golang.org/x/crypto/argon2": "argon2id password hashing; cannot be implemented from stdlib alone; Go-team maintained",
}

// pkCorePackages lists every public pkg/ leaf in pk-core. Tests that walk
// imports use this list as the authoritative set of packages to check.
//
// When a new pk-core package is added, append it here.
var pkCorePackages = []string{
	"github.com/septagon-oss/pk-core/pkg/architecture",
	"github.com/septagon-oss/pk-core/pkg/authz",
	"github.com/septagon-oss/pk-core/pkg/entity",
	"github.com/septagon-oss/pk-core/pkg/entity/crud",
	"github.com/septagon-oss/pk-core/pkg/module",
	"github.com/septagon-oss/pk-core/pkg/mutation",
	"github.com/septagon-oss/pk-core/pkg/registry",
	"github.com/septagon-oss/pk-core/pkg/observability",
	"github.com/septagon-oss/pk-core/pkg/observability/logger",
	"github.com/septagon-oss/pk-core/pkg/observability/metrics",
	"github.com/septagon-oss/pk-core/pkg/observability/tracing",
	"github.com/septagon-oss/pk-core/pkg/observability/health",
	"github.com/septagon-oss/pk-core/pkg/observability/guardrail",
	"github.com/septagon-oss/pk-core/pkg/security",
	"github.com/septagon-oss/pk-core/pkg/security/passhash",
	"github.com/septagon-oss/pk-core/pkg/security/cookies",
	"github.com/septagon-oss/pk-core/pkg/security/signature",
	"github.com/septagon-oss/pk-core/pkg/security/headers",
	"github.com/septagon-oss/pk-core/pkg/security/cors",
	"github.com/septagon-oss/pk-core/pkg/security/csrf",
	"github.com/septagon-oss/pk-core/pkg/security/identity",
	"github.com/septagon-oss/pk-core/pkg/security/ratelimit",
	"github.com/septagon-oss/pk-core/pkg/security/authn",
	"github.com/septagon-oss/pk-core/pkg/security/authz",
	"github.com/septagon-oss/pk-core/pkg/security/middlewarepolicy",
	// security/* appended as they land
	"github.com/septagon-oss/pk-core/pkg/resilience",
	"github.com/septagon-oss/pk-core/pkg/resilience/retry",
	"github.com/septagon-oss/pk-core/pkg/resilience/circuitbreaker",
	"github.com/septagon-oss/pk-core/pkg/resilience/bulkhead",
	"github.com/septagon-oss/pk-core/pkg/event",
	"github.com/septagon-oss/pk-core/pkg/event/memory",
	"github.com/septagon-oss/pk-core/pkg/event/outbox",
	"github.com/septagon-oss/pk-core/pkg/infrastructure/cache",
	"github.com/septagon-oss/pk-core/pkg/infrastructure/database",
	"github.com/septagon-oss/pk-core/pkg/infrastructure/router",
	"github.com/septagon-oss/pk-core/pkg/infrastructure/config",
}

func TestPkCoreImportsAreAllowed(t *testing.T) {
	t.Parallel()
	for _, p := range pkCorePackages {
		pkg, err := build.Default.Import(p, "", 0)
		if err != nil {
			t.Errorf("import %s: %v", p, err)
			continue
		}
		for _, imp := range pkg.Imports {
			if isStdLib(imp) {
				continue
			}
			if strings.HasPrefix(imp, "github.com/septagon-oss/pk-core") {
				continue
			}
			if _, allowed := AllowedExternalDeps[imp]; allowed {
				continue
			}
			t.Errorf("%s imports unauthorized external dependency %q (add to AllowedExternalDeps with justification if intended)", p, imp)
		}
	}
}

// TestNoSeptagonDevImports preserves the absolute boundary: OSS may never
// import private septagon-dev packages, regardless of whitelist status.
func TestNoSeptagonDevImports(t *testing.T) {
	t.Parallel()
	for _, p := range pkCorePackages {
		pkg, err := build.Default.Import(p, "", 0)
		if err != nil {
			t.Errorf("import %s: %v", p, err)
			continue
		}
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, "github.com/septagon-dev") {
				t.Errorf("%s imports forbidden private package %q", p, imp)
			}
		}
	}
}

func isStdLib(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}
