// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package identity — identity.go owns the Principal value type, the
// request-context plumbing (ContextWithPrincipal, PrincipalFromContext),
// and the IdentityResolver interface plus its ResolverFunc adapter. These
// are the extension points authentication modules implement to integrate
// with the platform without importing each other.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package identity

import (
	"context"
	"net/http"
	"slices"
)

// AuthMethodAnonymous is the conventional AuthMethod label for unauthenticated
// callers. Resolvers that decline to authenticate a request should leave the
// Principal at its zero value (Subject == ""); the label exists for code paths
// that want to be explicit when logging or emitting metrics about anonymous
// access.
const AuthMethodAnonymous = "anonymous"

// Principal identifies the authenticated subject for a request. The zero
// value represents an anonymous (unauthenticated) caller; tests, helpers,
// and downstream authorization code all rely on Principal{}.IsAnonymous()
// being true.
//
// Fields are exported so downstream resolvers (JWT, OAuth, SAML, SCIM,
// magic-link, mTLS, etc.) can construct Principals directly without
// importing internals from this package. New resolvers add new AuthMethod
// labels rather than new fields; if a domain truly needs a field beyond
// these four it should attach its own value to context under its own
// package-private key and key off Subject.
type Principal struct {
	// Subject is the stable subject identifier (user ID, service account
	// ID, machine ID, etc.). Empty string means anonymous.
	Subject string

	// TenantID is the tenant context the caller is operating in. Empty
	// string means a cross-tenant request or an anonymous one. Tenant
	// resolution is a separate concern; resolvers populate this when the
	// credential carries tenant scoping.
	TenantID string

	// Scopes carries OAuth-style scope strings or analogous capability
	// labels. Nil for anonymous principals. Use HasScope to test
	// membership instead of comparing slices directly so callers stay
	// resilient to representation changes.
	Scopes []string

	// AuthMethod is a short label identifying which resolver authenticated
	// this principal (for example "cookie", "bearer", "api_key", "oauth",
	// "saml", "anonymous"). It is informational — authorization decisions
	// must not branch on AuthMethod.
	AuthMethod string
}

// IsAnonymous reports whether the principal represents an unauthenticated
// caller. The contract is intentionally narrow: Subject == "" is the only
// signal. A resolver that wants to mark an anonymous caller with metadata
// (for example, AuthMethod = AuthMethodAnonymous to suppress later
// fallbacks) may set other fields without changing IsAnonymous.
func (p Principal) IsAnonymous() bool {
	return p.Subject == ""
}

// HasScope reports whether the principal carries the named scope.
// A nil or empty Scopes slice always returns false. Comparison is exact;
// callers that need namespace prefix matching (e.g., "users:*") should
// implement that on top of HasScope rather than baking it into the
// contract here.
func (p Principal) HasScope(scope string) bool {
	if scope == "" {
		return false
	}
	return slices.Contains(p.Scopes, scope)
}

// Clone returns p with owned slice storage. Use it when moving Principal
// values across trust or lifecycle boundaries (for example, context storage)
// so later mutation of a caller-owned Scopes slice cannot alter the
// request's authenticated identity.
func (p Principal) Clone() Principal {
	p.Scopes = slices.Clone(p.Scopes)
	return p
}

// contextKey is unexported so other packages cannot stash arbitrary types
// under the same key and confuse PrincipalFromContext. The single
// principalKey instance below is the only legal key value.
type contextKey struct{}

// principalKey is the context.Context key under which the current request's
// Principal is stored. Unexported by construction.
var principalKey = contextKey{}

// ContextWithPrincipal returns ctx with p attached. Resolvers and tests are
// the primary callers; downstream handlers usually receive an already-
// enriched context from the Middleware.
func ContextWithPrincipal(ctx context.Context, p Principal) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, principalKey, p.Clone())
}

// PrincipalFromContext returns the Principal attached to ctx. If no
// Principal is attached the zero (anonymous) value is returned —
// anonymous is a first-class state in this package, so callers never
// need to distinguish "absent" from "anonymous."
func PrincipalFromContext(ctx context.Context) Principal {
	if ctx == nil {
		return Principal{}
	}
	if v, ok := ctx.Value(principalKey).(Principal); ok {
		return v.Clone()
	}
	return Principal{}
}

// IdentityResolver determines the principal for a request. Implementations
// inspect cookies, headers, tokens, mTLS client certificates, OAuth state,
// etc. The contract is:
//
//   - Return (Principal{}, nil) to signal "no credentials presented" — the
//     request continues unauthenticated.
//   - Return (Principal{Subject: …, …}, nil) to signal a successful
//     authentication.
//   - Return (Principal{}, err) to signal "credentials presented but
//     invalid" — Middleware writes 401 and stops the chain.
//
// Implementations must be safe for concurrent use; the same resolver value
// will service many requests in flight.
type IdentityResolver interface {
	Resolve(r *http.Request) (Principal, error)
}

// ResolverFunc adapts an ordinary function to the IdentityResolver
// interface, mirroring the http.HandlerFunc pattern. Use this for
// lightweight or one-off resolvers (tests, in-app closures over local
// state) rather than declaring a new named type.
type ResolverFunc func(r *http.Request) (Principal, error)

// Resolve satisfies IdentityResolver.
func (f ResolverFunc) Resolve(r *http.Request) (Principal, error) {
	return f(r)
}
