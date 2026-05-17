// Package cors — cors.go owns the Config struct and the Middleware factory.
// The factory captures the allowlist by defensive copy so callers cannot
// mutate the slice after construction to widen access.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package cors

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Config configures the CORS middleware. AllowedOrigins is matched exactly;
// for wildcard or regex matching supply an OriginAllowed function instead.
type Config struct {
	// AllowedOrigins is the exact-match origin allowlist. The slice is
	// defensively copied at Middleware construction; mutations to the
	// caller's slice afterward do not affect the running middleware.
	// Ignored when OriginAllowed is non-nil.
	AllowedOrigins []string

	// AllowedMethods enumerates the HTTP methods echoed back in
	// Access-Control-Allow-Methods on preflight. Empty falls back to
	// ["GET","POST","PUT","DELETE","OPTIONS"].
	AllowedMethods []string

	// AllowedHeaders enumerates the request headers echoed back in
	// Access-Control-Allow-Headers on preflight. Empty falls back to
	// ["Content-Type","Authorization"].
	AllowedHeaders []string

	// ExposeHeaders enumerates response headers exposed to the browser
	// scripts via Access-Control-Expose-Headers. Empty means no
	// Access-Control-Expose-Headers header is emitted.
	ExposeHeaders []string

	// AllowCredentials, when true, sets Access-Control-Allow-Credentials
	// to "true" on responses to allowed origins. Browsers refuse to honor
	// credentials with Access-Control-Allow-Origin: *, so this middleware
	// always echoes the request Origin (never "*") when an origin is
	// allowed.
	AllowCredentials bool

	// MaxAge is the preflight cache duration written to
	// Access-Control-Max-Age. Zero means no header is emitted.
	MaxAge time.Duration

	// OriginAllowed is an optional custom matcher. When non-nil it is
	// consulted instead of AllowedOrigins; returning true admits the
	// origin. This is the Pro/downstream extension surface for wildcards,
	// regex, or per-tenant matching.
	OriginAllowed func(origin string) bool
}

// Middleware returns an HTTP middleware that enforces the CORS policy in
// cfg. The middleware:
//
//   - On preflight (OPTIONS + Access-Control-Request-Method) returns 204
//     with the policy headers if the origin is allowed.
//   - On simple requests, echoes Access-Control-Allow-Origin and
//     Access-Control-Allow-Credentials/Expose-Headers as appropriate, then
//     passes through to the inner handler.
//   - On disallowed origins (or no Origin header), passes through without
//     emitting any Access-Control-* headers. CORS rejection happens in
//     the browser; the server-side concern is "do not lie about what we
//     allow."
func Middleware(cfg Config) func(http.Handler) http.Handler {
	// Defensive copy: callers cannot mutate the slice after construction.
	allowed := make([]string, len(cfg.AllowedOrigins))
	copy(allowed, cfg.AllowedOrigins)
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		allowedSet[o] = struct{}{}
	}

	methods := cfg.AllowedMethods
	if len(methods) == 0 {
		methods = []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
		}
	}
	headersAllowed := cfg.AllowedHeaders
	if len(headersAllowed) == 0 {
		headersAllowed = []string{"Content-Type", "Authorization"}
	}

	methodsHeader := strings.Join(methods, ", ")
	headersAllowedHeader := strings.Join(headersAllowed, ", ")
	exposeHeader := strings.Join(cfg.ExposeHeaders, ", ")
	maxAgeHeader := ""
	if cfg.MaxAge > 0 {
		maxAgeHeader = strconv.FormatInt(int64(cfg.MaxAge/time.Second), 10)
	}

	matcher := cfg.OriginAllowed
	if matcher == nil {
		matcher = func(origin string) bool {
			_, ok := allowedSet[origin]
			return ok
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			isPreflight := r.Method == http.MethodOptions &&
				r.Header.Get("Access-Control-Request-Method") != ""

			if origin != "" {
				addVary(w.Header(), "Origin")
				if isPreflight {
					addVary(w.Header(), "Access-Control-Request-Method", "Access-Control-Request-Headers")
				}
			}

			if origin == "" || !matcher(origin) {
				// Not CORS, or disallowed origin. Preflight still gets a
				// 204 so the browser does not retry, but without any
				// Access-Control-* headers — the browser will refuse the
				// real request, which is the desired security outcome.
				if isPreflight {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			if cfg.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			if exposeHeader != "" {
				h.Set("Access-Control-Expose-Headers", exposeHeader)
			}

			if isPreflight {
				h.Set("Access-Control-Allow-Methods", methodsHeader)
				h.Set("Access-Control-Allow-Headers", headersAllowedHeader)
				if maxAgeHeader != "" {
					h.Set("Access-Control-Max-Age", maxAgeHeader)
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func addVary(h http.Header, values ...string) {
	for _, value := range values {
		h.Add("Vary", value)
	}
}
