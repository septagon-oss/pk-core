// Implements: REQ-014.
// Per: ADR-0029.
// Discipline: C-14.

// Package retry provides a configurable Retrier that re-runs a fallible
// Operation under a Policy describing how many attempts to make, how to
// space them out, and which errors are worth retrying.
//
// # Why this package exists
//
// Transient failures — flaky network hops, momentary contention on a
// downstream, rate-limit blips — are the rule, not the exception, in any
// distributed system. Calling code that hand-rolls a retry loop tends to
// (a) get the backoff math wrong, (b) miss the context-cancellation case,
// (c) retry non-retryable errors (auth failures, validation), or
// (d) thunder-herd a recovering downstream because there is no jitter.
// Centralizing the loop behind one audited primitive eliminates those
// failure modes and gives every call site the same observable shape.
//
// # How it works
//
// The Retrier loops up to Policy.MaxAttempts times. Between attempts it
// sleeps for a delay computed as
//
//	delay = min(InitialDelay * Multiplier^(attempt-1), MaxDelay)
//
// with multiplicative jitter — delay is perturbed by up to ±JitterFactor
// to break synchronized client retries against a recovering server. The
// inter-attempt sleep is cancellation-aware: a ctx.Done() during the sleep
// aborts retries immediately and returns ctx.Err(). The Operation receives
// a 1-indexed attempt counter so callers can correlate logs/metrics.
//
// IsRetryable is the error-algebra extension point: callers classify which
// errors are worth retrying (network blip yes, validation no). The default
// retries any non-nil error.
//
// # Composable contract
//
// Retrier is the published extension point. Pro/downstream packages can
// provide alternative implementations — deadline-aware schedulers, jitter
// from a distributed clock, retry budgets shared across goroutines — and
// every caller that depends on the interface receives the upgrade
// automatically.
//
// The Do(ctx, op) shape composes with circuitbreaker.Breaker.Do and
// bulkhead.Bulkhead.Do — wrap a call in any combination of the three.
//
// # External dependencies
//
// None. Implemented from the Go standard library.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package retry
