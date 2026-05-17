// Package authz_test — authz_test.go is the external contract test
// suite for the security/authz package. It exercises Middleware
// decision mapping, RequestBuilder/Func behaviour, the
// PrincipalFromRequest mapping helper, the Options.ErrorWriter
// extension hook, and a cross-block integration test demonstrating an
// identity.Middleware → security/authz.Middleware chain backed by a
// real coreauthz.PolicyEvaluator.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package authz_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreauthz "github.com/septagon-oss/pk-core/pkg/authz"
	"github.com/septagon-oss/pk-core/pkg/security/authz"
	"github.com/septagon-oss/pk-core/pkg/security/identity"
)

// stubEvaluator returns canned (Result, error) pairs so each Middleware
// test case can drive the decision branch in isolation from the policy
// engine itself.
type stubEvaluator struct {
	result coreauthz.Result
	err    error
	called bool
}

func (s *stubEvaluator) Evaluate(_ context.Context, _ coreauthz.Request) (coreauthz.Result, error) {
	s.called = true
	return s.result, s.err
}

// staticBuilder returns a fixed coreauthz.Request, useful for tests
// that only need the evaluator branch under test.
func staticBuilder(req coreauthz.Request) authz.RequestBuilder {
	return authz.RequestBuilderFunc(func(*http.Request) (coreauthz.Request, error) {
		return req, nil
	})
}

func nextRecorder() (http.Handler, *bool) {
	called := false
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}), &called
}

func TestMiddlewareAllowsOnAllow(t *testing.T) {
	t.Parallel()

	eval := &stubEvaluator{result: coreauthz.Result{Decision: coreauthz.DecisionAllow}}
	mw := authz.Middleware(eval, staticBuilder(coreauthz.Request{Action: "read"}))

	next, called := nextRecorder()
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on allow, got %d", rec.Code)
	}
	if !*called {
		t.Fatal("next must run on allow")
	}
	if !eval.called {
		t.Fatal("evaluator must be consulted")
	}
}

func TestMiddlewareDeniesOnDeny403(t *testing.T) {
	t.Parallel()

	eval := &stubEvaluator{result: coreauthz.Result{Decision: coreauthz.DecisionDeny, PolicyID: "p1"}}
	mw := authz.Middleware(eval, staticBuilder(coreauthz.Request{Action: "read"}))

	next, called := nextRecorder()
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on deny, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run on deny")
	}
}

func TestMiddlewareDeniesOnAbstain403ByDefault(t *testing.T) {
	t.Parallel()

	eval := &stubEvaluator{result: coreauthz.Result{Decision: coreauthz.DecisionAbstain}}
	mw := authz.Middleware(eval, staticBuilder(coreauthz.Request{Action: "read"}))

	next, called := nextRecorder()
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on abstain, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run on abstain (default policy)")
	}
}

func TestMiddleware400OnBuilderError(t *testing.T) {
	t.Parallel()

	eval := &stubEvaluator{result: coreauthz.Result{Decision: coreauthz.DecisionAllow}}
	builder := authz.RequestBuilderFunc(func(*http.Request) (coreauthz.Request, error) {
		return coreauthz.Request{}, errors.New("missing path param")
	})
	mw := authz.Middleware(eval, builder)

	next, called := nextRecorder()
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on builder error, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run on builder error")
	}
	if eval.called {
		t.Fatal("evaluator must not be consulted when builder fails")
	}
}

func TestMiddleware500OnEvaluatorError(t *testing.T) {
	t.Parallel()

	eval := &stubEvaluator{err: errors.New("engine down")}
	mw := authz.Middleware(eval, staticBuilder(coreauthz.Request{Action: "read"}))

	next, called := nextRecorder()
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on evaluator error, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run on evaluator error")
	}
}

func TestMiddleware500OnNilWiring(t *testing.T) {
	t.Parallel()

	// Nil evaluator. Middleware must refuse to assume permissive defaults
	// when wiring is missing — it returns 500 so misconfiguration surfaces
	// loudly rather than silently authorizing every request.
	mw := authz.Middleware(nil, staticBuilder(coreauthz.Request{Action: "read"}))
	next, called := nextRecorder()
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on nil evaluator, got %d", rec.Code)
	}
	if *called {
		t.Fatal("next must not run on misconfigured middleware")
	}
}

