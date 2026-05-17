// Package headers — headers.go owns the Config struct, the Middleware
// factory, the CSP nonce context plumbing, and the managed-header
// inventory used by clear-and-overwrite response paths.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package headers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// nonceContextKey is a private type so external packages cannot collide
// with our context key and cannot read or write the nonce except via
// NonceFromContext.
type nonceContextKey struct{}

// DefaultHSTSMaxAge is the Strict-Transport-Security max-age applied when a
// caller's Config leaves HSTSMaxAge at zero. One year matches the HSTS
// preload eligibility window so deployments that opt into preload do not
// have to redefine the constant.
const DefaultHSTSMaxAge = 365 * 24 * time.Hour

// DefaultFrameOptions is the X-Frame-Options value applied when a caller's
// Config leaves FrameOptions empty. "DENY" is the safest default for an
// application that does not embed itself in frames.
const DefaultFrameOptions = "DENY"

// DefaultReferrerPolicy is the Referrer-Policy applied when a caller's
// Config leaves ReferrerPolicy empty. "strict-origin-when-cross-origin"
// is the modern browser default and matches OWASP guidance.
const DefaultReferrerPolicy = "strict-origin-when-cross-origin"

// managedResponseHeaderNames is the canonical, alphabetized list of every
// response header this package writes. ManagedResponseHeaderNames returns
// a defensive copy of this slice; ClearManagedResponseHeaders walks it to
// remove the headers from an http.Header.
var managedResponseHeaderNames = []string{
	"Content-Security-Policy",
	"Content-Security-Policy-Report-Only",
	"Permissions-Policy",
	"Referrer-Policy",
	"Strict-Transport-Security",
	"X-Content-Type-Options",
	"X-Frame-Options",
}

// Config controls which security headers the middleware emits and how.
// The zero value is safe to use: HSTSMaxAge falls back to DefaultHSTSMaxAge,
// FrameOptions to DefaultFrameOptions, ReferrerPolicy to
// DefaultReferrerPolicy, and ContentTypeOptions to true (emitting "nosniff").
//
// To omit a header entirely, set its field to the zero value semantically
// associated with "off": HSTSMaxAge=-1 disables HSTS, FrameOptions=""
// disables X-Frame-Options (only after merge resolution), ReferrerPolicy=""
// disables Referrer-Policy, ContentTypeOptions=false disables nosniff,
// PermissionsPolicy="" disables Permissions-Policy, and CSPDirectives
// being empty disables Content-Security-Policy.
//
// Pro/downstream packages extend the policy by supplying additional Configs
// to Middleware; the variadic merge is the contribution mechanism.
type Config struct {
	// HSTSMaxAge controls the Strict-Transport-Security max-age directive.
	// Zero falls back to DefaultHSTSMaxAge. A negative value disables HSTS.
	HSTSMaxAge time.Duration

	// HSTSIncludeSubdomains adds "; includeSubDomains" to the HSTS header
	// when true. Only emitted if HSTS itself is emitted.
	HSTSIncludeSubdomains bool

	// HSTSPreload adds "; preload" to the HSTS header when true. Only
	// emitted if HSTS itself is emitted. Browsers require max-age >= 1 year
	// AND includeSubDomains for preload eligibility; this package does not
	// validate either — that is the operator's responsibility.
	HSTSPreload bool

	// FrameOptions controls the X-Frame-Options value. Empty falls back to
	// DefaultFrameOptions; "-" disables the header.
	FrameOptions string

	// ReferrerPolicy controls the Referrer-Policy value. Empty falls back
	// to DefaultReferrerPolicy; "-" disables the header.
	ReferrerPolicy string

	// ContentTypeOptions, when true, emits X-Content-Type-Options: nosniff.
	// Defaults to true via the merge step.
	ContentTypeOptions bool

	// CSPDirectives is the Content-Security-Policy directive map. Keys are
	// directive names (e.g., "default-src", "script-src"). Values may
	// contain a "%s" placeholder; if so, the middleware substitutes a
	// freshly generated per-request nonce into the placeholder. Empty
	// map (or nil) disables the CSP header.
	CSPDirectives map[string]string

	// CSPReportOnly, when true, emits the policy as
	// Content-Security-Policy-Report-Only instead of Content-Security-Policy.
	// Useful for rollout / observation periods.
	CSPReportOnly bool

	// PermissionsPolicy is the raw Permissions-Policy header value.
	// Empty disables the header.
	PermissionsPolicy string

	// contentTypeOptionsSet tracks whether the caller explicitly set
	// ContentTypeOptions so the merge step can distinguish "leave at
	// default" from "explicitly disabled". Internal; not part of the
	// public API.
	contentTypeOptionsSet bool
}

