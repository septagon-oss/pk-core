// Package bulkhead — bulkhead.go owns the entire public surface: the
// ErrFull sentinel, the Config struct, the Bulkhead interface, the New
// constructor, and the default semaphore implementation that backs them.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package bulkhead

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// ErrFull is returned by Do when the bulkhead has no available slot AND
// the wait queue is already at MaxQueue depth.
var ErrFull = errors.New("bulkhead: full")

// Config configures a Bulkhead.
type Config struct {
	// MaxConcurrent is the maximum number of in-flight operations.
	// Must be > 0.
	MaxConcurrent int

	// MaxQueue is the maximum number of callers waiting for a slot. 0
	// means no queue: if every slot is busy, Do returns ErrFull
	// immediately without waiting. Negative values are rejected by New.
	MaxQueue int

	// AcquireTimeout is the maximum time a queued caller will wait for a
	// slot. 0 means wait indefinitely (until the caller's context is
	// cancelled). Negative values are rejected by New.
	AcquireTimeout time.Duration
}

// Bulkhead limits concurrent operations. Implementations are safe for
// concurrent use across many goroutines.
//
// Do attempts to acquire a slot. On success it invokes op with ctx and
// releases the slot when op returns. On failure (no slot available and
// queue full, AcquireTimeout exceeded, or ctx cancelled) Do returns
// without invoking op.
//
// InFlight returns the number of ops currently executing — primarily an
// observability hook for metrics and tests; it is not a synchronization
// primitive.
type Bulkhead interface {
	Do(ctx context.Context, op func(ctx context.Context) error) error
	InFlight() int
}

// New returns a Bulkhead with the given Config. Invalid configurations
// (MaxConcurrent <= 0, MaxQueue < 0, AcquireTimeout < 0) return a
// non-nil error and a nil Bulkhead.
func New(cfg Config) (Bulkhead, error) {
	if cfg.MaxConcurrent <= 0 {
		return nil, fmt.Errorf("bulkhead: MaxConcurrent must be > 0, got %d", cfg.MaxConcurrent)
	}
	if cfg.MaxQueue < 0 {
		return nil, fmt.Errorf("bulkhead: MaxQueue must be >= 0, got %d", cfg.MaxQueue)
	}
	if cfg.AcquireTimeout < 0 {
		return nil, fmt.Errorf("bulkhead: AcquireTimeout must be >= 0, got %v", cfg.AcquireTimeout)
	}
	return &bulkhead{
		cfg: cfg,
		sem: make(chan struct{}, cfg.MaxConcurrent),
	}, nil
}

// bulkhead is the default Bulkhead. Concurrency state lives in three
// fields:
//
//   - sem: a buffered channel of capacity MaxConcurrent. A send acquires
//     a slot; a receive releases one. len(sem) == in-flight count, which
//     is what InFlight reports.
//   - waiting: an atomic counter of callers currently queued for a slot.
//     Used to fail fast when waiting >= MaxQueue.
//
// Atomic counters are sufficient here because the only invariant that
// matters is "waiting + in-flight may transiently overshoot bounds by a
// constant factor due to interleaving, but the channel itself is the
// authoritative slot accounting." That trade-off is documented in the
// MaxQueue field comment.
type bulkhead struct {
	cfg     Config
	sem     chan struct{}
	waiting atomic.Int64
}

// Do tries to acquire a slot and run op under the bulkhead.
func (b *bulkhead) Do(ctx context.Context, op func(ctx context.Context) error) error {
	if op == nil {
		return errors.New("bulkhead: op must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Fast path: a slot is free; take it without queuing.
	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
		return op(ctx)
	default:
	}

	// Slow path: no slot. If we are already at the queue cap, fail fast.
	// We increment the waiting counter optimistically then check; if it
	// pushed us past MaxQueue we roll back.
	w := b.waiting.Add(1)
	if int(w) > b.cfg.MaxQueue {
		b.waiting.Add(-1)
		return ErrFull
	}
	defer b.waiting.Add(-1)

	// Wait for a slot, AcquireTimeout, or ctx cancellation.
	if b.cfg.AcquireTimeout > 0 {
		timer := time.NewTimer(b.cfg.AcquireTimeout)
		defer timer.Stop()
		select {
		case b.sem <- struct{}{}:
			defer func() { <-b.sem }()
			return op(ctx)
		case <-timer.C:
			return ErrFull
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
		return op(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// InFlight reports how many ops currently hold a slot.
func (b *bulkhead) InFlight() int { return len(b.sem) }
