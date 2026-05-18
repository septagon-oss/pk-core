// Package resilience groups PlatformKit's fault-tolerance primitives.
//
// # Why this package exists
//
// Network-bound, IO-bound, and otherwise-fallible operations need a small,
// audited set of guard rails: retry transient failures, short-circuit a
// repeatedly-failing dependency, and cap the concurrency aimed at any one
// downstream. Building these by hand at every call site fragments behavior
// (different backoff curves, half-baked breakers, missing semaphores) and
// erodes operability. The resilience package centralizes the three
// primitives behind small interfaces so call sites compose them without
// reinventing the math.
//
// # Sub-packages
//
//   - retry: exponential-backoff Retrier with jitter, context cancellation,
//     and a pluggable IsRetryable predicate.
//   - circuitbreaker: closed/open/half-open state machine that short-circuits
//     calls when a downstream is unhealthy and probes for recovery.
//   - bulkhead: concurrency-limiting semaphore that caps in-flight work and
//     optionally queues callers up to a bounded depth.
//
// # Composition
//
// Every primitive is callable in the same shape — Do(ctx, op) where op is a
// func(ctx) error — and therefore chains:
//
//	err := retrier.Do(ctx, func(ctx context.Context, attempt int) error {
//	    return breaker.Do(ctx, func(ctx context.Context) error {
//	        return bulkhead.Do(ctx, callBackend)
//	    })
//	})
//
// Each primitive is independently swappable. Pro/downstream packages can
// provide alternative Retriers (deadline-aware schedulers, distributed
// coordinators), Breakers (Redis-backed shared state), and Bulkheads
// (dynamic-capacity, queue-aware) by implementing the published interface
// without touching pk-core.
//
// # External dependencies
//
// None. Every sub-package is implemented from the Go standard library so
// the resilience kernel remains audit-friendly and free of supply-chain
// surface beyond stdlib.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package resilience
