// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package authn — compose.go owns RequireAllOf, the Block-level composer
// that runs several require-middlewares as a single short-circuiting
// pipeline.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package authn

import "net/http"

// RequireAllOf composes multiple middlewares into one that runs them in
// order. The first middleware to short-circuit (e.g., issue a 401/403)
// wins: subsequent middlewares are not invoked and next never runs.
//
// Build order: mws[0] is the OUTERMOST middleware — it sees the request
// first. mws[len-1] is the innermost, sitting directly above next. This
// matches every other middleware-chain helper in the project so callers
// can mix RequireAllOf with middlewarepolicy.Chain without surprises.
//
// Nil entries are tolerated and skipped, mirroring identity.Chain's
// permissive defaults. An empty mws argument produces an identity
// middleware that passes every request through unchanged — useful as a
// neutral element in higher-level composers.
func RequireAllOf(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	// Defensive copy so caller-side mutation of mws after construction
	// cannot retroactively reshape the chain.
	chain := make([]func(http.Handler) http.Handler, 0, len(mws))
	for _, mw := range mws {
		if mw != nil {
			chain = append(chain, mw)
		}
	}
	return func(next http.Handler) http.Handler {
		h := next
		for i := len(chain) - 1; i >= 0; i-- {
			h = chain[i](h)
		}
		return h
	}
}
