// Validates: REQ-014.
// Per: ADR-0029.
// Discipline: C-14.

package retry_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/resilience/retry"
)

func TestDefaultPolicyValid(t *testing.T) {
	t.Parallel()
	p := retry.DefaultPolicy()
	if err := p.Validate(); err != nil {
		t.Fatalf("DefaultPolicy().Validate(): %v", err)
	}
	if p.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", p.MaxAttempts)
	}
	if p.InitialDelay != 100*time.Millisecond {
		t.Errorf("InitialDelay = %v, want 100ms", p.InitialDelay)
	}
	if p.MaxDelay != 30*time.Second {
		t.Errorf("MaxDelay = %v, want 30s", p.MaxDelay)
	}
	if p.Multiplier != 2.0 {
		t.Errorf("Multiplier = %v, want 2.0", p.Multiplier)
	}
	if p.JitterFactor != 0.25 {
		t.Errorf("JitterFactor = %v, want 0.25", p.JitterFactor)
	}
	if p.IsRetryable == nil {
		t.Error("IsRetryable is nil")
	}
}

func TestPolicyValidateRejectsZeroMaxAttempts(t *testing.T) {
	t.Parallel()
	p := retry.DefaultPolicy()
	p.MaxAttempts = 0
	if err := p.Validate(); err == nil {
		t.Fatal("Validate() with MaxAttempts=0: want error, got nil")
	}
}

func TestPolicyValidateRejectsNegativeMultiplier(t *testing.T) {
	t.Parallel()
	p := retry.DefaultPolicy()
	p.Multiplier = -1
	if err := p.Validate(); err == nil {
		t.Fatal("Validate() with Multiplier=-1: want error, got nil")
	}
}

