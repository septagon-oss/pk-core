// Validates: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package authn_test — authn_test.go is the external contract test suite
// for the authn package. It exercises every exported middleware
// (RequireAuth, RequireScopes, RequireTenant, RequireAllOf), the Options
// extension hook, and a cross-block integration test demonstrating an
// identity.Middleware → authn.RequireScopes chain end-to-end.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package authn_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/security/authn"
	"github.com/septagon-oss/pk-core/pkg/security/identity"
)

// withPrincipal builds a request whose context already carries p, so each
// test case can exercise the middleware in isolation from the upstream
// identity.Middleware. The cross-block integration test below uses the
// real middleware.
func withPrincipal(p identity.Principal) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	return r.WithContext(identity.ContextWithPrincipal(r.Context(), p))
}

// nextRecorder returns a handler that flips a flag when invoked, plus the
// flag pointer so tests can assert whether next ran.
func nextRecorder() (http.Handler, *bool) {
	called := false
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}), &called
}

func TestRequireAuthRejectsAnonymous(t *testing.T) {
	t.Parallel()

	mw := authn.RequireAuth()
	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, withPrincipal(identity.Principal{}))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run on anonymous principal")
	}
}

func TestRequireAuthAllowsAuthenticated(t *testing.T) {
	t.Parallel()

	mw := authn.RequireAuth()
	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, withPrincipal(identity.Principal{Subject: "u-1"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !*called {
		t.Fatal("next must run when principal is authenticated")
	}
}

func TestRequireScopesRejectsMissingScope(t *testing.T) {
	t.Parallel()

	mw := authn.RequireScopes([]string{"read:users", "write:users"})
	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, withPrincipal(identity.Principal{
		Subject: "u-1",
		Scopes:  []string{"read:users"},
	}))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run when a scope is missing")
	}
}

func TestRequireScopesAcceptsAllScopes(t *testing.T) {
	t.Parallel()

	mw := authn.RequireScopes([]string{"read:users", "write:users"})
	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, withPrincipal(identity.Principal{
		Subject: "u-1",
		Scopes:  []string{"read:users", "write:users", "admin"},
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !*called {
		t.Fatal("next must run when all scopes present")
	}
}

func TestRequireScopesAnonymousRejected(t *testing.T) {
	t.Parallel()

	mw := authn.RequireScopes([]string{"read:users"})
	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, withPrincipal(identity.Principal{}))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous scope-check must 401, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run on anonymous principal")
	}
}

func TestRequireTenantRejectsMismatch(t *testing.T) {
	t.Parallel()

	mw := authn.RequireTenant("tenant-a")
	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, withPrincipal(identity.Principal{
		Subject:  "u-1",
		TenantID: "tenant-b",
	}))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run on tenant mismatch")
	}
}

func TestRequireTenantAcceptsMatch(t *testing.T) {
	t.Parallel()

	mw := authn.RequireTenant("tenant-a")
	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, withPrincipal(identity.Principal{
		Subject:  "u-1",
		TenantID: "tenant-a",
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !*called {
		t.Fatal("next must run when tenant matches")
	}
}

func TestRequireTenantAnonymousRejected(t *testing.T) {
	t.Parallel()

	mw := authn.RequireTenant("tenant-a")
	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, withPrincipal(identity.Principal{}))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous tenant-check must 401, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run on anonymous principal")
	}
}

func TestRequireTenantRejectsEmptyPrincipalTenant(t *testing.T) {
	t.Parallel()

	// An authenticated Principal whose TenantID is empty is rejected with
	// 403 — the credential carried no tenant scoping and therefore cannot
	// satisfy a tenant-bound route.
	mw := authn.RequireTenant("tenant-a")
	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, withPrincipal(identity.Principal{Subject: "u-1"}))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("empty-tenant principal must 403, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run when principal tenant is empty")
	}
}

