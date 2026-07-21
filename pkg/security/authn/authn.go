// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package authn — authn.go owns the Options type, the default error writer,
// and RequireAuth, the baseline middleware that rejects anonymous principals
// with 401 Unauthorized.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package authn

import (
	"net/http"

	"github.com/septagon-oss/pk-core/pkg/security/identity"
)

// Options configures how an authn middleware shapes denial responses.
//
// ErrorWriter, when non-nil, fully owns the response: it MUST write the
// status header and any body the application requires. When nil the
// middleware falls back to defaultErrorWriter, which writes the supplied
// reason as a plain-text body via http.Error.
//
// Options is a value type. Pass it by value; the middleware factories
// capture a copy so later mutation of caller-side Options cannot alter
// previously-configured middlewares.
type Options struct {
	// ErrorWriter, if non-nil, writes the denial response. It is called
	// with the underlying ResponseWriter, the request that triggered the
	// denial, the status code the middleware would otherwise have used
	// (401 for missing auth, 403 for policy failures), and a short human-
	// readable reason string. Implementations MUST NOT call next on a
	// denial; the middleware has already short-circuited the chain.
	ErrorWriter func(w http.ResponseWriter, r *http.Request, status int, reason string)
}

// firstOptions returns opts[0] when supplied, or a zero Options otherwise.
// Centralised so the variadic option pattern stays uniform across every
// middleware factory in the package.
func firstOptions(opts []Options) Options {
	if len(opts) == 0 {
		return Options{}
	}
	return opts[0]
}

// writeError dispatches the denial response to the configured ErrorWriter
// or the package default. Centralised so every middleware honours the
// same Options contract without duplicating the nil-check.
func writeError(w http.ResponseWriter, r *http.Request, opts Options, status int, reason string) {
	if opts.ErrorWriter != nil {
		opts.ErrorWriter(w, r, status, reason)
		return
	}
	defaultErrorWriter(w, r, status, reason)
}

// defaultErrorWriter is the package-default 401/403 response writer.
// It writes the reason as plain text via http.Error so test harnesses and
// curl-style clients see actionable text without needing JSON parsing.
func defaultErrorWriter(w http.ResponseWriter, _ *http.Request, status int, reason string) {
	http.Error(w, reason, status)
}

// RequireAuth returns middleware that rejects anonymous principals with
// 401 Unauthorized. identity.Middleware MUST run earlier in the chain so
// the Principal is already attached to the request context; otherwise
// every request is treated as anonymous.
//
// When the principal is authenticated the middleware delegates to next
// without modification — RequireAuth never re-issues the principal or
// rewrites the request.
func RequireAuth(opts ...Options) func(http.Handler) http.Handler {
	o := firstOptions(opts)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if identity.PrincipalFromContext(r.Context()).IsAnonymous() {
				writeError(w, r, o, http.StatusUnauthorized, "authn: unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
