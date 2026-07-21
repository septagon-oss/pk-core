// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package cors provides an origin-allowlist Cross-Origin Resource Sharing
// HTTP middleware.
//
// # Why this package exists
//
// Modern browsers enforce the same-origin policy on cross-origin requests
// initiated by scripts. Servers that want to participate in cross-origin
// flows must opt in by emitting the WHATWG-Fetch-defined CORS headers:
// Access-Control-Allow-Origin, Access-Control-Allow-Methods, ...
// Hand-wiring this at each handler does not scale; centralizing the
// allowlist + preflight handling in one middleware does.
//
// # Composable contract
//
// Middleware is parameterized by a Config struct. The AllowedOrigins
// slice is defensively copied at Middleware construction so the caller's
// slice cannot be mutated to widen the allowlist after the fact. Custom
// matching strategies (regex, wildcard subdomains) plug in through the
// OriginAllowed func field rather than by reflection or by package-level
// global state, which keeps the block deterministic and substitutable.
//
// # Chainable contract
//
// The returned func(http.Handler) http.Handler composes with any peer
// middleware under standard net/http rules: context propagates via
// r.Context(); cancellation is observed by inner handlers, not by this
// middleware; no panics are introduced.
//
// # What this package does NOT cover
//
// Identity authentication, CSRF double-submit tokens, and the rest of the
// response security policy (HSTS, CSP, ...) are in sibling packages
// (identity, csrf, headers).
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package cors
