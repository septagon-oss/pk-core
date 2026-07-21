// Implements: REQ-014.
// Per: ADR-0029.
// Discipline: C-14.

// Package retry — backoff.go owns the inter-attempt delay calculation:
// exponential growth from InitialDelay by Multiplier, clamped at MaxDelay,
// then multiplicative jitter applied symmetrically. Isolating the math
// from retry.go keeps the loop control flow easy to audit and makes the
// jitter source replaceable in tests.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package retry

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// backoff computes inter-attempt delays under a Policy. It owns its own
// *rand.Rand seeded from the wall clock; access is mutex-guarded so
// concurrent Retrier.Do calls do not race on the rand state.
type backoff struct {
	policy Policy
	mu     sync.Mutex
	rng    *rand.Rand
}

// newBackoff returns a backoff bound to p. The rng is seeded from the
// nanosecond wall clock so independent processes do not synchronize.
func newBackoff(p Policy) *backoff {
	return &backoff{
		policy: p,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// delay returns the wait between attempt and attempt+1. attempt is
// 1-indexed (the first delay is between attempt=1 and attempt=2).
//
// Formula:
//
//	base   = InitialDelay * Multiplier^(attempt-1)
//	capped = min(base, MaxDelay)         // only when MaxDelay > 0
//	jitter = capped * JitterFactor * (2*rand()-1)
//	delay  = max(0, capped + jitter)
func (b *backoff) delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if b.policy.InitialDelay <= 0 {
		return 0
	}

	mult := math.Pow(b.policy.Multiplier, float64(attempt-1))
	base := float64(b.policy.InitialDelay) * mult
	if b.policy.MaxDelay > 0 && base > float64(b.policy.MaxDelay) {
		base = float64(b.policy.MaxDelay)
	}

	if b.policy.JitterFactor > 0 {
		b.mu.Lock()
		// rand.Float64() in [0,1) -> map to (-1, 1) for symmetric jitter.
		signed := 2*b.rng.Float64() - 1
		b.mu.Unlock()
		base += base * b.policy.JitterFactor * signed
	}

	if base < 0 {
		base = 0
	}
	return time.Duration(base)
}
