// Validates: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package csrf_test — csrf_test.go is the external contract test suite
// for the csrf package. Tests exercise the observable properties of the
// Composable + Chainable contract: safe-method exemption, unsafe-method
// rejection without/with mismatched tokens, unsafe-method acceptance
// with a matching token, post-success rotation, ExemptPaths bypass,
// ClearResponseCookie semantics, and confirmation that the cookie write
// uses the KindCSRF profile from the cookies block.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package csrf_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/security/cookies"
	"github.com/septagon-oss/pk-core/pkg/security/csrf"
)

// csrfCookieName resolves the canonical CSRF cookie name once for the
// whole suite so tests are immune to a future rename in the cookies
// package — they exercise the contract, not the literal string.
func csrfCookieName(t *testing.T) string {
	t.Helper()
	name, err := cookies.Name(cookies.KindCSRF)
	if err != nil {
		t.Fatalf("cookies.Name(KindCSRF): %v", err)
	}
	return name
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// extractCSRFCookie returns the Set-Cookie entry for the CSRF cookie or
// "" if none is present.
func extractCSRFCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	name := csrfCookieName(t)
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestGetIsExempt(t *testing.T) {
	t.Parallel()

	mw := csrf.Middleware()
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET should pass through, got status %d", rec.Code)
	}
	if c := extractCSRFCookie(t, rec); c == nil {
		t.Fatal("GET should issue a CSRF cookie when none is present")
	}
}

func TestPostWithoutTokenRejected(t *testing.T) {
	t.Parallel()

	mw := csrf.Middleware()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/widgets", nil)
	// Cookie present, but no header.
	req.AddCookie(&http.Cookie{Name: csrfCookieName(t), Value: "abc"})
	mw(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestPostWithValidTokenAccepted(t *testing.T) {
	t.Parallel()

	mw := csrf.Middleware()
	const token = "matching-token-value"

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/widgets", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName(t), Value: token})
	req.Header.Set(csrf.DefaultHeaderName, token)
	mw(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestPostWithMismatchedTokenRejected(t *testing.T) {
	t.Parallel()

	mw := csrf.Middleware()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/widgets", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName(t), Value: "cookie-token"})
	req.Header.Set(csrf.DefaultHeaderName, "header-token")
	mw(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on token mismatch, got %d", rec.Code)
	}
}

func TestTokenRotatesAfterPost(t *testing.T) {
	t.Parallel()

	mw := csrf.Middleware()
	const token = "before-rotation-token"

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/widgets", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName(t), Value: token})
	req.Header.Set(csrf.DefaultHeaderName, token)
	mw(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	c := extractCSRFCookie(t, rec)
	if c == nil {
		t.Fatal("expected fresh CSRF cookie to be set after successful POST")
	}
	if c.Value == token {
		t.Fatalf("expected rotated cookie value, still got %q", c.Value)
	}
}

func TestExemptPathBypassesCheck(t *testing.T) {
	t.Parallel()

	mw := csrf.Middleware(csrf.Config{
		ExemptPaths: []string{"/webhooks/"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", nil)
	// No cookie, no header — still passes because the path is exempt.
	mw(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected exempt path to pass through, got %d", rec.Code)
	}
}

func TestClearResponseCookieRemovesIt(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	if err := csrf.ClearResponseCookie(h); err != nil {
		t.Fatalf("ClearResponseCookie: %v", err)
	}

	values := h.Values("Set-Cookie")
	if len(values) == 0 {
		t.Fatal("expected ClearResponseCookie to add a Set-Cookie entry")
	}
	name := csrfCookieName(t)
	found := false
	for _, v := range values {
		if strings.HasPrefix(v, name+"=") &&
			(strings.Contains(v, "Max-Age=0") || strings.Contains(v, "Expires=")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a clearing Set-Cookie for %q, got %v", name, values)
	}
}

func TestUsesCookiesKindCSRF(t *testing.T) {
	t.Parallel()

	mw := csrf.Middleware()
	rec := httptest.NewRecorder()
	// GET with no existing cookie => middleware issues one via KindCSRF.
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	c := extractCSRFCookie(t, rec)
	if c == nil {
		t.Fatal("expected CSRF cookie to be issued on GET")
	}

	// Confirm the cookie carries the KindCSRF security profile:
	//   - HttpOnly=false (the frontend must read it to mirror into a header)
	//   - SameSite=Strict (defeats cross-site state-changing requests)
	//   - Path=/
	if c.HttpOnly {
		t.Errorf("KindCSRF profile requires HttpOnly=false, got HttpOnly=true")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("KindCSRF profile requires SameSite=Strict, got %v", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("KindCSRF profile requires Path=/, got %q", c.Path)
	}
	if c.Name != csrfCookieName(t) {
		t.Errorf("cookie name = %q, want %q (KindCSRF canonical name)", c.Name, csrfCookieName(t))
	}
}

func TestDeleteRequiresToken(t *testing.T) {
	t.Parallel()

	mw := csrf.Middleware()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/resource/1", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName(t), Value: "token"})
	// No header => rejection.
	mw(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("DELETE without header should be 403, got %d", rec.Code)
	}
}

func TestCustomHeaderNameRespected(t *testing.T) {
	t.Parallel()

	mw := csrf.Middleware(csrf.Config{HeaderName: "X-Custom-CSRF"})
	const token = "tk"

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName(t), Value: token})
	req.Header.Set("X-Custom-CSRF", token)
	mw(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("custom header should be honored, got %d", rec.Code)
	}
}