func TestRequireAllOfRejectsAtFirstFailure(t *testing.T) {
	t.Parallel()

	// Outer: RequireAuth (passes). Inner: RequireScopes (fails). The
	// composer must surface the inner failure with 403 and skip next.
	composed := authn.RequireAllOf(
		authn.RequireAuth(),
		authn.RequireScopes([]string{"missing-scope"}),
	)

	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	composed(next).ServeHTTP(rec, withPrincipal(identity.Principal{
		Subject: "u-1",
		Scopes:  []string{"read:users"},
	}))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 from scope check, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run when a composed middleware rejects")
	}
}

func TestRequireAllOfShortCircuitsOuter(t *testing.T) {
	t.Parallel()

	// Verifies that the OUTER middleware sees the request first: an
	// anonymous principal triggers RequireAuth's 401 and the inner scope
	// middleware never runs (so it cannot upgrade to 403).
	composed := authn.RequireAllOf(
		authn.RequireAuth(),
		authn.RequireScopes([]string{"read:users"}),
	)

	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	composed(next).ServeHTTP(rec, withPrincipal(identity.Principal{}))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("outer RequireAuth must 401 first, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run on anonymous principal")
	}
}

func TestRequireAllOfPassesWhenAllPass(t *testing.T) {
	t.Parallel()

	composed := authn.RequireAllOf(
		authn.RequireAuth(),
		authn.RequireScopes([]string{"read:users"}),
		authn.RequireTenant("tenant-a"),
	)

	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	composed(next).ServeHTTP(rec, withPrincipal(identity.Principal{
		Subject:  "u-1",
		TenantID: "tenant-a",
		Scopes:   []string{"read:users"},
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when all requirements pass, got %d", rec.Code)
	}
	if !*called {
		t.Fatal("next must run when every requirement passes")
	}
}

func TestRequireAllOfEmptyIsIdentity(t *testing.T) {
	t.Parallel()

	// Empty composer is the neutral element: every request passes
	// through unchanged.
	mw := authn.RequireAllOf()
	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, withPrincipal(identity.Principal{}))

	if rec.Code != http.StatusOK || !*called {
		t.Fatalf("empty RequireAllOf must pass through: code=%d called=%v", rec.Code, *called)
	}
}

func TestErrorWriterOptionCustomizesResponse(t *testing.T) {
	t.Parallel()

	opts := authn.Options{
		ErrorWriter: func(w http.ResponseWriter, _ *http.Request, status int, reason string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"` + reason + `"}`))
		},
	}

	mw := authn.RequireAuth(opts)
	next, called := nextRecorder()
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, withPrincipal(identity.Principal{}))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("ErrorWriter override ignored; Content-Type=%q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"error":`) {
		t.Fatalf("ErrorWriter body not used: %q", body)
	}
	if *called {
		t.Fatal("next must not run after a custom-written 401")
	}
}

// TestIdentityToAuthnIntegration exercises the cross-block contract: an
// identity.Middleware that produces a scope-bearing Principal must flow
// through authn.RequireScopes without any glue code.
func TestIdentityToAuthnIntegration(t *testing.T) {
	t.Parallel()

	// Resolver that produces a principal with a single scope.
	resolverWithScope := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{
			Subject:    "u-1",
			Scopes:     []string{"read:users"},
			AuthMethod: "bearer",
		}, nil
	})

	resolverWithoutScope := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{
			Subject:    "u-2",
			Scopes:     []string{"write:posts"},
			AuthMethod: "bearer",
		}, nil
	})

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sanity: the downstream handler must see the Principal attached
		// upstream — proving the chain preserves request context.
		if identity.PrincipalFromContext(r.Context()).IsAnonymous() {
			t.Fatal("downstream handler saw anonymous principal")
		}
		w.WriteHeader(http.StatusOK)
	})

	t.Run("scope present passes both middlewares", func(t *testing.T) {
		t.Parallel()
		mw := identity.Middleware(resolverWithScope)
		guarded := mw(authn.RequireScopes([]string{"read:users"})(final))
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 with matching scope, got %d", rec.Code)
		}
	})

	t.Run("scope missing yields 403 from authn layer", func(t *testing.T) {
		t.Parallel()
		mw := identity.Middleware(resolverWithoutScope)
		guarded := mw(authn.RequireScopes([]string{"read:users"})(final))
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 from authn after identity attached scopeless principal, got %d", rec.Code)
		}
	})
}
