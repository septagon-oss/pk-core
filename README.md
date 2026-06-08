# pk-core

[![Go Reference](https://pkg.go.dev/badge/github.com/septagon-oss/pk-core.svg)](https://pkg.go.dev/github.com/septagon-oss/pk-core)
[![CI](https://github.com/septagon-oss/pk-core/actions/workflows/go.yml/badge.svg)](https://github.com/septagon-oss/pk-core/actions/workflows/go.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

`pk-core` is the small, trusted core of the open-source PlatformKit family. It
defines the shared semantics every PlatformKit module, app, and distribution
builds on: module composition, registry catalogs, provider-neutral authorization
and tracing, entity descriptors, governed mutation boundaries, and reusable
security primitives. It is a contract layer, not a feature catalog, so
independent modules can cooperate through declared ports without importing each
other's implementation.

## Install

```bash
go get github.com/septagon-oss/pk-core@v0.1.0
```

## Usage

```go
package main

import (
	"fmt"
	"log"

	"github.com/septagon-oss/pk-core/pkg/module"
)

// AuditService is a port: the contract one module provides and another
// consumes, without either importing the other's package.
type AuditService interface {
	Record(message string) error
}

func main() {
	audit := module.NewBundle("example.audit", []module.Entry{
		{ID: "audit", New: func() module.Composable {
			return module.Must(
				module.Metadata{ID: "audit", Name: "Audit"},
				module.WithProvides(module.Provide[AuditService]("1.0.0")),
			)
		}},
	}, []string{"audit"})

	content := module.NewBundle("example.content", []module.Entry{
		{ID: "content", New: func() module.Composable {
			return module.Must(
				module.Metadata{ID: "content", Name: "Content"},
				module.WithDependencies(module.Require[AuditService](module.DependencySpec{
					Version:           "1.0.0",
					Purpose:           "audit content publication",
					PreferredProvider: "audit",
				})),
			)
		}},
	}, []string{"content"})

	// Compose validates the required AuditService dependency and returns the
	// modules in dependency order (provider before consumer).
	catalog := module.NewCatalog().Add(content).Add(audit).MustBuild()
	plan, err := module.Compose(catalog)
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range plan.Modules {
		fmt.Println(m.Metadata().ID)
	}
}
```

## Current Surface

- `pkg/module` — modules, bundles, catalogs, typed ports, and dependency-ordered
  `Compose`/`Validate`/`Sort`.
- `pkg/registry` — thread-safe `Registry` plus declarative `CatalogBuilder`
  catalogs with conflict policy, validation hooks, and structured diagnostics.
- `pkg/authz` — provider-neutral policy declarations and evaluator contracts.
- `pkg/entity` — entity descriptors, fields, relationships, capabilities, and
  policy-token requirements.
- `pkg/mutation` — governed mutation intents, rules, gates, and decisions.
- `pkg/observability` — provider-neutral logging, metrics, tracing, health, and
  guardrail contracts.
- `pkg/resilience` — retry, circuit-breaker, and bulkhead primitives.
- `pkg/security` — cookies, password hashing, signatures, security headers,
  CORS, CSRF, request identity, and rate limiting.

These packages are contracts, not product implementations. Concrete providers,
repositories, admin screens, workflows, and HTTP hosts belong in modules,
runtime packages, tools, testkits, or downstream Pro/private extensions.

## Verify

```bash
make verify   # go test + go vet + staticcheck + race
```

Run the minimal example with `go run ./examples/minimal`. See
[docs/EXTENSIBILITY_FITNESS.md](docs/EXTENSIBILITY_FITNESS.md) for the rubric
used to keep core small, composable, deterministic, and safe to extend.

## License

Apache-2.0. See [LICENSE](LICENSE).
