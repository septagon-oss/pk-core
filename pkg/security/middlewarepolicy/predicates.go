// Package middlewarepolicy — predicates.go owns the Predicate type,
// the SkipIf wrapper, and the built-in predicates PathPrefixSkip,
// MethodSkip, and BearerAuthSkip.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package middlewarepolicy

import (
	"net/http"
	"strings"
)

// Predicate decides whether a wrapped middleware should be SKIPPED for
// a given request. Returning true means "skip the middleware and run
// next directly"; false means "run the middleware". A named function
// type lets callers pass plain function literals and the built-in
// helpers interchangeably.
type Predicate func(r *http.Request) bool

// SkipIf wraps mw so that when predicate returns true, mw is skipped
// entirely — next.ServeHTTP runs directly without ever entering mw.
// Common use: CSRF middleware that should not run on bearer-token API
// requests; rate-limit middleware that should ignore health probes.
//
// A nil predicate behaves as "never skip" so the wrapped middleware
// always runs. A nil mw collapses to an identity middleware (next
// runs unchanged).
func SkipIf(predicate Predicate, mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if mw == nil {
			return next
		}
		wrapped := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if predicate != nil && predicate(r) {
				next.ServeHTTP(w, r)
				return
			}
			wrapped.ServeHTTP(w, r)
		})
	}
}

// PathPrefixSkip returns a Predicate that matches when r.URL.Path
// equals any of prefixes exactly OR is prefixed by "prefix/". Useful
// for "skip this middleware on /health, /ready, /metrics". The match
// avoids the common foot-gun where "/healthz" would wrongly match a
// prefix list containing "/health" — only "/health" itself and
// "/health/..." sub-paths are matched.
//
// Empty prefixes argument or empty entries produce a predicate that
// matches nothing (returns false for every request); this lets
// callers wire optional skip lists from configuration without
// nil-guarding.
func PathPrefixSkip(prefixes ...string) Predicate {
	// Filter empty entries once at construction so the per-request
	// loop stays tight.
	cleaned := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		cleaned = append(cleaned, p)
	}
	return func(r *http.Request) bool {
		if r == nil {
			return false
		}
		path := r.URL.Path
		for _, prefix := range cleaned {
			if path == prefix {
				return true
			}
			if strings.HasPrefix(path, prefix+"/") {
				return true
			}
		}
		return false
	}
}

// MethodSkip returns a Predicate that matches when r.Method is one of
// methods (case-insensitive). Useful for "skip CSRF on safe methods"
// — pass GET, HEAD, OPTIONS.
//
// Empty methods argument produces a predicate that matches nothing.
func MethodSkip(methods ...string) Predicate {
	cleaned := make([]string, 0, len(methods))
	for _, m := range methods {
		if m == "" {
			continue
		}
		cleaned = append(cleaned, m)
	}
	return func(r *http.Request) bool {
		if r == nil {
			return false
		}
		for _, m := range cleaned {
			if strings.EqualFold(r.Method, m) {
				return true
			}
		}
		return false
	}
}

// BearerAuthSkip returns a Predicate that matches when the request's
// Authorization header begins with "Bearer " (case-insensitive).
// Common pattern: skip browser-oriented CSRF protection on API
// endpoints authenticated by bearer tokens, where Same-Origin Policy
// alone does not protect the credential.
//
// Requests without an Authorization header do not match.
func BearerAuthSkip() Predicate {
	const prefix = "bearer "
	return func(r *http.Request) bool {
		if r == nil {
			return false
		}
		v := r.Header.Get("Authorization")
		if len(v) < len(prefix) {
			return false
		}
		return strings.EqualFold(v[:len(prefix)], prefix)
	}
}
