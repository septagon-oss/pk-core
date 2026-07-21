// Validates: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package cors_test — cors_test.go is the external contract test suite
// for the cors package. Tests exercise the observable properties of the
// Composable + Chainable contract: preflight allow + reject, simple
// request pass-through, credentials gating, exposed headers, and the
// OriginAllowed override surface.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package cors_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/security/cors"
)

func noopHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestPreflightReturns204WithAllowOrigin(t *testing.T) {
	t.Parallel()

	mw := cors.Middleware(cors.Config{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         60 * time.Second,
	})
	h := mw(noopHandler())

	req := httptest.NewRequest(http.MethodOptions, "/api/widgets", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "https://app.example.com")
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Errorf("missing Access-Control-Allow-Methods")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Errorf("missing Access-Control-Allow-Headers")
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "60" {
		t.Errorf("Access-Control-Max-Age = %q, want 60", got)
	}
	requireVary(t, rec.Header(), "Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers")
}

func TestSimpleRequestPassesThrough(t *testing.T) {
	t.Parallel()

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	})
	mw := cors.Middleware(cors.Config{
		AllowedOrigins: []string{"https://app.example.com"},
	})
	h := mw(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/widgets", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("inner handler was not invoked")
	}
	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestPreflightRejectsDisallowedOrigin(t *testing.T) {
	t.Parallel()

	mw := cors.Middleware(cors.Config{
		AllowedOrigins: []string{"https://app.example.com"},
	})
	h := mw(noopHandler())

	req := httptest.NewRequest(http.MethodOptions, "/api/widgets", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin should be empty for disallowed origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "" {
		t.Errorf("Access-Control-Allow-Methods should be empty for disallowed origin, got %q", got)
	}
	requireVary(t, rec.Header(), "Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers")
}

func TestCredentialsHeaderOnlyWhenEnabled(t *testing.T) {
	t.Parallel()

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		mw := cors.Middleware(cors.Config{AllowedOrigins: []string{"https://x"}})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "https://x")
		rec := httptest.NewRecorder()
		mw(noopHandler()).ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
			t.Errorf("Access-Control-Allow-Credentials should be empty, got %q", got)
		}
	})

	t.Run("enabled", func(t *testing.T) {
		t.Parallel()
		mw := cors.Middleware(cors.Config{
			AllowedOrigins:   []string{"https://x"},
			AllowCredentials: true,
		})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "https://x")
		rec := httptest.NewRecorder()
		mw(noopHandler()).ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Errorf("Access-Control-Allow-Credentials = %q, want \"true\"", got)
		}
	})
}

func TestExposeHeadersAppearsInResponse(t *testing.T) {
	t.Parallel()

	mw := cors.Middleware(cors.Config{
		AllowedOrigins: []string{"https://x"},
		ExposeHeaders:  []string{"X-Correlation-ID", "X-RateLimit-Remaining"},
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://x")
	rec := httptest.NewRecorder()
	mw(noopHandler()).ServeHTTP(rec, req)

	got := rec.Header().Get("Access-Control-Expose-Headers")
	if !strings.Contains(got, "X-Correlation-ID") || !strings.Contains(got, "X-RateLimit-Remaining") {
		t.Errorf("Access-Control-Expose-Headers = %q, missing expected entries", got)
	}
}

func TestOriginAllowedFuncOverridesList(t *testing.T) {
	t.Parallel()

	mw := cors.Middleware(cors.Config{
		// AllowedOrigins list explicitly excludes the origin under test;
		// OriginAllowed must override it.
		AllowedOrigins: []string{"https://disallowed.example.com"},
		OriginAllowed: func(origin string) bool {
			return strings.HasSuffix(origin, ".example.com")
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://tenant-42.example.com")
	rec := httptest.NewRecorder()
	mw(noopHandler()).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://tenant-42.example.com" {
		t.Errorf("expected OriginAllowed to admit the tenant origin, got Access-Control-Allow-Origin = %q", got)
	}
}

func TestAllowedOriginsDefensivelyCopied(t *testing.T) {
	t.Parallel()

	origins := []string{"https://a.example.com"}
	mw := cors.Middleware(cors.Config{AllowedOrigins: origins})
	// Mutate the caller's slice. The middleware must not pick this up.
	origins[0] = "https://b.example.com"

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://a.example.com")
	rec := httptest.NewRecorder()
	mw(noopHandler()).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://a.example.com" {
		t.Errorf("original origin should still be allowed; got %q", got)
	}
}

func TestNoOriginHeaderPassesThroughCleanly(t *testing.T) {
	t.Parallel()

	called := false
	mw := cors.Middleware(cors.Config{AllowedOrigins: []string{"https://x"}})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil) // no Origin
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("inner handler should be invoked when there is no Origin header")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("non-CORS request should not get Access-Control-Allow-Origin, got %q", got)
	}
}

func requireVary(t *testing.T, h http.Header, want ...string) {
	t.Helper()
	values := strings.Join(h.Values("Vary"), ",")
	for _, value := range want {
		if !strings.Contains(values, value) {
			t.Fatalf("Vary = %q, missing %q", values, value)
		}
	}
}
