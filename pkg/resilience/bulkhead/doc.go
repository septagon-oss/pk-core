// Package bulkhead implements a concurrency-limiting semaphore that caps
// in-flight work to a fixed number of slots and optionally queues callers
// up to a bounded depth before failing fast with ErrFull.
//
// # Why this package exists
//
// Resources in the caller — goroutines, memory, sockets, connection pool
// entries — are bounded even when the downstream is healthy. A surge of
// uncapped concurrent calls aimed at one downstream can starve the rest
// of the process: every other unrelated piece of work runs slower, or
// fails, because the surge consumed shared capacity. The bulkhead pattern
// (named for ship compartments) confines that surge to its own
// compartment: only N calls run concurrently against a downstream, while
// the rest either wait briefly or fail fast — never starve neighbors.
//
// # How it works
//
// A Bulkhead has two knobs:
//
//   - MaxConcurrent: how many ops may execute in parallel.
//   - MaxQueue: how many additional callers may wait for a slot before
//     the bulkhead returns ErrFull immediately.
//
// Internally a buffered channel of capacity MaxConcurrent represents the
// slots. Do tries to acquire a slot. If one is free it runs op
// immediately. If none is free and the wait queue is full (len(waiters)
// >= MaxQueue), Do returns ErrFull without invoking op. Otherwise Do
// joins the wait queue and blocks until either a slot becomes available,
// AcquireTimeout elapses, or the caller's context is cancelled.
//
// # Composable contract
//
// Bulkhead is the published interface. Pro/downstream packages can
// provide alternative implementations — dynamic-capacity bulkheads that
// adjust to back-pressure signals, queue-aware bulkheads with priority,
// or cross-process bulkheads coordinated through a control plane — by
// implementing the same interface.
//
// The Do(ctx, op) shape composes with retry.Retrier.Do and
// circuitbreaker.Breaker.Do.
//
// # External dependencies
//
// None. Implemented from the Go standard library.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package bulkhead
