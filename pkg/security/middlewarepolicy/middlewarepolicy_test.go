// Validates: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package middlewarepolicy_test — middlewarepolicy_test.go is the
// external contract test suite for the middlewarepolicy package. It
// exercises Chain ordering and identity semantics, SkipIf behaviour,
// and each built-in Predicate (PathPrefixSkip, MethodSkip,
// BearerAuthSkip).
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package middlewarepolicy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/security/middlewarepolicy"
)

// taggingMiddleware appends tag to a slice owned by trace each time it
// runs, so tests can assert call order across composed chains.
func taggingMiddleware(trace *[]string, tag string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*trace = append(*trace, tag)
			next.ServeHTTP(w, r)
		})
	}
}

func TestChainAppliesMiddlewaresInOrder(t *testing.T) {
	t.Parallel()

	var trace []string
	chain := middlewarepolicy.Chain(
		taggingMiddleware(&trace, "outer"),
		taggingMiddleware(&trace, "middle"),
		taggingMiddleware(&trace, "inner"),
	)

	finalCalled := false
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		finalCalled = true
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	chain(final).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !finalCalled {
		t.Fatal("final handler must run")
	}
	want := []string{"outer", "middle", "inner"}
	if len(trace) != len(want) {
		t.Fatalf("trace length mismatch: got %v want %v", trace, want)
	}
	for i := range want {
		if trace[i] != want[i] {
			t.Fatalf("trace[%d] = %q, want %q (full trace: %v)", i, trace[i], want[i], trace)
		}
	}
}

func TestChainEmptyIsIdentity(t *testing.T) {
	t.Parallel()

	chain := middlewarepolicy.Chain()
	called := false
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})

	rec := httptest.NewRecorder()
	chain(final).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Fatal("empty chain must invoke final handler unchanged")
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("empty chain must not alter response: got %d", rec.Code)
	}
}

func TestChainSkipsNilEntries(t *testing.T) {
	t.Parallel()

	var trace []string
	chain := middlewarepolicy.Chain(
		nil,
		taggingMiddleware(&trace, "only"),
		nil,
	)

	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	chain(final).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if len(trace) != 1 || trace[0] != "only" {
		t.Fatalf("nil entries must be skipped, got trace=%v", trace)
	}
}

func TestSkipIfSkipsWhenPredicateTrue(t *testing.T) {
	t.Parallel()

	mwRan := false
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mwRan = true
			next.ServeHTTP(w, r)
		})
	}

	finalRan := false
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		finalRan = true
	})

	guarded := middlewarepolicy.SkipIf(func(*http.Request) bool { return true }, mw)
	rec := httptest.NewRecorder()
	guarded(final).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if mwRan {
		t.Fatal("middleware must NOT run when predicate is true")
	}
	if !finalRan {
		t.Fatal("final handler must still run when middleware is skipped")
	}
}

func TestSkipIfRunsMwWhenPredicateFalse(t *testing.T) {
	t.Parallel()

	mwRan := false
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mwRan = true
			next.ServeHTTP(w, r)
		})
	}

	finalRan := false
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		finalRan = true
	})

	guarded := middlewarepolicy.SkipIf(func(*http.Request) bool { return false }, mw)
	rec := httptest.NewRecorder()
	guarded(final).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !mwRan {
		t.Fatal("middleware must run when predicate is false")
	}
	if !finalRan {
		t.Fatal("final handler must run after middleware")
	}
}

func TestSkipIfNilPredicateNeverSkips(t *testing.T) {
	t.Parallel()

	mwRan := false
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mwRan = true
			next.ServeHTTP(w, r)
		})
	}
	final := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	guarded := middlewarepolicy.SkipIf(nil, mw)
	rec := httptest.NewRecorder()
	guarded(final).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !mwRan {
		t.Fatal("nil predicate must default to running mw")
	}
}

func TestPathPrefixSkipMatchesExact(t *testing.T) {
	t.Parallel()

	p := middlewarepolicy.PathPrefixSkip("/health", "/ready")
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	if !p(r) {
		t.Fatal("exact match on /health must return true")
	}
}

func TestPathPrefixSkipMatchesPrefix(t *testing.T) {
	t.Parallel()

	p := middlewarepolicy.PathPrefixSkip("/health")
	r := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	if !p(r) {
		t.Fatal("/health/live must match prefix /health")
	}
}

func TestPathPrefixSkipDoesNotMatchSuperset(t *testing.T) {
	t.Parallel()

	// "/healthz" must NOT match "/health" — only "/health" itself or
	// "/health/..." subpaths are valid matches.
	p := middlewarepolicy.PathPrefixSkip("/health")
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	if p(r) {
		t.Fatal("/healthz must NOT match /health prefix list")
	}
}

func TestPathPrefixSkipEmptyMatchesNothing(t *testing.T) {
	t.Parallel()

	p := middlewarepolicy.PathPrefixSkip()
	r := httptest.NewRequest(http.MethodGet, "/anything", nil)
	if p(r) {
		t.Fatal("empty prefix list must match nothing")
	}
}

func TestMethodSkipIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	p := middlewarepolicy.MethodSkip("GET", "HEAD", "OPTIONS")

	cases := []struct {
		method string
		want   bool
	}{
		{"GET", true},
		{"get", true},
		{"Get", true},
		{"HEAD", true},
		{"head", true},
		{"OPTIONS", true},
		{"POST", false},
		{"DELETE", false},
	}

	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, "/", nil)
		if got := p(r); got != tc.want {
			t.Errorf("MethodSkip(%q) = %v, want %v", tc.method, got, tc.want)
		}
	}
}

func TestBearerAuthSkipDetectsBearer(t *testing.T) {
	t.Parallel()

	p := middlewarepolicy.BearerAuthSkip()

	cases := []struct {
		header string
		want   bool
	}{
		{"Bearer eyJhbGciOi", true},
		{"bearer eyJhbGciOi", true},
		{"BEARER eyJhbGciOi", true},
		{"BeArEr eyJhbGciOi", true},
	}

	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", tc.header)
		if got := p(r); got != tc.want {
			t.Errorf("BearerAuthSkip(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}

func TestBearerAuthSkipIgnoresNonBearer(t *testing.T) {
	t.Parallel()

	p := middlewarepolicy.BearerAuthSkip()

	cases := []struct {
		header string
	}{
		{""},                 // missing header
		{"Basic dXNlcjpwdw"}, // Basic auth
		{"Token abc"},        // OAuth1-style
		{"Bear"},             // too short to match
	}

	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if tc.header != "" {
			r.Header.Set("Authorization", tc.header)
		}
		if p(r) {
			t.Errorf("BearerAuthSkip(%q) must NOT match", tc.header)
		}
	}
}
