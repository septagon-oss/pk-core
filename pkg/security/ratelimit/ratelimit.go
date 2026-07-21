// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package ratelimit — ratelimit.go owns the Limiter interface, the
// LimiterFunc adapter, and the ClientIPKey helper. These are the
// extension surfaces the rest of the package — and downstream Pro
// limiters (Redis, leaky-bucket, etc.) — implement against.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"time"
)

// Limiter is the provider-neutral rate-limit contract. Implementations
// must be safe for concurrent use; one Limiter value services many
// requests in flight.
//
// Allow checks whether one request for key may proceed now. Returns
// (true, 0) when the request is admitted; returns (false, retryAfter)
// when the request must be rejected, with retryAfter being a hint to
// the caller for how long to wait before retrying. retryAfter may be
// zero if the implementation cannot compute it.
type Limiter interface {
	Allow(key string) (allowed bool, retryAfter time.Duration)
}

// LimiterFunc adapts an ordinary function to the Limiter interface,
// mirroring the http.HandlerFunc pattern. Use this for tests or
// one-off limiters in app composition code.
type LimiterFunc func(key string) (bool, time.Duration)

// Allow satisfies Limiter.
func (f LimiterFunc) Allow(key string) (bool, time.Duration) {
	return f(key)
}

// ClientIPKey extracts the client IP from a request for use as a
// rate-limit key. Lookup order:
//
//  1. X-Forwarded-For — the first hop is the original client when the
//     header is set by a trusted proxy. Operators that do NOT terminate
//     traffic behind a trusted proxy must NOT route this function's
//     output as a security boundary, since X-Forwarded-For is
//     attacker-controlled in that topology.
//  2. X-Real-IP — many proxies set this instead of X-Forwarded-For.
//  3. r.RemoteAddr — the actual transport peer; this strips the port.
//
// Returns "" if none of the above produced a usable IP. Callers
// receiving "" should treat the request as un-keyable and decide
// policy: a sensible default is to bypass the limiter rather than
// share a single bucket among un-keyable requests (which would let an
// attacker pin everyone's bucket by spoofing zero-IP).
func ClientIPKey(r *http.Request) string {
	if r == nil {
		return ""
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For: client, proxy1, proxy2, ...
		first, _, _ := strings.Cut(xff, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return canonicalIP(ip)
		}
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return canonicalIP(xri)
	}

	if r.RemoteAddr != "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil && host != "" {
			return canonicalIP(host)
		}
		// RemoteAddr without a port (rare; tests sometimes set this).
		return canonicalIP(r.RemoteAddr)
	}

	return ""
}

// canonicalIP normalizes an IP literal for stable keying. Returns the
// input unchanged if it is not a parseable IP — the caller still gets a
// deterministic key shape, which is what the limiter cares about. IPv6
// addresses are returned without surrounding brackets.
func canonicalIP(s string) string {
	s = strings.TrimSpace(s)
	// strip brackets on IPv6 ([::1])
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return s
}
