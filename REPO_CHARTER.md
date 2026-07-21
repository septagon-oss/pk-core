# pk-core Charter

## Purpose

Composable core contracts for PlatformKit's modular application framework. This is the smallest trusted kernel: module composition, registry, authz, entity and mutation gates, security primitives, observability contracts, and resilience patterns.

Every PlatformKit module, app, and distribution builds on these types.

## In Scope

- Module system (`pkg/module`): lifecycle, dependency declaration, singleton wiring
- Registry (`pkg/registry`): ID-based `Registry[T]` with defaults and overrides
- Authorisation (`pkg/authz`): permission and policy contracts
- Entity and mutation gates (`pkg/entity`, `pkg/mutation`): CRUD descriptors, mutation hooks, projection contracts
- Event contracts (`pkg/event`): in-memory and outbox event bus
- Security (`pkg/security`): cookies, passhash (argon2/bcrypt), signature, headers, CORS, CSRF, identity, rate limiting
- Observability (`pkg/observability`): logger, metrics, tracing, health contracts
- Resilience (`pkg/resilience`): bulkhead, circuit breaker, retry
- Infrastructure contracts (`pkg/infrastructure`): cache, config, database, router abstractions

## Out of Scope

- Runnable applications or HTTP servers
- Business logic (user management, billing, etc.)
- Database drivers or migration tooling
- UI components or templates
- CI/CD workflow definitions or deployment manifests

## Dependencies

- `golang.org/x/crypto` — password hashing primitives

## Release Posture

Forward-only within the v0.x line. Replacements migrate every owned caller and
remove the superseded API in the same change; release notes identify deliberate
breaking changes without retaining compatibility shims.
