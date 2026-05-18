// Package circuitbreaker_test exercises the circuitbreaker package from
// the outside: it imports only the published API and drives the state
// machine through every transition using an injected Clock.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package circuitbreaker_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/resilience/circuitbreaker"
)

// fakeClock is a deterministic clock for state-machine timing tests. It
// is safe for concurrent use because every Now/Advance call is mutex
// guarded; the breaker calls Now from inside its own mutex but tests can
// Advance from outside it.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_700_000_000, 0)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestStateString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s    circuitbreaker.State
		want string
	}{
		{circuitbreaker.StateClosed, "closed"},
		{circuitbreaker.StateOpen, "open"},
		{circuitbreaker.StateHalfOpen, "half-open"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("State(%d).String() = %q, want %q", c.s, got, c.want)
		}
	}
	// Sanity: unknown state still produces a printable label.
	if got := circuitbreaker.State(99).String(); got == "" {
		t.Errorf("unknown state stringified to empty")
	}
}

func TestDefaultConfigIsValid(t *testing.T) {
	t.Parallel()
	br, err := circuitbreaker.New(circuitbreaker.DefaultConfig())
	if err != nil {
		t.Fatalf("New(Default): %v", err)
	}
	if br.State() != circuitbreaker.StateClosed {
		t.Fatalf("initial state = %v, want closed", br.State())
	}
}

func TestNewRejectsZeroFailureThreshold(t *testing.T) {
	t.Parallel()
	cfg := circuitbreaker.DefaultConfig()
	cfg.FailureThreshold = 0
	if _, err := circuitbreaker.New(cfg); err == nil {
		t.Fatal("New with FailureThreshold=0: want error, got nil")
	}

	cfg = circuitbreaker.DefaultConfig()
	cfg.OpenTimeout = 0
	if _, err := circuitbreaker.New(cfg); err == nil {
		t.Fatal("New with OpenTimeout=0: want error, got nil")
	}

	cfg = circuitbreaker.DefaultConfig()
	cfg.HalfOpenMaxProbes = 0
	if _, err := circuitbreaker.New(cfg); err == nil {
		t.Fatal("New with HalfOpenMaxProbes=0: want error, got nil")
	}
}

