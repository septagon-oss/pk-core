// Validates: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

package headers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/security/headers"
)

// noopHandler is a tiny inner handler used to drive the middleware under test.
func noopHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestMiddlewareSetsAllDefaultHeaders(t *testing.T) {
	t.Parallel()

	h := headers.Middleware()(noopHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	got := rec.Header()
	if v := got.Get("Strict-Transport-Security"); !strings.HasPrefix(v, "max-age=") {
		t.Errorf("Strict-Transport-Security missing or malformed: %q", v)
	}
	if v := got.Get("X-Frame-Options"); v != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", v)
	}
	if v := got.Get("Referrer-Policy"); v != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", v)
	}
	if v := got.Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", v)
	}
	// Default Config has no CSP directives → no CSP header.
	if v := got.Get("Content-Security-Policy"); v != "" {
		t.Errorf("Content-Security-Policy should be empty by default, got %q", v)
	}
	// Default Config has no Permissions-Policy.
	if v := got.Get("Permissions-Policy"); v != "" {
		t.Errorf("Permissions-Policy should be empty by default, got %q", v)
	}
}

func TestNonceIsFreshPerRequest(t *testing.T) {
	t.Parallel()

	mw := headers.Middleware(headers.Config{
		CSPDirectives: map[string]string{
			"default-src": "'self'",
			"script-src":  "'self' 'nonce-%s'",
		},
	})
	h := mw(noopHandler())

	seen := make(map[string]struct{})
	for i := range 3 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		csp := rec.Header().Get("Content-Security-Policy")
		if csp == "" {
			t.Fatalf("iteration %d: missing CSP header", i)
		}
		// Extract the nonce from the rendered CSP.
		_, after, ok := strings.Cut(csp, "'nonce-")
		if !ok {
			t.Fatalf("iteration %d: CSP missing nonce token: %q", i, csp)
		}
		rest := after
		end := strings.Index(rest, "'")
		if end < 0 {
			t.Fatalf("iteration %d: unterminated nonce token: %q", i, csp)
		}
		nonce := rest[:end]
		if nonce == "" {
			t.Fatalf("iteration %d: empty nonce", i)
		}
		if _, dup := seen[nonce]; dup {
			t.Fatalf("iteration %d: nonce repeated across requests: %q", i, nonce)
		}
		seen[nonce] = struct{}{}
	}
}

