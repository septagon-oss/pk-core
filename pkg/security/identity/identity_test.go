// Package identity_test — identity_test.go is the external contract test
// suite for the identity package. It exercises the Composable + Chainable
// properties of Principal, the context plumbing, IdentityResolver,
// ResolverFunc, Chain, and Middleware — plus a cross-block integration
// test (TestCookieResolverIntegration) that demonstrates a real resolver
// reading a session cookie produced by pk-core/pkg/security/cookies.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package identity_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/security/cookies"
	"github.com/septagon-oss/pk-core/pkg/security/identity"
)

func TestPrincipalIsAnonymousByDefault(t *testing.T) {
	t.Parallel()

	var p identity.Principal
	if !p.IsAnonymous() {
		t.Fatal("zero Principal must be anonymous")
	}
}

func TestPrincipalIsAnonymousReturnsFalseWhenSubjectSet(t *testing.T) {
	t.Parallel()

	p := identity.Principal{Subject: "user-123"}
	if p.IsAnonymous() {
		t.Fatal("Principal with Subject set must not be anonymous")
	}
}

func TestPrincipalHasScopeFindsScope(t *testing.T) {
	t.Parallel()

	p := identity.Principal{
		Subject: "user-123",
		Scopes:  []string{"read:users", "write:users", "admin"},
	}
	if !p.HasScope("write:users") {
		t.Fatal("HasScope should find a scope that is present")
	}
}

func TestPrincipalHasScopeReturnsFalseForMissing(t *testing.T) {
	t.Parallel()

	p := identity.Principal{
		Subject: "user-123",
		Scopes:  []string{"read:users"},
	}
	if p.HasScope("admin") {
		t.Fatal("HasScope must return false when the scope is absent")
	}
	if p.HasScope("") {
		t.Fatal("HasScope must return false for the empty scope")
	}

	var empty identity.Principal
	if empty.HasScope("read:users") {
		t.Fatal("HasScope must return false on a nil Scopes slice")
	}
}

func TestContextWithPrincipalRoundTrip(t *testing.T) {
	t.Parallel()

	want := identity.Principal{
		Subject:    "user-42",
		TenantID:   "tenant-x",
		Scopes:     []string{"read", "write"},
		AuthMethod: "cookie",
	}
	ctx := identity.ContextWithPrincipal(context.Background(), want)
	got := identity.PrincipalFromContext(ctx)
	if got.Subject != want.Subject ||
		got.TenantID != want.TenantID ||
		got.AuthMethod != want.AuthMethod ||
		len(got.Scopes) != len(want.Scopes) {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, want)
	}
}

func TestPrincipalFromContextReturnsAnonymousByDefault(t *testing.T) {
	t.Parallel()

	got := identity.PrincipalFromContext(context.Background())
	if !got.IsAnonymous() {
		t.Fatalf("expected anonymous principal from empty context, got %+v", got)
	}

	// nil context: also anonymous, no panic. We pass nil deliberately to
	// pin defensive behavior; production callers never do this.
	var nilCtx context.Context
	got2 := identity.PrincipalFromContext(nilCtx)
	if !got2.IsAnonymous() {
		t.Fatalf("expected anonymous principal from nil context, got %+v", got2)
	}
}

func TestResolverFuncSatisfiesInterface(t *testing.T) {
	t.Parallel()

	var r identity.IdentityResolver = identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{Subject: "func-resolver"}, nil
	})
	p, err := r.Resolve(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Subject != "func-resolver" {
		t.Fatalf("ResolverFunc adapter dropped subject: got %q", p.Subject)
	}
}

func TestChainReturnsFirstNonAnonymous(t *testing.T) {
	t.Parallel()

	anon := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{}, nil
	})
	authed := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{Subject: "second-wins", AuthMethod: "bearer"}, nil
	})
	never := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		t.Fatal("third resolver must not be consulted after second returns non-anonymous")
		return identity.Principal{}, nil
	})

	c := identity.Chain(anon, authed, never)
	p, err := c.Resolve(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Subject != "second-wins" || p.AuthMethod != "bearer" {
		t.Fatalf("chain did not return second resolver's principal: %+v", p)
	}
}

func TestChainReturnsAnonymousIfAllAnonymous(t *testing.T) {
	t.Parallel()

	anon1 := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{}, nil
	})
	anon2 := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{}, nil
	})

	c := identity.Chain(anon1, anon2)
	p, err := c.Resolve(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !p.IsAnonymous() {
		t.Fatalf("chain of anonymous resolvers must return anonymous, got %+v", p)
	}
}

func TestChainShortCircuitsOnError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("invalid bearer")
	failing := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{}, wantErr
	})
	never := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		t.Fatal("second resolver must not be consulted after first returns error")
		return identity.Principal{}, nil
	})

	c := identity.Chain(failing, never)
	_, err := c.Resolve(httptest.NewRequest(http.MethodGet, "/", nil))
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected chain to surface wantErr, got %v", err)
	}
}

