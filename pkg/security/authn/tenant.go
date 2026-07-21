// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package authn — tenant.go owns RequireTenant, the middleware that
// rejects principals whose TenantID does not match a fixed tenant ID
// (or whose TenantID is empty) with 403 Forbidden.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package authn

import (
	"net/http"

	"github.com/septagon-oss/pk-core/pkg/security/identity"
)

// RequireTenant returns middleware that rejects requests whose Principal
// belongs to a tenant other than tenantID. Behaviour:
//
//   - Anonymous Principal: 401 Unauthorized.
//   - Authenticated Principal with empty TenantID: 403 Forbidden. An
//     empty TenantID means the credential carried no tenant scoping and
//     therefore cannot be matched against a tenant-bound route.
//   - Authenticated Principal with TenantID != tenantID: 403 Forbidden.
//   - Authenticated Principal with TenantID == tenantID: delegates.
//
// Routes that should accept "any tenant the principal belongs to" must
// extract the route tenant dynamically and use a custom middleware on top
// of identity.PrincipalFromContext — RequireTenant is intentionally
// limited to a fixed tenant ID per middleware instance so the contract is
// declarative and reviewable.
func RequireTenant(tenantID string, opts ...Options) func(http.Handler) http.Handler {
	o := firstOptions(opts)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := identity.PrincipalFromContext(r.Context())
			if p.IsAnonymous() {
				writeError(w, r, o, http.StatusUnauthorized, "authn: unauthorized")
				return
			}
			if p.TenantID == "" || p.TenantID != tenantID {
				writeError(w, r, o, http.StatusForbidden, "authn: tenant mismatch")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