func TestClosedBreakerAllowsCalls(t *testing.T) {
	t.Parallel()
	br, err := circuitbreaker.New(circuitbreaker.DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var calls atomic.Int32
	for i := 0; i < 10; i++ {
		err := br.Do(context.Background(), func(_ context.Context) error {
			calls.Add(1)
			return nil
		})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := calls.Load(); got != 10 {
		t.Fatalf("calls = %d, want 10", got)
	}
	if br.State() != circuitbreaker.StateClosed {
		t.Fatalf("state = %v, want closed", br.State())
	}
}

func TestFailuresTripBreaker(t *testing.T) {
	t.Parallel()
	cfg := circuitbreaker.DefaultConfig()
	cfg.FailureThreshold = 3
	br, err := circuitbreaker.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fail := errors.New("boom")
	for i := 0; i < 3; i++ {
		err := br.Do(context.Background(), func(_ context.Context) error { return fail })
		if !errors.Is(err, fail) {
			t.Fatalf("call %d err = %v, want %v", i, err, fail)
		}
	}
	if br.State() != circuitbreaker.StateOpen {
		t.Fatalf("state = %v, want open", br.State())
	}
}

func TestOpenBreakerReturnsErrOpen(t *testing.T) {
	t.Parallel()
	cfg := circuitbreaker.DefaultConfig()
	cfg.FailureThreshold = 1
	br, err := circuitbreaker.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = br.Do(context.Background(), func(_ context.Context) error { return errors.New("x") })

	var calls atomic.Int32
	doErr := br.Do(context.Background(), func(_ context.Context) error {
		calls.Add(1)
		return nil
	})
	if !errors.Is(doErr, circuitbreaker.ErrOpen) {
		t.Fatalf("err = %v, want ErrOpen", doErr)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("op invoked %d times while open, want 0", got)
	}
}

func TestHalfOpenAfterTimeout(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	cfg := circuitbreaker.DefaultConfig()
	cfg.FailureThreshold = 1
	cfg.OpenTimeout = time.Second
	cfg.Clock = clk.Now
	br, err := circuitbreaker.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = br.Do(context.Background(), func(_ context.Context) error { return errors.New("trip") })
	if br.State() != circuitbreaker.StateOpen {
		t.Fatalf("state = %v, want open", br.State())
	}

	clk.Advance(2 * time.Second) // past OpenTimeout
	if br.State() != circuitbreaker.StateHalfOpen {
		t.Fatalf("state = %v, want half-open", br.State())
	}
}

func TestHalfOpenSuccessClosesBreaker(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	cfg := circuitbreaker.DefaultConfig()
	cfg.FailureThreshold = 1
	cfg.OpenTimeout = time.Second
	cfg.Clock = clk.Now
	br, err := circuitbreaker.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = br.Do(context.Background(), func(_ context.Context) error { return errors.New("trip") })
	clk.Advance(2 * time.Second)
	// First probe succeeds.
	err = br.Do(context.Background(), func(_ context.Context) error { return nil })
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if br.State() != circuitbreaker.StateClosed {
		t.Fatalf("state after good probe = %v, want closed", br.State())
	}
}

func TestHalfOpenFailureReopens(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	cfg := circuitbreaker.DefaultConfig()
	cfg.FailureThreshold = 1
	cfg.OpenTimeout = time.Second
	cfg.Clock = clk.Now
	br, err := circuitbreaker.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = br.Do(context.Background(), func(_ context.Context) error { return errors.New("trip") })
	clk.Advance(2 * time.Second) // -> half-open
	bad := errors.New("still bad")
	err = br.Do(context.Background(), func(_ context.Context) error { return bad })
	if !errors.Is(err, bad) {
		t.Fatalf("probe err = %v, want %v", err, bad)
	}
	if br.State() != circuitbreaker.StateOpen {
		t.Fatalf("state after bad probe = %v, want open", br.State())
	}
	// And subsequent calls within the new OpenTimeout window short-circuit.
	err = br.Do(context.Background(), func(_ context.Context) error { return nil })
	if !errors.Is(err, circuitbreaker.ErrOpen) {
		t.Fatalf("post-reopen call = %v, want ErrOpen", err)
	}
}

func TestSuccessResetsFailureCount(t *testing.T) {
	t.Parallel()
	cfg := circuitbreaker.DefaultConfig()
	cfg.FailureThreshold = 3
	br, err := circuitbreaker.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fail := errors.New("x")
	// 2 failures, then a success, then 2 more failures should NOT trip.
	_ = br.Do(context.Background(), func(_ context.Context) error { return fail })
	_ = br.Do(context.Background(), func(_ context.Context) error { return fail })
	_ = br.Do(context.Background(), func(_ context.Context) error { return nil })
	_ = br.Do(context.Background(), func(_ context.Context) error { return fail })
	_ = br.Do(context.Background(), func(_ context.Context) error { return fail })
	if br.State() != circuitbreaker.StateClosed {
		t.Fatalf("state = %v, want closed (success should have reset failure count)", br.State())
	}
}

func TestCustomIsFailurePredicate(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("count me")
	ignored := errors.New("ignore me")
	cfg := circuitbreaker.DefaultConfig()
	cfg.FailureThreshold = 2
	cfg.IsFailure = func(err error) bool { return errors.Is(err, sentinel) }
	br, err := circuitbreaker.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 10 ignored errors should NOT trip.
	for i := 0; i < 10; i++ {
		_ = br.Do(context.Background(), func(_ context.Context) error { return ignored })
	}
	if br.State() != circuitbreaker.StateClosed {
		t.Fatalf("state = %v, want closed (ignored err shouldn't count)", br.State())
	}
	// 2 counted errors trip.
	_ = br.Do(context.Background(), func(_ context.Context) error { return sentinel })
	_ = br.Do(context.Background(), func(_ context.Context) error { return sentinel })
	if br.State() != circuitbreaker.StateOpen {
		t.Fatalf("state = %v, want open", br.State())
	}
}

func TestBreakerConcurrentSafe(t *testing.T) {
	t.Parallel()
	cfg := circuitbreaker.DefaultConfig()
	cfg.FailureThreshold = 50
	br, err := circuitbreaker.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const goroutines = 32
	const callsPerG = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < callsPerG; i++ {
				_ = br.Do(context.Background(), func(_ context.Context) error {
					// Alternate success/failure to keep state churning
					// without permanently tripping.
					if (g+i)%5 == 0 {
						return errors.New("x")
					}
					return nil
				})
				_ = br.State()
			}
		}(g)
	}
	wg.Wait()
}

func TestContextCancellationShortCircuits(t *testing.T) {
	t.Parallel()
	br, err := circuitbreaker.New(circuitbreaker.DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = br.Do(ctx, func(_ context.Context) error {
		t.Fatal("op invoked despite cancelled ctx")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
