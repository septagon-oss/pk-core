# Composability Audit

This audit answers one question:

> Is this PlatformKit building block truly composable, or is it only reusable
> code with a convenient API?

Use this process for every public architectural block: module contracts,
registries, entity descriptors, policies, mutation gates, metrics, guardrails,
settings, events, jobs, design contributions, docs bundles, test flows, module
packs, app compositions, and client overlays.

Private helpers are not audited as standalone blocks unless they become public
extension points or shared contracts.

## Audit Levels

Composability is audited in six levels. A block is not release-grade until it
passes all levels that apply to its granularity.

| Level | Question | Evidence |
|---|---|---|
| L0 Inventory | Do we know this block exists and who owns it? | Block manifest or descriptor with ID, owner, kind, version. |
| L1 Boundary | Can consumers use it without importing private implementation? | Import-boundary checks, package layout, public contract packages. |
| L2 Contract | Are assumptions and guarantees explicit? | Ports, descriptors, schemas, policies, settings, events, or specs. |
| L3 Algebra | Does composition obey deterministic laws? | Identity, associativity, closure, conflict, substitution, and determinism tests. |
| L4 Runtime Binding | Can the abstract contract bind to runtime behavior without changing the graph? | Adapter tests, route/exporter/worker binding tests, conformance suites. |
| L5 Evidence | Can humans and tools inspect why the block exists and whether it still works? | Tests, docs, requirements, examples, diagnostics, generated catalogs. |

## Scorecard

Score each property from 0 to 3:

| Score | Meaning |
|---|---|
| 0 | Missing. |
| 1 | Present only by convention or informal code shape. |
| 2 | Explicit in descriptors, contracts, docs, or tests. |
| 3 | Enforced by CI, static analysis, conformance tests, or executable fitness tests. |

| Property | What to inspect |
|---|---|
| Identity | Stable ID, owner, version, compatibility range, kind. |
| Boundary | No private implementation imports; public contracts are separate from implementation. |
| Contract | Inputs, outputs, errors, policies, events, settings, and extension points are declared. |
| Contribution | The block contributes descriptors to registries/catalogs instead of mutating process globals. |
| Composition | Ordering, defaults, optional dependencies, and conflict rules are deterministic. |
| Replacement | A compatible implementation can be swapped without consumer changes. |
| Extension | Pro/community packages can add namespaced metadata or contributions without core forks. |
| Runtime binding | Runtime adapters preserve the abstract block graph. |
| Evidence | Tests, docs, requirements, examples, and diagnostics exist and are linked. |

Release-grade composability requires:

- no property scored `0`
- Identity, Boundary, Contract, Composition, and Evidence scored at least `2`
- at least one executable check for any public composition law the block claims
- structured diagnostics for invalid composition

## Block Manifest

Every public block should be representable as a manifest, even when the source
of truth is Go code.

```yaml
id: billing.invoice
kind: feature
owner: billing_management
version: 1.0.0
compatibility: ">=1.0.0,<2.0.0"
provides:
  - billing.InvoiceService@1.0.0
requires:
  - audit.AuditSink@^1.0.0
contributes:
  entities:
    - billing.Invoice
  authz:
    - billing.invoice.read
    - billing.invoice.write
  metrics:
    - billing.invoice.created
  guardrails:
    - billing.provider.unavailable
  docs:
    - docs/billing/invoices.md
evidence:
  tests:
    - features/invoice/service_test.go
  requirements:
    - REQ-BILLING-010
```

The manifest does not need to be hand-written for every block. It can be
generated from Go descriptors, docs manifests, and module metadata. The audit
requires the model to exist, not a specific file format.

## Law Tests

A block's composition surface should have law tests that match its algebra.

### Generic Block Composition

- Identity: composing with the empty block changes nothing.
- Associativity: grouping compatible blocks changes nothing.
- Closure: compatible valid blocks produce a valid composed block.
- Invalidity: incompatible blocks fail with structured diagnostics.
- Determinism: input order does not change the final catalog when order is not
  semantically meaningful.

### Registry Composition

- Disjoint keys merge.
- Duplicate ownership fails when the policy is reject.
- Explicit override policies are deterministic and documented.
- Diagnostics include registry ID, key, source, code, and severity.

### Ordered Overlay Composition

- Precedence is declared.
- Higher-precedence values override lower-precedence values only where allowed.
- Missing values fall back deterministically.
- Cycles and unknown references fail closed.

### Type/Port Composition

- Required ports must be provided by a compatible version.
- Optional ports must not create hard ordering edges.
- Preferred providers and fallback providers are honored deterministically.
- Replacement providers satisfy the same consumer contract.

### Runtime Binding

- Binding a composed graph preserves ownership and diagnostics.
- HTTP, worker, metric, docs, UI, and MCP bindings are projections of the same
  block graph, not separate hidden graphs.

## Audit Outputs

Each audited block receives one status:

| Status | Meaning |
|---|---|
| Composable | Passes the release-grade gate. |
| Composable with gaps | Usable, but missing automation, docs, or edge-case evidence. |
| Reusable only | Helpful API, but not yet a PlatformKit block. |
| Implementation detail | Not intended to be independently composed. |
| Boundary violation | Imports, global mutation, hidden ordering, or concrete dependencies break composability. |

The goal is not to turn every helper into a block. The goal is to make every
public architectural block honest about its status.

## Mechanical Checks

At minimum, every OSS repo should support these checks:

```bash
make verify
go test ./...
go vet ./...
staticcheck ./...
go mod tidy -diff
govulncheck ./...
```

PlatformKit-specific checks should then layer on top:

- import-boundary check: no cross-module implementation imports
- registry audit: all contribution registries declare conflict algebra
- descriptor audit: every public block has identity, owner, kind, version
- law tests: composition surfaces prove identity, associativity, closure, and
  diagnostics where applicable
- evidence audit: docs, requirements, and tests link to public blocks
- substitution audit: compatible replacement implementations compile and pass
  contract tests
- runtime projection audit: app, docs, admin, MCP, worker, metric, and UI
  projections originate from the same block graph

## Priority Order

Audit the block system in this order:

1. `pk-core/pkg/module`: modules, bundles, catalogs, ports, composition.
2. `pk-core/pkg/registry`: contribution algebra and diagnostics.
3. `pk-core/pkg/entity`, `pkg/authz`, `pkg/mutation`: product-governance
   primitives.
4. Observability: metrics, health, tracing, guardrails.
5. Settings, events, jobs, and workflow descriptors when they are introduced.
6. `pk-design`: tokens, themes, component descriptors, design catalogs.
7. `pk-modules`: full module blocks and module packs.
8. `pk-apps`, `pk-runtime`, `pk-testkit`, `pk-docs`, and Pro/client overlays:
   runtime projections and evidence.

This order keeps the grammar stable before auditing larger products built from
that grammar.

## Audit Questions

For each candidate block, ask:

1. Is it an atom, primitive, feature, module, pack, app composition, or overlay?
2. What public contract does it provide?
3. What assumptions does it require?
4. Where are its contributions registered?
5. What algebra composes those contributions?
6. How does it fail when composition is invalid?
7. What runtime adapter binds it?
8. What can replace it?
9. How can Pro/community extend it without a fork?
10. What tests and docs prove the answers?
11. If it participates in runtime behavior, what typed chain links does it
    provide or consume?

If the answers are unclear, the block is not yet composable.

See [BLOCKS_AND_CHAINS.md](BLOCKS_AND_CHAINS.md) for the companion chainability
model.
