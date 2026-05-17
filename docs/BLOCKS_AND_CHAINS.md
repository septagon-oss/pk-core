# Blocks and Chains

PlatformKit uses two related ideas:

- composable blocks: stable product parts that can be assembled into an app
  graph
- chainable links: typed behavior steps that can execute in a predictable flow

The distinction matters. A module can be composable but not chainable if it can
be selected into an app but cannot participate in typed flows. A middleware can
be chainable but not composable if it is only a local function chain with no
identity, owner, registry contribution, docs, or replacement contract.

PlatformKit building blocks should usually become both.

## Definitions

| Term | PlatformKit meaning |
|---|---|
| Block | A product capability unit with contract, contribution, runtime binding, and evidence. |
| Composition | Assembly of blocks into a valid product graph. |
| Chain | A typed execution sequence where each link's output can feed the next link's input. |
| Link | A behavior step such as auth, tenant resolution, policy check, validation, mutation gate, handler, audit, event publish, metric observation, or guardrail emission. |
| Flow | A chain with branches, joins, retries, cancellation, compensation, or human review. |
| Projection | A runtime view of the same block graph: HTTP, worker, docs, admin, MCP, metrics, or UI. |

In short:

```text
Composable answers: can these parts assemble?
Chainable answers: can behavior flow through these parts?
```

## Mathematical Shape

Composable blocks use the partial-monoid model described in
[COMPOSABILITY.md](COMPOSABILITY.md):

```text
compose : Block x Block -> Block | Diagnostics
```

Chainable links use a typed-arrow model:

```text
Link[A,B] = Context x A -> Result[B]
chain : Link[A,B] x Link[B,C] -> Link[A,C] | Diagnostics
```

`chain(f, g)` is valid only when `f` produces the type, context, evidence, and
error contract that `g` requires.

For links that can fail, emit evidence, or use context, this resembles Kleisli
composition:

```text
A -> Effect[B]
B -> Effect[C]
----------------
A -> Effect[C]
```

PlatformKit does not need to expose category-theory vocabulary in its APIs, but
it should preserve the laws that make typed chains reliable:

| Law | Meaning |
|---|---|
| Identity | A no-op link can be inserted or removed without changing behavior. |
| Associativity | Grouping adjacent links does not change the result where error and context semantics match. |
| Type compatibility | A link's output contract must satisfy the next link's input contract. |
| Context preservation | Tenant, actor, request ID, trace ID, deadlines, and auth scope propagate unless explicitly transformed. |
| Error algebra | Errors are passed, wrapped, recovered, denied, retried, or compensated only through declared rules. |
| Evidence preservation | Audit, metrics, traces, guardrails, and test evidence remain attributable to the block that emitted them. |
| Cancellation safety | Deadlines and cancellation propagate through the chain. |
| Idempotency clarity | Retryable links declare whether repeated execution is safe, rejected, or compensated. |

## PlatformKit Flow Example

A typical request chain should be describable as a typed flow:

```text
HTTPRequest
  -> ResolveTenant
  -> AuthenticatePrincipal
  -> AuthorizeAction
  -> ValidateInput
  -> EvaluateMutationGate
  -> ExecuteFeatureHandler
  -> RecordAudit
  -> PublishEvent
  -> ObserveMetrics
  -> HTTPResponse
```

Each link should declare:

- input type and required context
- output type and guarantees
- errors and recovery rules
- policies and mutation gates it invokes
- audit, metrics, traces, and guardrails it emits
- tests proving the link composes with its neighbors

The runtime may bind this chain to HTTP, a worker, MCP, admin UI, or E2E test
driver. Those bindings should be projections of the same declared flow, not
separate hand-built pipelines.

## Historical Lineage

PlatformKit does not invent these ideas. It combines proven lines of software
architecture into one product framework grammar.

| Lineage | Names | What PlatformKit borrows |
|---|---|---|
| Unix pipes | Douglas McIlroy, Ken Thompson, Research Unix | Simple tools become powerful when connected by a narrow, stable stream contract. |
| Pipe-and-filter architecture | Mary Shaw, David Garlan | Complex behavior can be modeled as filters connected by pipes when data contracts are explicit. |
| Chain of Responsibility | Gamma, Helm, Johnson, Vlissides | Requests can pass through ordered handlers while sender and receiver remain decoupled. |
| Ports and Adapters | Alistair Cockburn | Core logic depends on ports; adapters bind external technologies without contaminating the core. |
| Functional composition | Philip Wadler, Kleisli composition, monads | Effectful operations can chain predictably when identity, associativity, and type boundaries hold. |
| Actor/message systems | Carl Hewitt and actor-model successors | Isolated components communicate by messages instead of shared implementation state. |
| Assume-guarantee contracts | contract-based design and compositional verification | A system can be reasoned about by checking each component's assumptions against others' guarantees. |
| Microkernel and plugin architecture | operating-system microkernels, plugin systems | A small stable core can host independently developed extensions when extension points are explicit. |

## PlatformKit Position

PlatformKit's position is stricter than ordinary "plugin architecture" and more
product-oriented than ordinary function chaining:

```text
Core defines the grammar.
Blocks contribute declared contracts.
Apps compose blocks into graphs.
Runtime projections bind graphs into chains and flows.
Evidence proves the graph and chains remain valid.
```

This means:

- metrics are blocks when modules declare metric specs into registries
- metrics are links when flows observe values through context-preserving
  instrumentation
- guardrails are blocks when modules declare owned guardrail codes
- guardrails are links when runtime paths emit structured fallback/degraded
  outcomes
- modules are blocks when they contribute contracts and descriptors
- features are links when they participate in typed request, job, event, MCP, or
  E2E flows

## What We Avoid

PlatformKit should not confuse these ideas with weaker patterns:

- fluent method chaining without typed contracts
- middleware stacks with hidden global state
- plugin systems without manifests or conflict rules
- event buses with anonymous payloads
- registries that silently accept duplicate ownership
- workflows that cannot explain tenant, actor, deadline, idempotency, or audit
  propagation
- UI components that can be rendered but cannot declare tokens, anatomy,
  variants, docs, or tests

Those can be useful implementation techniques, but they are not enough to be
PlatformKit blocks or chains.

## Audit Consequence

The composability audit must be extended with a chainability question:

> If this block participates in runtime behavior, can its behavior be expressed
> as typed links with declared input, output, context, error, evidence, and
> idempotency semantics?

Blocks that only contribute static vocabulary, such as design tokens, may not
need a runtime chain. Blocks that handle requests, jobs, events, MCP tools,
admin operations, or E2E flows do.

The goal is not ceremony. The goal is that PlatformKit can assemble products
and reason about their behavior without relying on hidden coupling.
