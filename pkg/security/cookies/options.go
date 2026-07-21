// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package cookies — options.go owns the Option functional-options type and
// every WithFoo helper. Splitting options into their own file keeps the
// extension surface obvious: downstream Pro adds new per-call hooks here,
// the immutable security profile stays in cookies.go.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package cookies

import "time"

// option is the internal config a builder accumulates from caller options.
type option struct {
	maxAge    time.Duration
	maxAgeSet bool
}

// Option mutates the per-call configuration. Use With* helpers to construct
// values rather than building option structs directly — the unexported
// fields are an implementation detail.
type Option func(*option)

// WithMaxAge overrides the default MaxAge for this call. A negative
// duration clears the cookie (used internally by BuildClear); zero
// produces a session cookie (dies when the browser closes).
func WithMaxAge(d time.Duration) Option {
	return func(o *option) {
		o.maxAge = d
		o.maxAgeSet = true
	}
}
