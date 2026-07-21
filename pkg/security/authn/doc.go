// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package authn provides HTTP middleware that enforces authentication
// requirements on top of the Principal attached by pk-core/pkg/security/identity.
//
// # Why this package exists
//
// The identity package answers "who is this request from?" by attaching a
// Principal to the request context. That Principal may be anonymous,
// authenticated, scope-bearing, or tenant-scoped. Route handlers and
// upstream composition code need a uniform way to declare REQUIREMENTS on
// the Principal — "this endpoint requires authentication", "this endpoint
// requires the read:users scope", "this endpoint is tenant-scoped to
// tenant T". Hand-rolling those checks in every handler couples handlers
// to identity internals and produces inconsistent 401/403 responses.
//
// This package owns the contract instead. Every middleware:
//
//   - reads the Principal via identity.PrincipalFromContext;
//   - applies a single requirement (auth presence, scope set, tenant ID);
//   - responds with 401 on anonymity, 403 on policy violation;
//   - delegates response shaping to a caller-supplied ErrorWriter when
//     non-default 401/403 bodies are desired.
//
// # How it composes
//
// The middlewares share the standard func(http.Handler) http.Handler
// signature so they slot into any router. They are intended to run AFTER
// identity.Middleware (which attaches the Principal) and BEFORE the route
// handler. Multiple requirements compose either by stacking middlewares
// directly or by passing them to RequireAllOf, a Block-level composer that
// short-circuits on the first failure.
//
// # Composable contract
//
// Options carries an ErrorWriter func field as the only extension point —
// Pro packages override 401/403 response shaping for JSON APIs, HTML
// flash messages, or telemetry hooks without forking the middleware. The
// middleware factories themselves are stateless and safe for concurrent
// use; the same middleware value services every request in flight.
//
// # What this package does NOT cover
//
// Credential validation (resolving tokens, cookies, headers into a
// Principal) lives in identity and its downstream resolver packages.
// Policy-based authorization (ABAC/RBAC over actions and resources) lives
// in pk-core/pkg/security/authz. This package only enforces simple
// declarative requirements on the already-attached Principal.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package authn
