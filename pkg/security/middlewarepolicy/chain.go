// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package middlewarepolicy — chain.go owns Chain, the associative
// composer that turns a list of middlewares into a single middleware
// value whose outermost element is mws[0].
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package middlewarepolicy

import "net/http"

// Chain composes multiple middlewares into one. Calling order:
//
//   - mws[0] is OUTERMOST — it sees the request first and the response
//     last.
//   - mws[len-1] is innermost, sitting directly above next.
//
// This matches the build order used by authn.RequireAllOf and the
// natural reading order of a route declaration ("auth, then rate
// limit, then handler"), so Chain mixes cleanly with other composers.
//
// Empty mws is the identity middleware: Chain()(h) == h. Nil entries
// in mws are tolerated and skipped, mirroring identity.Chain's
// permissive defaults — callers can build chains from optional
// configuration without nil-guarding each entry.
//
// Chain is safe to call once at startup and reuse across every
// request; the returned middleware closes over a defensive copy of
// mws so callers may mutate their original slice after construction.
func Chain(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
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
