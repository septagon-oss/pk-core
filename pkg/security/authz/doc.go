// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package authz provides HTTP middleware that bridges an *http.Request
// to pk-core/pkg/authz's provider-neutral Evaluator contract.
//
// # Why this package exists
//
// The pk-core/pkg/authz block owns the policy-evaluation vocabulary
// (Request, Principal, Resource, Action, Decision, Evaluator). It is
// deliberately transport-agnostic so the same evaluator instance can be
// reused from HTTP, gRPC, NATS, CLIs, and tests. That neutrality means
// some application-side glue is needed to convert each transport's
// representation of "who is asking, for what action, on which resource"
// into an authz.Request.
//
// This package is the HTTP half of that glue. It carries no policy
// vocabulary of its own: callers supply an authz.Evaluator and a
// RequestBuilder, and the middleware runs Evaluate against the result.
// Decisions map onto HTTP status codes the same way every PlatformKit
// surface expects:
//
//   - DecisionAllow → next runs;
//   - DecisionDeny → 403 Forbidden;
//   - DecisionAbstain → 403 Forbidden (treated as deny by default);
//   - Builder error → 400 Bad Request (the request is malformed);
//   - Evaluator error → 500 Internal Server Error (the engine failed).
//
// # Composable contract
//
// RequestBuilder is an interface, so any application can implement its
// own resource-extraction logic (path parameters, header maps, request
// body parsing) without forking the middleware. RequestBuilderFunc
// adapts a plain function literal to the interface for lightweight or
// in-test builders. The Evaluator parameter is an interface from
// pk-core/pkg/authz so policy-evaluation engines plug in by interface
// satisfaction — single-policy, aggregate, OPA-backed, ABAC, or future
// data-driven implementations all interoperate with this middleware.
//
// Options carries an ErrorWriter func field as the only response-shape
// extension point: Pro packages can return JSON error envelopes, attach
// telemetry tags, or upgrade abstain to 401 by overriding it.
//
// # What this package does NOT cover
//
// Policy storage, policy authoring, evaluator implementations, and the
// policy language itself live in pk-core/pkg/authz (and downstream Pro
// engines). Authentication — resolving credentials to a Principal —
// lives in pk-core/pkg/security/identity. Simple authentication
// requirements (must-be-authenticated, must-have-scope, must-be-tenant)
// live in pk-core/pkg/security/authn. This package only owns the HTTP
// glue between an attached Principal and the authz Evaluator contract.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package authz
