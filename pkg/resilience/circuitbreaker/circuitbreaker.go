// Package circuitbreaker — circuitbreaker.go owns the entire public
// surface for this primitive: the State enum, the ErrOpen sentinel, the
// Config struct, the Breaker interface, the New constructor, and the
// default implementation that backs them. The state machine is small
// enough that splitting it across files would obscure more than it
// reveals; auditing every transition in one file is the intent.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package circuitbreaker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// State identifies the breaker's current state.
type State int

const (
	// StateClosed lets requests flow through normally.
	StateClosed State = iota
	// StateOpen short-circuits requests with ErrOpen.
	StateOpen
	// StateHalfOpen allows a bounded number of probe requests through to
	// test whether the downstream has recovered.
	StateHalfOpen
)

// String returns a human-readable name for the state — useful in logs
// and test assertions.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// ErrOpen is returned by Do when the breaker is in StateOpen.
var ErrOpen = errors.New("circuitbreaker: open")

// Config configures a Breaker. Every field has a documented default
// applied by DefaultConfig; New validates inputs and rejects illegal
// combinations at construction time.
type Config struct {
	// FailureThreshold is the number of consecutive failures that trip the
	// breaker. Must be >= 1. Default: 5.
	FailureThreshold int

	// OpenTimeout is how long the breaker stays open before transitioning
	// to half-open. Must be > 0. Default: 30s.
	OpenTimeout time.Duration

	// HalfOpenMaxProbes is the number of probe requests admitted while
	// half-open. Must be >= 1. Default: 1.
	HalfOpenMaxProbes int

	// IsFailure decides which returned errors count as failures. Default:
	// any non-nil error.
	IsFailure func(err error) bool

	// Clock returns the current time. Default: time.Now. Tests inject a
	// deterministic clock to exercise timing-based transitions.
	Clock func() time.Time
}

// DefaultConfig returns a sensible Config: 5 failures trip the breaker,
// 30s open timeout, 1 probe in half-open, any non-nil error is a failure.
func DefaultConfig() Config {
	return Config{
		FailureThreshold:  5,
		OpenTimeout:       30 * time.Second,
		HalfOpenMaxProbes: 1,
		IsFailure:         defaultIsFailure,
		Clock:             time.Now,
	}
}

func defaultIsFailure(err error) bool { return err != nil }

// Breaker is the circuit breaker contract.
//
// Do invokes op iff the breaker permits. If the breaker is open, Do
// returns ErrOpen without invoking op. State returns the current state
// for observability; callers should not race-check state to decide
// whether to call Do — Do is itself the only authoritative gate.
type Breaker interface {
	Do(ctx context.Context, op func(ctx context.Context) error) error
	State() State
}

// New returns a Breaker with the given Config. Invalid configurations
// (FailureThreshold < 1, OpenTimeout <= 0, HalfOpenMaxProbes < 1) return
// a non-nil error and a nil Breaker.
func New(cfg Config) (Breaker, error) {
	if cfg.FailureThreshold < 1 {
		return nil, fmt.Errorf("circuitbreaker: FailureThreshold must be >= 1, got %d", cfg.FailureThreshold)
	}
	if cfg.OpenTimeout <= 0 {
		return nil, fmt.Errorf("circuitbreaker: OpenTimeout must be > 0, got %v", cfg.OpenTimeout)
	}
	if cfg.HalfOpenMaxProbes < 1 {
		return nil, fmt.Errorf("circuitbreaker: HalfOpenMaxProbes must be >= 1, got %d", cfg.HalfOpenMaxProbes)
	}
	if cfg.IsFailure == nil {
		cfg.IsFailure = defaultIsFailure
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &breaker{cfg: cfg, state: StateClosed}, nil
}

// breaker is the default Breaker. All mutable fields are protected by mu;
// State() acquires the mutex to read a coherent snapshot, and Do acquires
// it across the admission decision plus result accounting so concurrent
// callers cannot interleave through the transitions.
type breaker struct {
	cfg Config

	mu             sync.Mutex
	state          State
	failures       int
	openedAt       time.Time
	probesInFlight int
	probesAdmitted int
}

// State returns the breaker's current state, lazily transitioning Open ->
// HalfOpen if OpenTimeout has elapsed since the trip.
func (b *breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maybeOpenToHalfOpenLocked()
	return b.state
}

// Do enforces the state machine and forwards admitted calls to op.
//
// Threading note: the mutex is held across the admission decision (and
// across result accounting after op returns), but released while op runs
// so a slow downstream does not serialize unrelated callers.
func (b *breaker) Do(ctx context.Context, op func(ctx context.Context) error) error {
	if op == nil {
		return errors.New("circuitbreaker: op must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	b.mu.Lock()
	b.maybeOpenToHalfOpenLocked()
	switch b.state {
	case StateOpen:
		b.mu.Unlock()
		return ErrOpen
	case StateHalfOpen:
		if b.probesAdmitted >= b.cfg.HalfOpenMaxProbes {
			b.mu.Unlock()
			return ErrOpen
		}
		b.probesAdmitted++
		b.probesInFlight++
	}
	wasHalfOpen := b.state == StateHalfOpen
	b.mu.Unlock()

	err := op(ctx)

	b.mu.Lock()
	defer b.mu.Unlock()
	if wasHalfOpen {
		b.probesInFlight--
	}
	failed := b.cfg.IsFailure(err)
	switch {
	case !failed && wasHalfOpen:
		// First probe success closes the breaker.
		b.transitionToClosedLocked()
	case !failed:
		// Healthy call in Closed resets the consecutive-failure counter.
		b.failures = 0
	case failed && wasHalfOpen:
		// Probe failed; trip back to open with a fresh clock.
		b.transitionToOpenLocked()
	case failed:
		b.failures++
		if b.failures >= b.cfg.FailureThreshold {
			b.transitionToOpenLocked()
		}
	}
	return err
}

// maybeOpenToHalfOpenLocked lazily promotes Open -> HalfOpen when the
// OpenTimeout has elapsed. mu must be held.
func (b *breaker) maybeOpenToHalfOpenLocked() {
	if b.state != StateOpen {
		return
	}
	now := b.cfg.Clock()
	if !now.Before(b.openedAt.Add(b.cfg.OpenTimeout)) {
		b.state = StateHalfOpen
		b.probesAdmitted = 0
		b.probesInFlight = 0
	}
}

func (b *breaker) transitionToOpenLocked() {
	b.state = StateOpen
	b.openedAt = b.cfg.Clock()
	b.probesAdmitted = 0
	b.probesInFlight = 0
}

func (b *breaker) transitionToClosedLocked() {
	b.state = StateClosed
	b.failures = 0
	b.probesAdmitted = 0
	b.probesInFlight = 0
}
