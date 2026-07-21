// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package headers provides HTTP middleware that applies a security-header
// response policy and attaches a per-request Content-Security-Policy nonce
// to the request context.
//
// # Why this package exists
//
// Modern browsers enforce a defense-in-depth posture through a small set of
// response headers (Strict-Transport-Security, X-Frame-Options,
// X-Content-Type-Options, Referrer-Policy, Content-Security-Policy,
// Permissions-Policy, ...). Every PlatformKit-rendered response needs them
// applied consistently; hand-wiring at each handler does not scale and
// reliably drifts. This package centralizes the policy in one composable
// middleware factory.
//
// # Composable contract
//
// Middleware is parameterized by a Config struct. Multiple Configs supplied
// to Middleware merge left-to-right: scalar fields use last-wins semantics
// and CSPDirectives are map-merged by key. Pro/downstream packages extend
// the policy by passing additional Configs, not by mutating package state.
//
// # CSP nonce
//
// When CSPDirectives contain a value with a "%s" placeholder, the middleware
// generates a fresh 16-byte cryptographic nonce per request, base64
// raw-URL-encodes it, substitutes it into every placeholder, and attaches
// the nonce to the request context so downstream template renderers can
// emit <script nonce="..."> tags without re-discovering the value.
//
// # What this package does NOT cover
//
// CORS preflight handling, CSRF double-submit tokens, request authentication,
// and rate limiting are deliberately out of scope. They live in
// sibling packages (cors, csrf, identity, ratelimit) so each block has a
// single responsibility and a self-contained contract.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package headers
