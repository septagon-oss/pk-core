// Package middlewarepolicy provides composition primitives for HTTP
// middleware chains: Chain to compose multiple middlewares into one,
// SkipIf to conditionally skip a middleware, and a small library of
// reusable predicates (PathPrefixSkip, MethodSkip, BearerAuthSkip).
//
// # Why this package exists
//
// PlatformKit apps stack many HTTP middlewares — identity resolution,
// authentication checks, authorization checks, security headers, CORS,
// CSRF, rate limiting, observability. Two patterns recur and deserve a
// dedicated home rather than a hand-rolled chain helper in every app:
//
//  1. Composition: turning a list of middlewares into a single
//     middleware value so apps can pass "the security stack" around as
//     a first-class object.
//  2. Conditional skipping: most middlewares need exceptions (skip CSRF
//     on bearer-token API endpoints, skip rate limiting for health
//     probes, skip authentication for the login route). Open-coding
//     those exceptions inside each middleware makes them harder to
//     audit and reuse.
//
// This package owns both patterns. Every primitive is a small
// composable building block; none of them know anything about
// authentication, authorization, or any specific middleware. Pro
// packages and downstream apps build their custom skip predicates on
// top of the Predicate type.
//
// # Composable contract
//
// Predicate is a named function type so custom predicates compose with
// the built-in ones (callers can pass `func(r *http.Request) bool`
// literals or named functions interchangeably). Chain is associative
// and uses an outermost-first build order so it composes cleanly with
// authn.RequireAllOf and any router-native middleware stack.
// SkipIf accepts ANY middleware — there is no special type for
// skippable middlewares; every standard func(http.Handler) http.Handler
// is eligible.
//
// # What this package does NOT cover
//
// Specific middleware implementations live in their own packages
// (identity, authn, authz, cors, csrf, headers, ratelimit, …). Routing
// (path matching with parameters, method dispatch) is the router's
// job; this package only offers coarse skip predicates suitable for
// middleware-level branching, not for replacing a router.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package middlewarepolicy
