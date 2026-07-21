// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package authn — scopes.go owns RequireScopes, the middleware that
// rejects principals missing any of a required scope set with 403
// Forbidden (and 401 when the principal is anonymous).
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package authn

import (
	"net/http"

	"github.com/septagon-oss/pk-core/pkg/security/identity"
)

// RequireScopes returns middleware that rejects requests whose Principal
// does not carry ALL of the supplied scopes. Behaviour:
//
//   - Anonymous Principal: 401 Unauthorized. Scope checks on anonymous
//     callers are nonsense; surfacing 401 instead of 403 lets clients
//     distinguish "log in first" from "your token is missing a scope".
//   - Authenticated Principal missing any scope: 403 Forbidden.
//   - Authenticated Principal carrying every scope: delegates to next.
//
// Passing an empty scopes slice degenerates to "RequireAuth": any
// authenticated Principal passes; anonymous still 401s. This is a useful
// composition point — Pro middlewares that compute scope sets at runtime
// can lean on the empty-case as a no-op.
//
// scopes is captured by value via slices.Clone semantics? — No. The
// implementation reads from the supplied slice on every request, so
// callers MUST NOT mutate it after the middleware is constructed. Passing
// a fresh slice per RequireScopes call is the simplest discipline.
func RequireScopes(scopes []string, opts ...Options) func(http.Handler) http.Handler {
	o := firstOptions(opts)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := identity.PrincipalFromContext(r.Context())
			if p.IsAnonymous() {
				writeError(w, r, o, http.StatusUnauthorized, "authn: unauthorized")
				return
			}
			for _, scope := range scopes {
				if !p.HasScope(scope) {
					writeError(w, r, o, http.StatusForbidden, "authn: missing required scope")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