func TestNonceFromContextReturnsNonceInsideHandler(t *testing.T) {
	t.Parallel()

	var ctxNonce string
	mw := headers.Middleware(headers.Config{
		CSPDirectives: map[string]string{
			"script-src": "'self' 'nonce-%s'",
		},
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxNonce = headers.NonceFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if ctxNonce == "" {
		t.Fatal("expected NonceFromContext to return a non-empty nonce inside the handler")
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "'nonce-"+ctxNonce+"'") {
		t.Fatalf("CSP %q does not contain the context nonce %q", csp, ctxNonce)
	}

	// NonceFromContext deliberately treats a nil context as absent metadata;
	// keep this defensive contract covered without passing a nil literal in
	// ordinary call-site shape.
	var nilContext context.Context
	if got := headers.NonceFromContext(nilContext); got != "" {
		t.Fatalf("NonceFromContext(nil) = %q, want \"\"", got)
	}
}

func TestCSPDirectivesMerge(t *testing.T) {
	t.Parallel()

	base := headers.Config{
		CSPDirectives: map[string]string{
			"default-src": "'self'",
			"script-src":  "'self'",
		},
	}
	override := headers.Config{
		CSPDirectives: map[string]string{
			"script-src": "'self' https://cdn.example.com",
			"style-src":  "'self' 'unsafe-inline'",
		},
	}

	mw := headers.Middleware(base, override)
	rec := httptest.NewRecorder()
	mw(noopHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("expected base default-src to survive, got %q", csp)
	}
	if !strings.Contains(csp, "script-src 'self' https://cdn.example.com") {
		t.Errorf("expected override script-src to win, got %q", csp)
	}
	if !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") {
		t.Errorf("expected override style-src to appear, got %q", csp)
	}
}

func TestPermissionsPolicyHeaderRespectsConfig(t *testing.T) {
	t.Parallel()

	const policy = "geolocation=(), camera=(self)"
	mw := headers.Middleware(headers.Config{PermissionsPolicy: policy})
	rec := httptest.NewRecorder()
	mw(noopHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := rec.Header().Get("Permissions-Policy"); got != policy {
		t.Errorf("Permissions-Policy = %q, want %q", got, policy)
	}

	// Empty PermissionsPolicy => no header emitted.
	mw2 := headers.Middleware(headers.Config{})
	rec2 := httptest.NewRecorder()
	mw2(noopHandler()).ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec2.Header().Get("Permissions-Policy"); got != "" {
		t.Errorf("Permissions-Policy should be unset when empty, got %q", got)
	}
}

func TestClearManagedResponseHeadersRemovesAll(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	for _, name := range headers.ManagedResponseHeaderNames() {
		h.Set(name, "managed")
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Application-Header", "keep")

	headers.ClearManagedResponseHeaders(h)

	for _, name := range headers.ManagedResponseHeaderNames() {
		if got := h.Get(name); got != "" {
			t.Errorf("expected %s to be cleared, got %q", name, got)
		}
	}
	if got := h.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("expected Content-Type to remain, got %q", got)
	}
	if got := h.Get("X-Application-Header"); got != "keep" {
		t.Errorf("expected custom header to remain, got %q", got)
	}
}

func TestManagedResponseHeaderNamesIsStable(t *testing.T) {
	t.Parallel()

	a := headers.ManagedResponseHeaderNames()
	b := headers.ManagedResponseHeaderNames()
	if len(a) != len(b) {
		t.Fatalf("len = %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("name[%d] = %q vs %q", i, a[i], b[i])
		}
	}

	// Returned slices must be independent copies (mutating one must not
	// affect the other).
	a[0] = "MUTATED"
	c := headers.ManagedResponseHeaderNames()
	if c[0] == "MUTATED" {
		t.Fatal("ManagedResponseHeaderNames must return a defensive copy")
	}

	// The list must include the canonical security headers, in sorted order.
	sorted := make([]string, len(c))
	copy(sorted, c)
	sort.Strings(sorted)
	for i := range c {
		if c[i] != sorted[i] {
			t.Errorf("ManagedResponseHeaderNames is not sorted: %v", c)
			break
		}
	}
}

func TestCSPReportOnlyChangesHeaderName(t *testing.T) {
	t.Parallel()

	mw := headers.Middleware(headers.Config{
		CSPReportOnly: true,
		CSPDirectives: map[string]string{
			"default-src": "'self'",
		},
	})
	rec := httptest.NewRecorder()
	mw(noopHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := rec.Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("expected Content-Security-Policy to be empty when report-only, got %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy-Report-Only"); got == "" {
		t.Errorf("expected Content-Security-Policy-Report-Only header to be set")
	}
}

func TestHSTSConfigVariants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cfg  headers.Config
		want string
	}{
		{
			name: "default one year",
			cfg:  headers.Config{},
			want: "max-age=31536000",
		},
		{
			name: "subdomains and preload",
			cfg: headers.Config{
				HSTSMaxAge:            2 * 24 * time.Hour,
				HSTSIncludeSubdomains: true,
				HSTSPreload:           true,
			},
			want: "max-age=172800; includeSubDomains; preload",
		},
		{
			name: "disabled",
			cfg:  headers.Config{HSTSMaxAge: -1 * time.Second},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mw := headers.Middleware(tc.cfg)
			rec := httptest.NewRecorder()
			mw(noopHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			if got := rec.Header().Get("Strict-Transport-Security"); got != tc.want {
				t.Errorf("HSTS = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWithContentTypeOptionsDisablesNosniff(t *testing.T) {
	t.Parallel()

	mw := headers.Middleware(headers.WithContentTypeOptions(headers.Config{}, false))
	rec := httptest.NewRecorder()
	mw(noopHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := rec.Header().Get("X-Content-Type-Options"); got != "" {
		t.Errorf("X-Content-Type-Options should be off, got %q", got)
	}
}