func TestChainSatisfiesResolverInterface(t *testing.T) {
	t.Parallel()

	// Demonstrates closure under composition: a Chain itself can be nested
	// inside another Chain. Pro adds JWT/OAuth/SAML/SCIM as additional
	// resolvers without changing core.
	inner := identity.Chain(
		identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
			return identity.Principal{}, nil
		}),
		identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
			return identity.Principal{Subject: "deep-nested", AuthMethod: "oauth"}, nil
		}),
	)

	outer := identity.Chain(
		identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
			return identity.Principal{}, nil
		}),
		inner,
	)

	p, err := outer.Resolve(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("nested chain: unexpected error: %v", err)
	}
	if p.Subject != "deep-nested" {
		t.Fatalf("nested chain dropped subject: %+v", p)
	}
}

func TestMiddlewareAttachesPrincipal(t *testing.T) {
	t.Parallel()

	resolver := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{Subject: "u-1", AuthMethod: "bearer"}, nil
	})

	var seen identity.Principal
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = identity.PrincipalFromContext(r.Context())
	})

	mw := identity.Middleware(resolver)
	rec := httptest.NewRecorder()
	mw(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if seen.Subject != "u-1" || seen.AuthMethod != "bearer" {
		t.Fatalf("downstream handler did not see attached principal: %+v", seen)
	}
}

func TestMiddlewareWrites401OnResolverError(t *testing.T) {
	t.Parallel()

	resolver := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{}, errors.New("bad credentials")
	})

	called := false
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})

	mw := identity.Middleware(resolver)
	rec := httptest.NewRecorder()
	mw(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if called {
		t.Fatal("next handler must not be invoked when resolver errors")
	}
}

func TestMiddlewareAllowsAnonymousThrough(t *testing.T) {
	t.Parallel()

	resolver := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{}, nil
	})

	var seen identity.Principal
	called := false
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		seen = identity.PrincipalFromContext(r.Context())
	})

	mw := identity.Middleware(resolver)
	rec := httptest.NewRecorder()
	mw(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Fatal("next handler must run on anonymous resolution")
	}
	if !seen.IsAnonymous() {
		t.Fatalf("expected anonymous principal, got %+v", seen)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestMiddlewareNilResolverDegradesToAnonymous(t *testing.T) {
	t.Parallel()

	var seen identity.Principal
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = identity.PrincipalFromContext(r.Context())
	})

	mw := identity.Middleware(nil)
	rec := httptest.NewRecorder()
	mw(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !seen.IsAnonymous() {
		t.Fatalf("nil resolver must attach anonymous principal, got %+v", seen)
	}
}

// TestCookieResolverIntegration proves cross-block composition: a resolver
// reads a session cookie produced via pk-core/pkg/security/cookies.Build
// (KindSession) and turns it into a Principal. This is the exact pattern
// a cookie-based authentication module would use — and the integration
// only references public APIs from both packages.
func TestCookieResolverIntegration(t *testing.T) {
	t.Parallel()

	// A trivial "session token to subject" lookup. Real authentication
	// modules consult a session store or verify a JWT — the resolver
	// shape is identical.
	sessionStore := map[string]identity.Principal{
		"sess-abc": {
			Subject:    "user-7",
			TenantID:   "tenant-a",
			Scopes:     []string{"read:dashboard"},
			AuthMethod: "cookie",
		},
	}

	sessionName, err := cookies.Name(cookies.KindSession)
	if err != nil {
		t.Fatalf("cookies.Name(KindSession): %v", err)
	}

	cookieResolver := identity.ResolverFunc(func(r *http.Request) (identity.Principal, error) {
		c, err := r.Cookie(sessionName)
		if err != nil {
			// http.ErrNoCookie -> request is unauthenticated, not failed.
			return identity.Principal{}, nil
		}
		p, ok := sessionStore[c.Value]
		if !ok {
			// Cookie present but unknown -> credentials presented but invalid.
			return identity.Principal{}, errors.New("identity: unknown session token")
		}
		return p, nil
	})

	// Build a session cookie via the cookies block, then attach it to a
	// request and run the resolver through the Middleware.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	sessionCookie, err := cookies.Build(req, cookies.KindSession, "sess-abc")
	if err != nil {
		t.Fatalf("cookies.Build: %v", err)
	}
	req.AddCookie(sessionCookie)

	var seen identity.Principal
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = identity.PrincipalFromContext(r.Context())
	})

	rec := httptest.NewRecorder()
	identity.Middleware(cookieResolver)(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if seen.Subject != "user-7" {
		t.Fatalf("cookie resolver did not produce expected principal: %+v", seen)
	}
	if seen.TenantID != "tenant-a" {
		t.Fatalf("cookie resolver dropped tenant ID: %+v", seen)
	}
	if seen.AuthMethod != "cookie" {
		t.Fatalf("cookie resolver dropped auth method: %+v", seen)
	}
	if !seen.HasScope("read:dashboard") {
		t.Fatalf("cookie resolver dropped scopes: %+v", seen)
	}

	// Negative path: unknown session token -> resolver errors -> 401.
	badReq := httptest.NewRequest(http.MethodGet, "/", nil)
	badCookie, err := cookies.Build(badReq, cookies.KindSession, "sess-unknown")
	if err != nil {
		t.Fatalf("cookies.Build: %v", err)
	}
	badReq.AddCookie(badCookie)

	badRec := httptest.NewRecorder()
	identity.Middleware(cookieResolver)(handler).ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown session must produce 401, got %d", badRec.Code)
	}
}
