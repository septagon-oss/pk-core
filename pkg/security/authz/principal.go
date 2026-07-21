// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package authz — principal.go owns PrincipalFromRequest, the
// convenience helper that maps an identity.Principal attached to a
// request into the coreauthz.Principal shape RequestBuilders need.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package authz

import (
	"net/http"

	coreauthz "github.com/septagon-oss/pk-core/pkg/authz"
	"github.com/septagon-oss/pk-core/pkg/security/identity"
)

// PrincipalFromRequest maps the identity.Principal attached to r into
// the coreauthz.Principal shape policy evaluators expect.
//
// The mapping is deliberately narrow because identity.Principal and
// coreauthz.Principal own different concerns: identity tracks the
// "who" and "how authenticated" for HTTP, while coreauthz tracks the
// "who" plus the role/claim metadata policies match against. Fields
// pk-core does not own (Kind, Roles, Claims) are left at their zero
// value; Pro builders that need richer mapping wrap this helper and
// fill in the extras from their own session or claims plumbing.
//
// Anonymous principals (identity.Principal{} zero value) map to the
// zero coreauthz.Principal — that is the only legal representation of
// "no authenticated caller" across the authz vocabulary. Scopes flow
// through unchanged so policies that match on scope (treating scopes
// as fine-grained roles) work without per-route translation.
func PrincipalFromRequest(r *http.Request) coreauthz.Principal {
	if r == nil {
		return coreauthz.Principal{}
	}
	p := identity.PrincipalFromContext(r.Context())
	if p.IsAnonymous() {
		return coreauthz.Principal{}
	}
	return coreauthz.Principal{
		ID:       p.Subject,
		TenantID: p.TenantID,
		// Scopes map onto Roles for policy matching — pk-core's
		// PolicyEvaluator treats Roles as the credential's capability
		// labels, which is exactly how identity.Scopes are produced.
		Roles: append([]string(nil), p.Scopes...),
	}
}
