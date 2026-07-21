// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package csrf provides HTTP middleware that enforces the double-submit
// cookie pattern for CSRF protection.
//
// # Why this package exists
//
// Browser cookies are attached automatically to every request to the
// originating domain — including state-changing requests triggered from
// a malicious third-party page. The double-submit pattern defends by
// requiring an additional request header whose value the attacker cannot
// forge (because they cannot read the cookie). This middleware centralizes
// the implementation so handlers never re-derive the rules.
//
// # Composable contract
//
// Middleware is parameterized by a Config struct. Multiple Configs merge
// left-to-right: scalar fields use last-wins, ExemptPaths is concatenated.
// The cookie write goes through pk-core/pkg/security/cookies.Write with
// KindCSRF, which guarantees the canonical CSRF cookie security profile
// (Path=/, HttpOnly=false so JS can mirror it into a request header,
// SameSite=Strict because the cookie's sole purpose is to defeat
// cross-site requests).
//
// # Chainable contract
//
// The returned func(http.Handler) http.Handler composes with any peer
// middleware. Safe methods (GET/HEAD/OPTIONS) pass through unchanged;
// unsafe methods (POST/PUT/PATCH/DELETE) are validated and rotate the
// cookie on success.
//
// # What this package does NOT cover
//
// Session authentication, CORS, response security headers, and identity
// extraction are in sibling packages.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package csrf