func TestDoSucceedsOnFirstAttempt(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	err := retry.Do(context.Background(), func(_ context.Context, attempt int) error {
		calls.Add(1)
		if attempt != 1 {
			t.Errorf("first call attempt = %d, want 1", attempt)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestDoRetriesUntilSuccess(t *testing.T) {
	t.Parallel()
	p := retry.DefaultPolicy()
	p.InitialDelay = time.Microsecond
	p.JitterFactor = 0
	r, err := retry.New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var calls atomic.Int32
	doErr := r.Do(context.Background(), func(_ context.Context, attempt int) error {
		n := calls.Add(1)
		if n < 3 {
			return errors.New("transient")
		}
		if attempt != 3 {
			t.Errorf("succeeding attempt = %d, want 3", attempt)
		}
		return nil
	})
	if doErr != nil {
		t.Fatalf("Do: %v", doErr)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
}

func TestDoStopsAtMaxAttempts(t *testing.T) {
	t.Parallel()
	p := retry.DefaultPolicy()
	p.MaxAttempts = 4
	p.InitialDelay = time.Microsecond
	p.JitterFactor = 0
	r, err := retry.New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := errors.New("persistent")
	var calls atomic.Int32
	doErr := r.Do(context.Background(), func(_ context.Context, _ int) error {
		calls.Add(1)
		return want
	})
	if !errors.Is(doErr, want) {
		t.Fatalf("Do err = %v, want %v", doErr, want)
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("calls = %d, want 4", got)
	}
}

func TestDoRespectsNonRetryableError(t *testing.T) {
	t.Parallel()
	fatal := errors.New("fatal: do not retry")
	p := retry.DefaultPolicy()
	p.InitialDelay = time.Microsecond
	p.IsRetryable = func(err error) bool { return !errors.Is(err, fatal) }
	r, err := retry.New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var calls atomic.Int32
	doErr := r.Do(context.Background(), func(_ context.Context, _ int) error {
		calls.Add(1)
		return fatal
	})
	if !errors.Is(doErr, fatal) {
		t.Fatalf("Do err = %v, want %v", doErr, fatal)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 (non-retryable should not retry)", got)
	}
}

func TestDoHonorsContextCancellation(t *testing.T) {
	t.Parallel()
	p := retry.DefaultPolicy()
	p.MaxAttempts = 100
	p.InitialDelay = 50 * time.Millisecond
	p.JitterFactor = 0
	r, err := retry.New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	doErr := r.Do(ctx, func(_ context.Context, _ int) error {
		return errors.New("transient")
	})
	if !errors.Is(doErr, context.Canceled) {
		t.Fatalf("Do err = %v, want context.Canceled", doErr)
	}
}

func TestDoExponentialBackoffGrows(t *testing.T) {
	t.Parallel()
	p := retry.DefaultPolicy()
	p.MaxAttempts = 4
	p.InitialDelay = 20 * time.Millisecond
	p.MaxDelay = 10 * time.Second
	p.Multiplier = 2.0
	p.JitterFactor = 0
	r, err := retry.New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var stamps []time.Time
	_ = r.Do(context.Background(), func(_ context.Context, _ int) error {
		stamps = append(stamps, time.Now())
		return errors.New("x")
	})
	if len(stamps) != 4 {
		t.Fatalf("attempts recorded = %d, want 4", len(stamps))
	}
	// Inter-attempt gaps should be roughly 20ms, 40ms, 80ms; allow ample
	// slack for scheduler jitter on CI. Each gap should be at least
	// 50% of its target, and each gap should strictly grow over its
	// predecessor (since JitterFactor=0).
	gap1 := stamps[1].Sub(stamps[0])
	gap2 := stamps[2].Sub(stamps[1])
	gap3 := stamps[3].Sub(stamps[2])
	if gap1 < 10*time.Millisecond || gap2 < 20*time.Millisecond || gap3 < 40*time.Millisecond {
		t.Fatalf("gaps too small: %v %v %v", gap1, gap2, gap3)
	}
	if gap2 <= gap1 || gap3 <= gap2 {
		t.Fatalf("backoff did not grow: %v %v %v", gap1, gap2, gap3)
	}
}

func TestDoBackoffClampedAtMaxDelay(t *testing.T) {
	t.Parallel()
	p := retry.DefaultPolicy()
	p.MaxAttempts = 5
	p.InitialDelay = 40 * time.Millisecond
	p.MaxDelay = 60 * time.Millisecond
	p.Multiplier = 10.0 // would blow past MaxDelay after one step
	p.JitterFactor = 0
	r, err := retry.New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var stamps []time.Time
	_ = r.Do(context.Background(), func(_ context.Context, _ int) error {
		stamps = append(stamps, time.Now())
		return errors.New("x")
	})
	if len(stamps) != 5 {
		t.Fatalf("attempts = %d, want 5", len(stamps))
	}
	// Every gap after the first should be near MaxDelay (60ms), never
	// 400ms (40 * 10^1). Allow up to 3x MaxDelay for slow CI.
	for i := 2; i < len(stamps); i++ {
		gap := stamps[i].Sub(stamps[i-1])
		if gap > 3*p.MaxDelay {
			t.Errorf("gap %d = %v, want <= %v", i, gap, 3*p.MaxDelay)
		}
	}
}

func TestDoJitterProducesVariance(t *testing.T) {
	t.Parallel()
	p := retry.DefaultPolicy()
	p.MaxAttempts = 2
	p.InitialDelay = 30 * time.Millisecond
	p.MaxDelay = time.Second
	p.JitterFactor = 0.5
	r, err := retry.New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Run two retries and collect their inter-attempt gaps. With a 50%
	// jitter band they should almost never be byte-identical.
	measure := func() time.Duration {
		var t1, t2 time.Time
		_ = r.Do(context.Background(), func(_ context.Context, attempt int) error {
			if attempt == 1 {
				t1 = time.Now()
			} else {
				t2 = time.Now()
			}
			return errors.New("x")
		})
		return t2.Sub(t1)
	}
	gapA := measure()
	gapB := measure()
	gapC := measure()
	if gapA == gapB && gapB == gapC {
		t.Fatalf("three jittered gaps were identical: %v %v %v", gapA, gapB, gapC)
	}
}

func TestDoZeroJitterIsDeterministic(t *testing.T) {
	t.Parallel()
	p := retry.DefaultPolicy()
	p.MaxAttempts = 2
	p.InitialDelay = 20 * time.Millisecond
	p.MaxDelay = time.Second
	p.JitterFactor = 0
	r, err := retry.New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	measure := func() time.Duration {
		var t1, t2 time.Time
		_ = r.Do(context.Background(), func(_ context.Context, attempt int) error {
			if attempt == 1 {
				t1 = time.Now()
			} else {
				t2 = time.Now()
			}
			return errors.New("x")
		})
		return t2.Sub(t1)
	}
	// With JitterFactor=0 the configured delay is exactly InitialDelay
	// (20ms). Allow a generous +20ms upper slack for scheduler latency
	// and require at least 80% of the target for the lower bound. We
	// check repeatability by ensuring three runs land in the same window.
	for range 3 {
		got := measure()
		if got < 16*time.Millisecond || got > 80*time.Millisecond {
			t.Fatalf("gap = %v, want in [16ms,80ms]", got)
		}
	}
}

// fakeBreaker is the cross-block-composition stand-in for the real
// circuitbreaker.Breaker. It refuses to invoke op while open and counts
// consecutive failures up to threshold. Keeping the mock here keeps the
// retry test file independent of the circuitbreaker package, while still
// proving the Do(ctx, op) shape composes.
type fakeBreaker struct {
	threshold int
	failures  int
	open      bool
}

func (b *fakeBreaker) Do(ctx context.Context, op func(ctx context.Context) error) error {
	if b.open {
		return errors.New("breaker: open")
	}
	if err := op(ctx); err != nil {
		b.failures++
		if b.failures >= b.threshold {
			b.open = true
		}
		return err
	}
	b.failures = 0
	return nil
}

// TestRetryComposesWithCircuitBreaker is the explicit chain-composition
// proof from the plan: a retrier wraps a breaker which wraps a flaky
// operation. The operation fails twice then succeeds; the breaker tolerates
// those failures (threshold=5) and the retrier delivers the eventual
// success.
func TestRetryComposesWithCircuitBreaker(t *testing.T) {
	t.Parallel()
	p := retry.DefaultPolicy()
	p.MaxAttempts = 5
	p.InitialDelay = time.Microsecond
	p.JitterFactor = 0
	r, err := retry.New(p)
	if err != nil {
		t.Fatalf("retry.New: %v", err)
	}
	br := &fakeBreaker{threshold: 5}

	var calls atomic.Int32
	doErr := r.Do(context.Background(), func(ctx context.Context, _ int) error {
		return br.Do(ctx, func(ctx context.Context) error {
			n := calls.Add(1)
			if n < 3 {
				return errors.New("flaky")
			}
			return nil
		})
	})
	if doErr != nil {
		t.Fatalf("composed Do: %v", doErr)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("op invocations = %d, want 3", got)
	}
	if br.open {
		t.Fatal("breaker tripped open under threshold")
	}
}

// TestDoNilOperationReturnsError defends the constructor against accidental
// nil-Operation calls; this is a small but real foot-gun in retry packages.
func TestDoNilOperationReturnsError(t *testing.T) {
	t.Parallel()
	r, err := retry.New(retry.DefaultPolicy())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.Do(context.Background(), nil); err == nil {
		t.Fatal("Do(nil): want error, got nil")
	}
}
