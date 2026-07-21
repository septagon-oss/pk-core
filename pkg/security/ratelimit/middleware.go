// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package ratelimit — middleware.go owns the Config struct and the
// Middleware factory that binds any Limiter implementation to the
// standard func(http.Handler) http.Handler shape. The factory captures
// all hooks at construction so the hot path stays branch-light.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package ratelimit

import (
	"net/http"
	"strconv"
	"time"
)

// Config configures the HTTP rate-limit middleware. All fields are
// optional; the zero-value Config falls back to ClientIPKey for keying,
// no skipping, and no observability callback.
type Config struct {
	// KeyFunc extracts the rate-limit key from a request. Nil falls back
	// to ClientIPKey. Returning "" causes the request to bypass the
	// limiter — see the ClientIPKey documentation for the rationale.
	KeyFunc func(r *http.Request) string

	// SkipFunc, if non-nil, returns true to bypass the limiter for this
	// request. Useful for whitelisting internal monitors or health
	// probes by header, path, or authenticated identity. Evaluated
	// before KeyFunc.
	SkipFunc func(r *http.Request) bool

	// OnLimited, if non-nil, is invoked when a request is rate-limited,
	// after the 429 response has been written. Useful for metrics,
	// audit logs, and structured alerting. The callback must not block
	// the request goroutine for long.
	OnLimited func(r *http.Request, retryAfter time.Duration)
}

// Middleware returns rate-limit middleware backed by limiter. Limited
// requests receive HTTP 429 with a Retry-After header (seconds, rounded
// up) and a "rate limit exceeded" body. The Limiter argument is the
// replacement boundary — pass a TokenBucket, a Redis-backed limiter, or
// any LimiterFunc; the middleware does not care which.
//
// If limiter is nil the middleware degrades to a pass-through. This
// avoids forcing tests and disabled-feature code paths to wire a no-op
// limiter by hand.
func Middleware(limiter Limiter, cfg Config) func(http.Handler) http.Handler {
	keyFunc := cfg.KeyFunc
	if keyFunc == nil {
		keyFunc = ClientIPKey
	}
	skipFunc := cfg.SkipFunc
	onLimited := cfg.OnLimited

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil {
				next.ServeHTTP(w, r)
				return
			}
			if skipFunc != nil && skipFunc(r) {
				next.ServeHTTP(w, r)
				return
			}
			key := keyFunc(r)
			if key == "" {
				// Un-keyable request. See ClientIPKey docs: bypassing is
				// safer than sharing a single bucket across un-keyable
				// requests.
				next.ServeHTTP(w, r)
				return
			}
			allowed, retryAfter := limiter.Allow(key)
			if allowed {
				next.ServeHTTP(w, r)
				return
			}

			seconds := int64(retryAfter / time.Second)
			if retryAfter > 0 && retryAfter%time.Second != 0 {
				seconds++
			}
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)

			if onLimited != nil {
				onLimited(r, retryAfter)
			}
		})
	}
}
