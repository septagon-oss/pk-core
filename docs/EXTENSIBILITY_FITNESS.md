# Extensibility Fitness

PlatformKit core is valuable only if downstream modules can evolve without
forking framework semantics. This repo treats extensibility as a CI-enforced
fitness function, not as a design aspiration.

Run it with:

```bash
make fitness
```

The executable fitness check lives in `pkg/architecture`. It verifies that:

- Pro modules can embed `module.Module` without losing the public `Composable`
  contract.
- Module composition orders dependencies through typed ports, not direct module
  imports.
- Entity descriptors can publish module-owned metadata, policy tokens, and
  namespaced extension field types.
- Authorization policies stay scoped to the resource module when a request
  declares one.
- Governed mutation gates can run lean by default and fail closed when a
  downstream distribution requires review or denial for unmatched changes.
- Extension registries emit structured duplicate-contribution diagnostics.

## Evaluation Rubric

Use this rubric before adding anything to core:

| Criterion | Pass condition |
|---|---|
| Minimality | The addition is a stable contract, not a product workflow, hosted feature, or provider adapter. |
| Composability | It works through typed interfaces, descriptors, bundles, gates, or registries. |
| Isolation | It does not import private repos, concrete SaaS modules, cloud SDKs, DI containers, databases, queues, browsers, or deployment tools. |
| Determinism | Ordering, conflict handling, normalization, and diagnostics are repeatable. |
| Tenant safety | Tenant ownership is explicit, and mismatches are rejected where core has enough context to know they are invalid. |
| Evolution | Downstream extensions can add metadata or namespaced vocabulary without changing core. |
| Observability | Validation failures return structured or specific errors suitable for CLI, docs, CI, and agent tooling. |
| Go quality | APIs are small, allocation-conscious, race-safe where mutable, context-aware where evaluators are invoked, and tested through public behavior. |

If a proposal fails one of these criteria, it belongs in `pk-modules`,
`pk-tools`, `pk-apps`, `pk-docs`, or a downstream Pro repository until the
contract has proven stable.
