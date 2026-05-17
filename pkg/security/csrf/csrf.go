// Package csrf — csrf.go owns the Config struct, the Middleware factory,
// and the double-submit validation/rotation logic. Cookie writes are
// delegated to pk-core/pkg/security/cookies with KindCSRF so the
// canonical security profile (SameSite=Strict, HttpOnly=false, Path=/)
// is applied uniformly across every PlatformKit deployment.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package csrf

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/septagon-oss/pk-core/pkg/security/cookies"
)

// DefaultHeaderName is the request header consulted for the CSRF token on
// unsafe-method requests.
const DefaultHeaderName = "X-CSRF-Token"

// DefaultTokenLength is the default number of cryptographically random
// bytes generated per token before base64 encoding. 32 bytes (256 bits)
// matches OWASP guidance for double-submit token strength.
const DefaultTokenLength = 32

// Config configures the CSRF middleware. Zero values fall back to safe
// defaults: HeaderName=DefaultHeaderName, TokenLength=DefaultTokenLength,
// ExemptPaths=nil.
type Config struct {
	// HeaderName is the request header consulted for the CSRF token on
	// unsafe-method requests. Empty falls back to DefaultHeaderName.
	HeaderName string

	// TokenLength is the byte length of the raw random token before
	// base64 encoding. Zero falls back to DefaultTokenLength.
	TokenLength int

	// ExemptPaths is a list of URL path prefixes that skip the CSRF check
	// entirely. Useful for webhook endpoints whose authentication mechanism
	// is unrelated to browser cookies. Match uses strings.HasPrefix.
	ExemptPaths []string
}

// Middleware returns an HTTP middleware that enforces CSRF protection via
// the double-submit cookie pattern.
//
// Safe methods (GET, HEAD, OPTIONS) pass through unchanged but receive a
// fresh CSRF cookie if none is already set.
//
// Unsafe methods (POST, PUT, PATCH, DELETE) require both:
//
//   - a CSRF cookie carrying the canonical name from
//     cookies.Name(cookies.KindCSRF); and
//   - a request header (HeaderName) whose value matches the cookie value
//     under constant-time comparison.
//
// On any unsafe-method validation success, a fresh cookie is issued
// (rotation), so a leaked token has a single-use window.
//
// Multiple Configs merge left-to-right: HeaderName/TokenLength last-wins,
// ExemptPaths concatenates.
func Middleware(cfgs ...Config) func(http.Handler) http.Handler {
	cfg := mergeConfigs(cfgs...)

	headerName := cfg.HeaderName
	if headerName == "" {
		headerName = DefaultHeaderName
	}
	tokenLength := cfg.TokenLength
	if tokenLength <= 0 {
		tokenLength = DefaultTokenLength
	}
	exempt := append([]string(nil), cfg.ExemptPaths...)

	// Resolve the canonical CSRF cookie name once at construction so the
	// hot path skips the cookies.Name lookup.
	cookieName, err := cookies.Name(cookies.KindCSRF)
	if err != nil {
		// cookies.KindCSRF is a built-in profile; this branch is
		// unreachable in well-formed builds. Fail closed if it ever
		// becomes reachable to avoid silently disabling CSRF.
		panic("csrf: cookies.Name(KindCSRF) failed: " + err.Error())
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ExemptPaths bypass the check entirely. The cookie is also
			// not auto-issued on exempt paths because such endpoints are
			// typically machine-to-machine and have no JS to mirror it.
			if isExemptPath(r.URL.Path, exempt) {
				next.ServeHTTP(w, r)
				return
			}

			cookie, _ := r.Cookie(cookieName)
			cookieValue := ""
			if cookie != nil {
				cookieValue = cookie.Value
			}

			if isSafeMethod(r.Method) {
				if cookieValue == "" {
					token, genErr := generateToken(tokenLength)
					if genErr != nil {
						http.Error(w, "csrf: token generation failed", http.StatusInternalServerError)
						return
					}
					if writeErr := cookies.Write(w, r, cookies.KindCSRF, token); writeErr != nil {
						http.Error(w, "csrf: cookie write failed", http.StatusInternalServerError)
						return
					}
				}
				next.ServeHTTP(w, r)
				return
			}

			// Unsafe method: validate.
			headerValue := r.Header.Get(headerName)
			if cookieValue == "" || headerValue == "" || !tokensMatch(cookieValue, headerValue) {
				http.Error(w, "csrf: invalid token", http.StatusForbidden)
				return
			}

			// Rotate after every successful unsafe-method validation so a
			// leaked token has a single-use window.
			fresh, genErr := generateToken(tokenLength)
			if genErr != nil {
				http.Error(w, "csrf: token rotation failed", http.StatusInternalServerError)
				return
			}
			if writeErr := cookies.Write(w, r, cookies.KindCSRF, fresh); writeErr != nil {
				http.Error(w, "csrf: cookie write failed", http.StatusInternalServerError)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ClearResponseCookie writes a clearing CSRF cookie header to h. Callers
// use this on logout flows so the browser drops the CSRF token alongside
// the session cookie. Returns an error only if the CSRF Kind profile is
// somehow missing — this is unreachable in well-formed builds.
func ClearResponseCookie(h http.Header) error {
	cookie, err := cookies.BuildClear(nil, cookies.KindCSRF)
	if err != nil {
		return err
	}
	if v := cookie.String(); v != "" {
		h.Add("Set-Cookie", v)
	}
	return nil
}

// mergeConfigs resolves an effective Config from a variadic input list.
// HeaderName/TokenLength use last-non-zero-wins; ExemptPaths concatenate.
func mergeConfigs(cfgs ...Config) Config {
	var out Config
	for _, c := range cfgs {
		if c.HeaderName != "" {
			out.HeaderName = c.HeaderName
		}
		if c.TokenLength != 0 {
			out.TokenLength = c.TokenLength
		}
		if len(c.ExemptPaths) > 0 {
			out.ExemptPaths = append(out.ExemptPaths, c.ExemptPaths...)
		}
	}
	return out
}

// isSafeMethod reports whether method is one of the HTTP-spec-defined
// methods that do not mutate server state. Safe methods are exempt from
// CSRF validation.
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// isExemptPath reports whether path starts with any prefix in prefixes.
func isExemptPath(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// generateToken returns a base64-RawURL-encoded random token of length
// derived from byteLen. Returns an error only if the crypto/rand source
// fails — that is operating-system-level catastrophic and the caller
// should fail the request closed.
func generateToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// tokensMatch compares two tokens in constant time, returning true iff
// they have equal length and equal contents.
func tokensMatch(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