// WithContentTypeOptions returns a copy of cfg with ContentTypeOptions
// explicitly set, distinguishing "leave at default true" from "explicitly
// false". Callers that want to disable nosniff must use this helper so the
// variadic merge respects their intent.
func WithContentTypeOptions(cfg Config, enabled bool) Config {
	cfg.ContentTypeOptions = enabled
	cfg.contentTypeOptionsSet = true
	return cfg
}

// ManagedResponseHeaderNames returns the canonical list of response headers
// this package writes. Callers (e.g., a cache writer or a proxy response
// path that owns its own policy) use this list to clear the managed headers
// before writing their own.
func ManagedResponseHeaderNames() []string {
	names := make([]string, len(managedResponseHeaderNames))
	copy(names, managedResponseHeaderNames)
	return names
}

// ClearManagedResponseHeaders removes the managed header set from h. It
// is safe to call when none of the managed headers are present.
func ClearManagedResponseHeaders(h http.Header) {
	for _, name := range managedResponseHeaderNames {
		h.Del(name)
	}
}

// NonceFromContext returns the per-request CSP nonce attached by Middleware.
// Returns "" if Middleware did not run or did not attach a nonce (e.g.,
// the configured CSPDirectives contain no "%s" placeholder).
func NonceFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(nonceContextKey{}).(string); ok {
		return v
	}
	return ""
}

// Middleware returns an HTTP middleware that applies the resolved security
// header policy to every response and attaches a fresh CSP nonce to each
// request context.
//
// Multiple Configs merge left-to-right: scalar fields use last-wins
// semantics, CSPDirectives are map-merged by key (last value wins per
// key), and bool fields use logical-or for HSTSIncludeSubdomains/
// HSTSPreload/CSPReportOnly so opting in via any Config is enough.
func Middleware(cfgs ...Config) func(http.Handler) http.Handler {
	effective := merge(cfgs...)

	// Pre-compute the directive ordering so the rendered CSP header is
	// deterministic across requests — easier to test and easier to spot
	// in network panels.
	directiveKeys := make([]string, 0, len(effective.CSPDirectives))
	for k := range effective.CSPDirectives {
		directiveKeys = append(directiveKeys, k)
	}
	sort.Strings(directiveKeys)

	hstsValue := buildHSTSValue(effective)
	cspNeedsNonce := cspContainsPlaceholder(effective.CSPDirectives)
	cspHeaderName := "Content-Security-Policy"
	if effective.CSPReportOnly {
		cspHeaderName = "Content-Security-Policy-Report-Only"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			if hstsValue != "" {
				h.Set("Strict-Transport-Security", hstsValue)
			}
			if effective.FrameOptions != "" && effective.FrameOptions != "-" {
				h.Set("X-Frame-Options", effective.FrameOptions)
			}
			if effective.ReferrerPolicy != "" && effective.ReferrerPolicy != "-" {
				h.Set("Referrer-Policy", effective.ReferrerPolicy)
			}
			if effective.ContentTypeOptions {
				h.Set("X-Content-Type-Options", "nosniff")
			}
			if effective.PermissionsPolicy != "" {
				h.Set("Permissions-Policy", effective.PermissionsPolicy)
			}

			if len(directiveKeys) > 0 {
				nonce := ""
				if cspNeedsNonce {
					nonce = generateNonce()
					r = r.WithContext(context.WithValue(r.Context(), nonceContextKey{}, nonce))
				}
				h.Set(cspHeaderName, renderCSP(directiveKeys, effective.CSPDirectives, nonce))
			}

			next.ServeHTTP(w, r)
		})
	}
}

