# Open-Core Boundary

This OSS repository is the upstream framework core.

The Pro/private version may be a downstream fork or a private distribution that
imports this module. In both cases, it should extend the OSS framework through
the same contracts public users get:

- embed `module.Module` for Pro/private modules
- register Pro/private modules through `module.Bundle`
- compose Pro/private modules through `module.Catalog`
- declare Pro/private module dependencies through typed ports
- contribute extension registries through `registry.Spec` and
  `registry.CatalogBuilder`
- publish module-owned entity metadata through `entity.Descriptor`
- declare provider-neutral policy metadata through `authz.Policy`
- route governed mutations through `mutation.Gate`
- host public module plans through `pk-runtime`, not private host contracts
- prove public requirements through `pk-testkit`, not private E2E glue
- add Pro/private providers, adapters, deployment targets, and presets outside
  the OSS core

Pro/private distributions should avoid privately redefining:

- module metadata semantics
- bundle/catalog behavior
- typed port dependency behavior
- dependency validation and ordering
- port version constraint semantics
- registry diagnostic semantics
- entity descriptor semantics
- authz policy vocabulary
- mutation gate vocabulary
- runtime host/readiness semantics
- conformance and flow coverage semantics

When those contracts need to change, change OSS first and let downstream
distributions consume the new public tag.

Before changing any of these contracts, run `make fitness` and update
`docs/EXTENSIBILITY_FITNESS.md` if the intended extension model changes. The
fitness check is the guardrail that keeps downstream extensions using the same
public extension points as community modules.

## Embedding Pattern

```go
type BillingModule struct {
    module.Module
    Provider string
}

func NewBillingModule() *BillingModule {
    return &BillingModule{
        Module: *module.Must(
            module.Metadata{ID: "billing"},
            module.WithDependencies(module.Require[AuditService](module.DependencySpec{
                Version: "^1.0.0",
                Purpose: "audit invoice lifecycle events",
            })),
        ),
        Provider: "stripe",
    }
}
```
