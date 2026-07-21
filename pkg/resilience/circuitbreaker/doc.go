// Implements: REQ-014.
// Per: ADR-0029.
// Discipline: C-14.

// Package circuitbreaker implements the closed/open/half-open state
// machine that short-circuits calls to a failing downstream and probes
// for recovery without re-overwhelming it.
//
// # Why this package exists
//
// When a downstream is unhealthy, naively forwarding every request
// (a) prolongs the outage by keeping the failing service saturated and
// (b) ties up resources in the caller — goroutines, sockets, connection
// pool entries — that could be released to serve healthy traffic instead.
// A circuit breaker turns those slow failures into fast failures: while a
// downstream is open, calls return ErrOpen immediately. Periodically a
// single probe is allowed through; if it succeeds the breaker closes and
// normal traffic resumes; if it fails the breaker re-opens for another
// cooldown.
//
// # How it works
//
//   - Closed: every call invokes op. Consecutive failures are counted; when
//     they reach FailureThreshold the breaker trips to Open and records the
//     current time.
//   - Open: every call returns ErrOpen without invoking op, until OpenTimeout
//     has elapsed since trip. The next call after timeout transitions the
//     breaker to HalfOpen lazily — there is no background goroutine.
//   - HalfOpen: a bounded number of probe calls (HalfOpenMaxProbes) are
//     allowed through. The first probe success closes the breaker and resets
//     the failure count. A probe failure re-opens the breaker and resets the
//     trip timestamp.
//
// A successful call in Closed state resets the consecutive-failure counter
// to zero, giving the breaker memory-less semantics across periods of
// healthy traffic.
//
// # Composable contract
//
// Breaker is the published interface. Pro/downstream packages can provide
// distributed implementations whose state is shared via Redis or a control
// plane, custom IsFailure predicates that classify error taxonomies, or
// adaptive thresholds — every caller binding to the Breaker interface
// receives the upgrade without code changes.
//
// Config.Clock is the test-injection seam: tests inject a deterministic
// clock to exercise transitions without sleeping.
//
// # External dependencies
//
// None. Implemented from the Go standard library.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package circuitbreaker
