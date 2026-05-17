// Package identity — middleware.go owns the Middleware factory that
// adapts an IdentityResolver to the standard func(http.Handler) http.Handler
// shape so the resolver chain plugs into any router or middleware stack
// without leaking package internals.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package identity

import "net/http"

// Middleware returns HTTP middleware that runs resolver against each
// incoming request and attaches the resulting Principal to the request
// context. Downstream handlers read it back via PrincipalFromContext.
//
// Error handling:
//
//   - Anonymous Principal + nil error: the request continues with an
//     anonymous Principal attached. Authorization code decides whether
//     unauthenticated access is allowed at the protected resource.
//   - Non-anonymous Principal + nil error: the Principal is attached and
//     the request continues.
//   - Non-nil error: the middleware writes 401 Unauthorized and does not
//     call next. The resolver decides what constitutes "presented but
//     invalid" — for example, a bearer token with a bad signature.
//
// If resolver is nil the middleware degrades to a pass-through that
// attaches the zero (anonymous) Principal. This avoids forcing callers
// who only want the context plumbing — for example in tests or in apps
// behind an upstream identity proxy — to wire a no-op resolver by hand.
func Middleware(resolver IdentityResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if resolver == nil {
				next.ServeHTTP(w, r.WithContext(ContextWithPrincipal(r.Context(), Principal{})))
				return
			}
			p, err := resolver.Resolve(r)
			if err != nil {
				http.Error(w, "identity: unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(ContextWithPrincipal(r.Context(), p)))
		})
	}
}
