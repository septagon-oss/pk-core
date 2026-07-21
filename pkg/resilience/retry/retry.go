// Implements: REQ-014.
// Per: ADR-0029.
// Discipline: C-14.

// Package retry — retry.go owns the public surface: the Operation
// callable, the Policy config, DefaultPolicy/Validate, the Retrier
// interface, the New constructor, and the package-level Do helper.
// backoff.go owns the internal delay calculation that this file invokes.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package retry

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Operation is a single retryable call. The attempt parameter is 1-indexed
// so the first invocation reports attempt=1; callers use it to correlate
// logs and metrics or to vary behavior on retries.
type Operation func(ctx context.Context, attempt int) error

// Policy configures retry behavior. Every field has a documented default
// applied by DefaultPolicy and surfaced by Validate when a zero/negative
// value is presented.
type Policy struct {
	// MaxAttempts is the total number of invocations including the first.
	// Must be >= 1. Default: 3.
	MaxAttempts int

	// InitialDelay is the base delay before the first retry. Default: 100ms.
	InitialDelay time.Duration

	// MaxDelay caps any single inter-attempt delay. Default: 30s.
	MaxDelay time.Duration

	// Multiplier is the exponential growth factor applied between attempts.
	// Must be > 0. Default: 2.0.
	Multiplier float64

	// JitterFactor is the symmetric jitter band applied to each delay
	// (0.0 = no jitter, 1.0 = ± full delay). Default: 0.25.
	JitterFactor float64

	// IsRetryable decides whether a returned error is worth retrying.
	// Default: any non-nil error is retryable.
	IsRetryable func(err error) bool
}

// DefaultPolicy returns a sensible Policy: 3 attempts, 100ms base delay,
// 30s cap, 2x backoff, 25% jitter, retry on any non-nil error.
func DefaultPolicy() Policy {
	return Policy{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		JitterFactor: 0.25,
		IsRetryable:  defaultIsRetryable,
	}
}

func defaultIsRetryable(err error) bool { return err != nil }

// Validate normalizes p in place: negative durations are clamped to the
// DefaultPolicy values and a nil IsRetryable predicate is replaced with
// the default. Truly invalid fields (MaxAttempts < 1, Multiplier <= 0,
// JitterFactor < 0 or > 1) return a non-nil error so misconfiguration
// surfaces at construction time instead of mid-loop.
func (p *Policy) Validate() error {
	if p.MaxAttempts < 1 {
		return fmt.Errorf("retry: MaxAttempts must be >= 1, got %d", p.MaxAttempts)
	}
	if p.Multiplier <= 0 {
		return fmt.Errorf("retry: Multiplier must be > 0, got %v", p.Multiplier)
	}
	if p.JitterFactor < 0 || p.JitterFactor > 1 {
		return fmt.Errorf("retry: JitterFactor must be in [0,1], got %v", p.JitterFactor)
	}
	if p.InitialDelay < 0 {
		p.InitialDelay = 100 * time.Millisecond
	}
	if p.MaxDelay < 0 {
		p.MaxDelay = 30 * time.Second
	}
	if p.MaxDelay > 0 && p.InitialDelay > p.MaxDelay {
		// Cap initial at max so the first delay does not exceed the bound.
		p.InitialDelay = p.MaxDelay
	}
	if p.IsRetryable == nil {
		p.IsRetryable = defaultIsRetryable
	}
	return nil
}

// Retrier executes operations under a Policy. Implementations are safe
// for concurrent use; one Retrier value services many goroutines.
type Retrier interface {
	Do(ctx context.Context, op Operation) error
}

// New returns a Retrier with the given Policy. The returned Retrier is
// safe for concurrent use. Validate is called once at construction; if it
// fails, New returns the error and a nil Retrier.
func New(p Policy) (Retrier, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &retrier{policy: p, backoff: newBackoff(p)}, nil
}

// Do runs op under DefaultPolicy(). Convenience for one-off retries where
// the caller does not need to retain a Retrier.
func Do(ctx context.Context, op Operation) error {
	r, err := New(DefaultPolicy())
	if err != nil {
		return err
	}
	return r.Do(ctx, op)
}

// retrier is the default Retrier implementation. It is intentionally tiny:
// the entire loop fits in one method so the cancellation, error-classification,
// and backoff edges are visible in one place.
type retrier struct {
	policy  Policy
	backoff *backoff
}

// Do executes op up to MaxAttempts times. It returns the operation's last
// error if every attempt fails, ctx.Err() if the context is cancelled, or
// nil on the first successful attempt.
func (r *retrier) Do(ctx context.Context, op Operation) error {
	if op == nil {
		return errors.New("retry: op must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	var lastErr error
	for attempt := 1; attempt <= r.policy.MaxAttempts; attempt++ {
		// Honor cancellation before invoking op for each attempt. This
		// guarantees that a cancelled context never starts a new attempt
		// even if the previous attempt completed quickly.
		if err := ctx.Err(); err != nil {
			return err
		}

		err := op(ctx, attempt)
		if err == nil {
			return nil
		}
		lastErr = err

		if !r.policy.IsRetryable(err) {
			return err
		}
		if attempt == r.policy.MaxAttempts {
			break
		}

		delay := r.backoff.delay(attempt)
		if delay <= 0 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}
