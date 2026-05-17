# Composability

PlatformKit uses composability as an architectural contract, not as a synonym
for "reusable code".

A PlatformKit part is composable when it can be selected, removed, replaced,
extended, inspected, and tested through declared contracts without importing
another part's private implementation or changing the platform core.

Composable parts are the building blocks of the modular monolith. They let one
deployment run a small product, another run a vertical pack, and another run a
Pro distribution while sharing the same core grammar.

## Definition

Composable means all of the following are true:

| Property | Required behavior |
|---|---|
| Identity | The part has a stable ID, owner, version, and compatibility range. |
| Boundary | Private implementation stays private; consumers depend on public contracts only. |
| Contract | Inputs, outputs, policies, errors, events, settings, and extension points are declared. |
| Contribution | The part contributes descriptors into registries or catalogs instead of mutating globals. |
| Composition | Ordering, conflict handling, defaults, overrides, and missing optional dependencies are deterministic. |
| Replacement | A compatible implementation can replace another without consumer code changes. |
| Extension | Downstream packages can add namespaced metadata or contributions without forking core. |
| Runtime binding | The declared contract can be bound to concrete providers, adapters, routes, jobs, or exporters at app composition time. |
| Evidence | Tests, docs, requirements, examples, and diagnostics prove the contract remains valid. |

If any of these are missing, the part may still be useful, but it is not a
PlatformKit building block yet.

## PlatformKit Block

A PlatformKit block is the smallest architectural unit that can be composed as
a product capability.

Every block has four layers:

```text
Block = Contract + Contribution + Runtime Binding + Evidence
```

- Contract: stable types, descriptors, policies, schemas, or interfaces.
- Contribution: module-owned declarations registered into catalogs or
  registries.
- Runtime binding: concrete provider, adapter, route, worker, exporter, or
  renderer chosen by an app composition.
- Evidence: tests, docs, requirements, examples, and validation diagnostics.

A block is not defined by package size. A token, entity, policy, metric,
guardrail, feature, module, module pack, app composition, or client overlay can
all be blocks if they satisfy the composability contract at the right level.

## Granularity Ladder

PlatformKit uses a ladder of blocks:

| Level | Examples | Composition role |
|---|---|---|
| Atom | design token, field, policy token, metric name, guardrail code | Named vocabulary that other blocks can reference. |
| Primitive | port, entity descriptor, authz policy, mutation rule, health check, component descriptor | Declarative contract with validation rules. |
| Feature | route group, service use case, admin panel, workflow, E2E flow | Product behavior owned by one module. |
| Module | tenant, auth, billing, audit, content, notification | Deployable capability with contracts and contributions. |
| Module pack | OSS pack, vertical pack, Pro pack | Curated set of modules with defaults and compatibility. |
| App composition | monolith, worker, docs portal, MCP server, admin console | Runtime selection and binding of blocks. |
| Client overlay | brand, copy, flows, tenant-specific surfaces | Business-specific product experience over stable blocks. |

The lower levels provide vocabulary. The higher levels assemble vocabulary into
products.

## Contribution Surfaces

A mature module should be able to contribute zero or more of these surfaces:

| Surface | Contract examples | Owned by core? |
|---|---|---|
| Module identity | metadata, version, tier, compatibility | Yes |
| Ports | provided interfaces, required interfaces, fallback providers | Yes |
| Entities | fields, relationships, capabilities, policy tokens | Yes |
| Authorization | policy descriptors, resource ownership, evaluators | Yes |
| Mutation governance | mutation intents, rules, default decisions | Yes |
| Registries | deterministic contribution catalogs and diagnostics | Yes |
| Observability | metric specs, health checks, trace spans, guardrail codes | Contract only |
| Settings | setting descriptors, scopes, defaults, validation | Contract only |
| Events | event descriptors, versions, idempotency policy, subscribers | Contract only |
| Jobs | job descriptors, schedules, retry policy, ownership | Contract only |
| Design | tokens, themes, component descriptors, anatomy, required tokens | `pk-design` |
| UI surfaces | admin panels, public pages, adaptive surfaces | Module/runtime |
| Docs | module docs, ADR links, requirement links, examples | `pk-docs` |
| Tests | unit contracts, conformance checks, E2E flows | `pk-testkit` |

"Contract only" means core should define the grammar and validation shape, not
the concrete implementation. For example, core can define a metric descriptor,
but Prometheus, OpenTelemetry, dashboards, and alert routing belong in runtime,
tooling, or Pro adapters.

## Non-Composable Smells

A part is drifting away from PlatformKit composability when it:

- imports another module's implementation package
- registers behavior through process globals or init-time side effects
- hides ownership in naming conventions instead of descriptors
- relies on implicit ordering
- silently accepts duplicate ownership
- exposes untyped maps where a descriptor would be stable
- requires a concrete database, queue, DI container, browser, cloud SDK, or UI
  framework in core
- cannot be listed by tooling
- cannot explain why it exists through docs or requirements
- cannot be tested without booting the whole product

Some product code will be intentionally non-composable at the edge. That code
belongs in client overlays or Pro/private packages, not in core contracts.

## Fitness Rule

Before a new core API is accepted, it must answer these questions:

1. What block does this make composable?
2. What is the stable contract?
3. What owns each contribution?
4. How are conflicts rejected or resolved?
5. How does an app bind the contract to runtime behavior?
6. How can a Pro/private package extend it without a core fork?
7. How do docs, tests, and tooling inspect it?

If the answer is "this is only convenient implementation code", it does not
belong in `pk-core`.