// merge resolves an effective Config from a variadic input list. Scalar
// string and time.Duration fields use last-non-zero-wins, bool fields use
// logical-or, and CSPDirectives are map-merged by key. ContentTypeOptions
// defaults to true unless any Config explicitly set it via
// WithContentTypeOptions.
func merge(cfgs ...Config) Config {
	out := Config{
		FrameOptions:       DefaultFrameOptions,
		ReferrerPolicy:     DefaultReferrerPolicy,
		ContentTypeOptions: true,
	}
	for _, c := range cfgs {
		if c.HSTSMaxAge != 0 {
			out.HSTSMaxAge = c.HSTSMaxAge
		}
		if c.HSTSIncludeSubdomains {
			out.HSTSIncludeSubdomains = true
		}
		if c.HSTSPreload {
			out.HSTSPreload = true
		}
		if c.FrameOptions != "" {
			out.FrameOptions = c.FrameOptions
		}
		if c.ReferrerPolicy != "" {
			out.ReferrerPolicy = c.ReferrerPolicy
		}
		if c.contentTypeOptionsSet {
			out.ContentTypeOptions = c.ContentTypeOptions
		}
		if c.CSPReportOnly {
			out.CSPReportOnly = true
		}
		if c.PermissionsPolicy != "" {
			out.PermissionsPolicy = c.PermissionsPolicy
		}
		if len(c.CSPDirectives) > 0 {
			if out.CSPDirectives == nil {
				out.CSPDirectives = make(map[string]string, len(c.CSPDirectives))
			}
			for k, v := range c.CSPDirectives {
				out.CSPDirectives[k] = v
			}
		}
	}
	return out
}

// buildHSTSValue renders the Strict-Transport-Security header value or
// returns "" when HSTS is disabled (HSTSMaxAge < 0). Zero falls back to
// DefaultHSTSMaxAge.
func buildHSTSValue(c Config) string {
	maxAge := c.HSTSMaxAge
	if maxAge == 0 {
		maxAge = DefaultHSTSMaxAge
	}
	if maxAge < 0 {
		return ""
	}
	seconds := int64(maxAge / time.Second)
	var b strings.Builder
	b.WriteString("max-age=")
	b.WriteString(strconv.FormatInt(seconds, 10))
	if c.HSTSIncludeSubdomains {
		b.WriteString("; includeSubDomains")
	}
	if c.HSTSPreload {
		b.WriteString("; preload")
	}
	return b.String()
}

// cspContainsPlaceholder reports whether any directive value contains a
// "%s" nonce placeholder.
func cspContainsPlaceholder(directives map[string]string) bool {
	for _, v := range directives {
		if strings.Contains(v, "%s") {
			return true
		}
	}
	return false
}

// renderCSP builds the Content-Security-Policy header value from the
// directive map. keys are passed in pre-sorted so the output is stable.
// If nonce is non-empty, "%s" placeholders are substituted.
func renderCSP(keys []string, directives map[string]string, nonce string) string {
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(k)
		v := directives[k]
		if v != "" {
			b.WriteByte(' ')
			if nonce != "" && strings.Contains(v, "%s") {
				b.WriteString(strings.ReplaceAll(v, "%s", nonce))
			} else {
				b.WriteString(v)
			}
		}
	}
	return b.String()
}

// generateNonce returns a freshly generated 16-byte CSP nonce,
// base64-RawURL-encoded for safe use inside CSP directive values.
func generateNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
