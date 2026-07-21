// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package authz — authz.go owns the Options type, the RequestBuilder
// interface plus its function adapter, and the Middleware factory that
// runs an authz.Evaluator against an *http.Request.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package authz

import (
	"net/http"

	coreauthz "github.com/septagon-oss/pk-core/pkg/authz"
)

// Options configures how the authz middleware shapes denial responses.
//
// ErrorWriter, when non-nil, fully owns the response: it MUST write the
// status header and any body the application requires. When nil the
// middleware falls back to defaultErrorWriter, which writes the supplied
// reason as a plain-text body via http.Error.
//
// Options is a value type. Pass it by value; the middleware factory
// captures a copy so later mutation of caller-side Options cannot alter
// previously-configured middlewares.
type Options struct {
	// ErrorWriter, if non-nil, writes the denial response. It is called
	// with the underlying ResponseWriter, the request that triggered the
	// denial, the status code the middleware would otherwise have used,
	// and a short human-readable reason string. Implementations MUST NOT
	// call next on a denial; the middleware has already short-circuited
	// the chain.
	ErrorWriter func(w http.ResponseWriter, r *http.Request, status int, reason string)
}

// RequestBuilder translates an *http.Request into a coreauthz.Request.
// This is the only application-aware piece of authz wiring — the policy
// evaluator itself is provider-neutral and resource-agnostic.
//
// Implementations typically pull:
//
//   - the Principal from identity context (via PrincipalFromRequest);
//   - the Action from request method (or a custom annotation);
//   - the Resource from URL path segments, headers, or body fields.
//
// Returning a non-nil error signals that the request is malformed for
// authorization purposes (missing required path parameter, invalid
// resource ID, etc.); the middleware translates that to 400 Bad
// Request. Builders MUST NOT use errors to express "denied" — denial is
// the evaluator's job.
type RequestBuilder interface {
	Build(r *http.Request) (coreauthz.Request, error)
}

// RequestBuilderFunc adapts an ordinary function to RequestBuilder,
// mirroring http.HandlerFunc. Use this for lightweight per-route
// builders or in-test builders.
type RequestBuilderFunc func(r *http.Request) (coreauthz.Request, error)

// Build satisfies RequestBuilder.
func (f RequestBuilderFunc) Build(r *http.Request) (coreauthz.Request, error) {
	return f(r)
}

// Middleware returns HTTP middleware that runs evaluator against the
// coreauthz.Request produced by builder for each incoming request. The
// decision-to-status mapping is fixed:
//
//   - DecisionAllow → next runs;
//   - DecisionDeny → 403 Forbidden;
//   - DecisionAbstain → 403 Forbidden (treated as deny by default);
//   - Any other decision (DecisionNotEvaluated, future values) → 403
//     Forbidden. The middleware refuses to interpret unknown decisions
//     permissively.
//
// Error paths:
//
//   - builder.Build returns a non-nil error → 400 Bad Request.
//   - evaluator.Evaluate returns a non-nil error → 500 Internal Server
//     Error. The middleware does not surface the engine's error
//     message in the response body to avoid leaking internals.
//
// If evaluator or builder is nil the middleware short-circuits with
// 500 — Middleware will not assume permissive defaults when wiring is
// missing.
func Middleware(evaluator coreauthz.Evaluator, builder RequestBuilder, opts ...Options) func(http.Handler) http.Handler {
	o := firstOptions(opts)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if evaluator == nil || builder == nil {
				writeError(w, r, o, http.StatusInternalServerError, "authz: middleware misconfigured")
				return
			}
			req, err := builder.Build(r)
			if err != nil {
				writeError(w, r, o, http.StatusBadRequest, "authz: invalid request")
				return
			}
			result, err := evaluator.Evaluate(r.Context(), req)
			if err != nil {
				writeError(w, r, o, http.StatusInternalServerError, "authz: evaluator error")
				return
			}
			switch result.Decision {
			case coreauthz.DecisionAllow:
				next.ServeHTTP(w, r)
			case coreauthz.DecisionDeny, coreauthz.DecisionAbstain:
				writeError(w, r, o, http.StatusForbidden, "authz: forbidden")
			default:
				writeError(w, r, o, http.StatusForbidden, "authz: forbidden")
			}
		})
	}
}

// firstOptions returns opts[0] when supplied, or a zero Options
// otherwise. Centralised so the variadic option pattern stays uniform.
func firstOptions(opts []Options) Options {
	if len(opts) == 0 {
		return Options{}
	}
	return opts[0]
}

// writeError dispatches the denial response to the configured
// ErrorWriter or the package default. Centralised so every error path
// honours the same Options contract without duplicating the nil-check.
func writeError(w http.ResponseWriter, r *http.Request, opts Options, status int, reason string) {
	if opts.ErrorWriter != nil {
		opts.ErrorWriter(w, r, status, reason)
		return
	}
	defaultErrorWriter(w, r, status, reason)
}

// defaultErrorWriter is the package-default error-response writer.
func defaultErrorWriter(w http.ResponseWriter, _ *http.Request, status int, reason string) {
	http.Error(w, reason, status)
}
