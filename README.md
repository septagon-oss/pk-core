# pk-core

Small, trusted OSS core for PlatformKit's modular application framework.

`pk-core` defines the shared semantics that every PlatformKit module, app, and
distribution can build on. It is intentionally not a feature catalog. Its job is
to make composition, validation, policy vocabulary, entity metadata, and
governed mutation boundaries explicit enough that independent modules can
cooperate without importing each other's implementation.

PlatformKit treats composability as a strict building-block contract:
independent parts must be selectable, replaceable, inspectable, testable, and
extensible through declared contracts. See
[docs/COMPOSABILITY.md](docs/COMPOSABILITY.md) for the canonical definition of a
PlatformKit block.

- modules declare metadata, providers, invocations, provided ports, and required
  ports
- bundles compose modules into catalogs
- catalogs validate duplicate module IDs and expose defaults
- registries provide a reusable substrate for core and extension contribution
  catalogs
- authz defines provider-neutral policy declarations and evaluator contracts
- entities publish descriptors, ownership, fields, relationships, capabilities,
  and policy-token requirements
- mutation gates define governed mutation intent, rules, and decisions without
  implementing the change-management module
- composition builds a dependency-ordered module plan
- typed ports use Go interfaces without importing implementation packages

Pro/private PlatformKit distributions can fork this repo or build on top of it
by adding private modules, providers, adapters, deployment targets, and hosted
workflows. They should extend the public contracts instead of redefining core
semantics privately.

This is one of the minimum OSS repos:

- `pk-shared`
- `pk-core`
- `pk-client`
- `pk-design`
- `pk-runtime`
- `pk-tools`
- `pk-testkit`
- `pk-modules`
- `pk-apps`
- `pk-docs`

See [docs/OPEN_CORE.md](docs/OPEN_CORE.md) for the Pro extension boundary and
[docs/COMPOSABILITY.md](docs/COMPOSABILITY.md) for the building-block model.
Use [docs/COMPOSABILITY_AUDIT.md](docs/COMPOSABILITY_AUDIT.md) to audit whether
a public block is actually composable.
[docs/BLOCKS_AND_CHAINS.md](docs/BLOCKS_AND_CHAINS.md) explains the difference
between composable product blocks and chainable runtime flows.
`docs/block-manifest.json` is the machine-readable public block inventory that
CI validates for v0.0.0 release readiness.

## Quickstart

```bash
make verify
make staticcheck
go run ./examples/minimal
```

Run the architecture fitness check before changing public extension contracts:

```bash
make fitness
```

See [docs/EXTENSIBILITY_FITNESS.md](docs/EXTENSIBILITY_FITNESS.md) for the
rubric used to keep core small, composable, deterministic, and safe to extend.

## Public Contract

The initial stable surface includes `pkg/module`:

- `module.Module`
- `module.Composable`
- `module.Bundle`
- `module.Catalog`
- `module.PortOf`, `module.Provide`
- `module.RequiresPort`, `module.OptionalPort`
- `module.Require`, `module.Optional`
- `module.Validate`, `module.Sort`
- `module.Compose`

`module.Module` is the concrete base type. Pro/private modules can embed it
and extend it without changing the OSS contract.

Port dependencies are declared against named Go interfaces. Provider versions
use concrete semantic versions; consumer dependencies use constraints such as
`^1.2.0`, `~1.2.3`, `>=1.0.0`, or `>=1.0.0,<2.0.0`. Invalid versions,
anonymous interfaces, duplicate module IDs, and malformed dependency
declarations fail during validation instead of being silently ignored.
When a dependency declares a preferred provider, validation accepts that
provider or an explicitly listed fallback provider.

It also includes `pkg/registry`:

- `registry.Registry`
- `registry.CatalogBuilder`
- `registry.Catalog`
- `registry.Contribution`
- `registry.RuleRegistry`
- `registry.ConflictPolicy`
- `registry.Spec`
- `registry.Diagnostic`

Use `Registry` for simple thread-safe lookups and `CatalogBuilder` for
declarative contribution registries that need conflict policies, source
diagnostics, validation hooks, registry metadata, and deterministic ordering.

It includes `pkg/authz`:

- `authz.Principal`
- `authz.Resource`
- `authz.Request`
- `authz.Policy`
- `authz.Evaluator`
- `authz.PolicyEvaluator`
- `authz.AggregateEvaluator`

It includes `pkg/entity`:

- `entity.Descriptor`
- `entity.Field`
- `entity.Relationship`
- `entity.Policy`
- `entity.NewCatalog`

It includes `pkg/mutation`:

- `mutation.Intent`
- `mutation.Rule`
- `mutation.Gate`
- `mutation.RuleGate`
- `mutation.NewCatalog`

It includes provider-neutral `pkg/security` primitives:

- `cookies.Build`, `cookies.Write`, `cookies.Clear`, and `cookies.RegisterKind`
- `passhash.Hasher`, bcrypt, Argon2id, and short-secret hashing
- `signature.Signer` and HMAC signing
- `headers.Middleware` for response security headers and CSP nonces
- `cors.Middleware` for allowlist-based browser origin policy
- `csrf.Middleware` for double-submit CSRF validation through the canonical
  CSRF cookie profile
- `identity.Principal`, `identity.IdentityResolver`, `identity.Chain`, and
  `identity.Middleware` for provider-neutral request identity plumbing
- `ratelimit.Limiter`, `ratelimit.TokenBucket`, and `ratelimit.Middleware`
  for provider-neutral request throttling

These packages are contracts, not product implementations. Concrete identity
providers, repositories, admin screens, approval workflows, job runners, HTTP
hosts, and client flows belong in modules, runtime packages, tools, testkits, or
downstream Pro/private extensions.

Everything else will be added only when it is necessary to keep an end-to-end
developer path complete.