func TestRequestBuilderFuncSatisfiesInterface(t *testing.T) {
	t.Parallel()

	want := coreauthz.Request{Action: "read", Principal: coreauthz.Principal{ID: "u-1"}}
	var b authz.RequestBuilder = authz.RequestBuilderFunc(func(*http.Request) (coreauthz.Request, error) {
		return want, nil
	})

	got, err := b.Build(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if got.Action != want.Action || got.Principal.ID != want.Principal.ID {
		t.Fatalf("Build returned unexpected request: %+v", got)
	}
}

func TestPrincipalFromRequestMapsIdentityPrincipal(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(identity.ContextWithPrincipal(r.Context(), identity.Principal{
		Subject:    "u-42",
		TenantID:   "tenant-a",
		Scopes:     []string{"read:users", "write:users"},
		AuthMethod: "bearer",
	}))

	got := authz.PrincipalFromRequest(r)

	if got.ID != "u-42" {
		t.Fatalf("ID mismatch: %q", got.ID)
	}
	if got.TenantID != "tenant-a" {
		t.Fatalf("TenantID mismatch: %q", got.TenantID)
	}
	if len(got.Roles) != 2 || got.Roles[0] != "read:users" || got.Roles[1] != "write:users" {
		t.Fatalf("Roles mapping unexpected: %+v", got.Roles)
	}
}

func TestPrincipalFromRequestAnonymousReturnsZero(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	got := authz.PrincipalFromRequest(r)
	if got.ID != "" || got.TenantID != "" || len(got.Roles) != 0 {
		t.Fatalf("anonymous request must yield zero Principal, got %+v", got)
	}
}

func TestPrincipalFromRequestNilSafe(t *testing.T) {
	t.Parallel()

	got := authz.PrincipalFromRequest(nil)
	if got.ID != "" || got.TenantID != "" || len(got.Roles) != 0 {
		t.Fatalf("nil request must yield zero Principal, got %+v", got)
	}
}

func TestErrorWriterOptionCustomizesResponse(t *testing.T) {
	t.Parallel()

	opts := authz.Options{
		ErrorWriter: func(w http.ResponseWriter, _ *http.Request, status int, reason string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"` + reason + `"}`))
		},
	}

	eval := &stubEvaluator{result: coreauthz.Result{Decision: coreauthz.DecisionDeny}}
	mw := authz.Middleware(eval, staticBuilder(coreauthz.Request{Action: "read"}), opts)

	next, called := nextRecorder()
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("ErrorWriter override ignored; Content-Type=%q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"error":`) {
		t.Fatalf("ErrorWriter body not used: %q", body)
	}
	if *called {
		t.Fatal("next must not run after a custom-written 403")
	}
}

// TestIdentityToAuthzIntegration exercises the cross-block contract: an
// identity.Middleware attaches a Principal; a builder pulls it through
// PrincipalFromRequest; a real coreauthz.PolicyEvaluator decides. Both
// allow and deny paths are exercised to prove the wiring is honest end
// to end.
func TestIdentityToAuthzIntegration(t *testing.T) {
	t.Parallel()

	// A single allow policy: role "admin" may perform action "read" on
	// resource type "user" in module "users".
	policy := coreauthz.Policy{
		ID:        "allow-admin-read-user",
		ModuleID:  "users",
		Effect:    coreauthz.EffectAllow,
		Actions:   []string{"read"},
		Resources: []string{"user"},
		Roles:     []string{"admin"},
	}
	evaluator, err := coreauthz.NewPolicyEvaluator(policy)
	if err != nil {
		t.Fatalf("NewPolicyEvaluator: %v", err)
	}

	// Builder reads the principal from the request and constructs an
	// action/resource pair from the path.
	builder := authz.RequestBuilderFunc(func(r *http.Request) (coreauthz.Request, error) {
		return coreauthz.Request{
			Principal: authz.PrincipalFromRequest(r),
			Action:    "read",
			Resource: coreauthz.Resource{
				Type:     "user",
				ModuleID: "users",
			},
		}, nil
	})

	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("admin scope yields allow", func(t *testing.T) {
		t.Parallel()
		resolver := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
			return identity.Principal{
				Subject:    "u-1",
				Scopes:     []string{"admin"},
				AuthMethod: "bearer",
			}, nil
		})

		guarded := identity.Middleware(resolver)(authz.Middleware(evaluator, builder)(final))
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/1", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 with admin scope, got %d", rec.Code)
		}
	})

	t.Run("non-admin scope abstains -> 403", func(t *testing.T) {
		t.Parallel()
		resolver := identity.ResolverFunc(func(*http.Request) (identity.Principal, error) {
			return identity.Principal{
				Subject:    "u-2",
				Scopes:     []string{"reader"},
				AuthMethod: "bearer",
			}, nil
		})

		guarded := identity.Middleware(resolver)(authz.Middleware(evaluator, builder)(final))
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/1", nil))

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for non-admin scope, got %d", rec.Code)
		}
	})
}
