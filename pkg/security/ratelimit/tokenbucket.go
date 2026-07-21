// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

package ratelimit

import (
	"errors"
	"sync"
	"time"
)

// DefaultIdleTTL is the default duration a per-key bucket stays in memory
// after its last access. Operators rarely need to tune this; 10 minutes
// is short enough that a million transient IPs don't accumulate, long
// enough that a steady client isn't constantly re-initialized.
const DefaultIdleTTL = 10 * time.Minute

// ErrInvalidRate is returned by NewTokenBucket when Rate <= 0.
var ErrInvalidRate = errors.New("ratelimit: rate must be > 0")

// ErrInvalidBurst is returned by NewTokenBucket when Burst <= 0.
var ErrInvalidBurst = errors.New("ratelimit: burst must be > 0")

// TokenBucketConfig configures a TokenBucket. All fields except IdleTTL
// are required.
type TokenBucketConfig struct {
	// Rate is the steady-state tokens-per-second refill rate. Must be > 0.
	Rate float64

	// Burst is the maximum bucket capacity and the initial token count.
	// Must be > 0. A burst of 1 disables bursting (strict pacing).
	Burst int

	// IdleTTL controls how long an inactive bucket stays in memory.
	// Zero falls back to DefaultIdleTTL. A negative value disables
	// reaping entirely (useful for closed key sets where unbounded
	// growth is not a concern).
	IdleTTL time.Duration
}

// TokenBucket is an in-memory token-bucket Limiter. Each key gets its
// own bucket holding up to Burst tokens, refilling at Rate tokens per
// second. Safe for concurrent use; the implementation holds a single
// mutex over the key map and per-bucket state.
type TokenBucket struct {
	rate    float64
	burst   float64
	idleTTL time.Duration

	now func() time.Time // overridable for tests

	mu      sync.Mutex
	buckets map[string]*bucket
}

// bucket is the per-key state. tokens is a float so refill granularity
// is exact across sub-tick request spacing. lastSeen drives reaping.
type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// NewTokenBucket constructs an in-memory TokenBucket Limiter. Returns
// ErrInvalidRate or ErrInvalidBurst on invalid configuration.
func NewTokenBucket(cfg TokenBucketConfig) (*TokenBucket, error) {
	if cfg.Rate <= 0 {
		return nil, ErrInvalidRate
	}
	if cfg.Burst <= 0 {
		return nil, ErrInvalidBurst
	}
	idleTTL := cfg.IdleTTL
	if idleTTL == 0 {
		idleTTL = DefaultIdleTTL
	}
	return &TokenBucket{
		rate:    cfg.Rate,
		burst:   float64(cfg.Burst),
		idleTTL: idleTTL,
		now:     time.Now,
		buckets: make(map[string]*bucket),
	}, nil
}

// Allow satisfies Limiter. The math is:
//
//   - On entry, the bucket is refilled by (elapsed * rate) tokens, capped
//     at burst.
//   - If at least 1 token remains, decrement and return (true, 0).
//   - Otherwise, compute retryAfter as the time required to refill 1
//     token at the configured rate, and return (false, retryAfter).
func (b *TokenBucket) Allow(key string) (bool, time.Duration) {
	now := b.now()

	b.mu.Lock()
	defer b.mu.Unlock()

	b.maybeReapLocked(now)

	bk, ok := b.buckets[key]
	if !ok {
		bk = &bucket{tokens: b.burst, lastSeen: now}
		b.buckets[key] = bk
	} else {
		elapsed := now.Sub(bk.lastSeen).Seconds()
		if elapsed > 0 {
			bk.tokens += elapsed * b.rate
			if bk.tokens > b.burst {
				bk.tokens = b.burst
			}
		}
		bk.lastSeen = now
	}

	if bk.tokens >= 1 {
		bk.tokens--
		return true, 0
	}

	// Need (1 - bk.tokens) more tokens at rate tokens/sec.
	needed := 1 - bk.tokens
	retryAfter := max(time.Duration(needed/b.rate*float64(time.Second)), 0)
	return false, retryAfter
}

// maybeReapLocked removes buckets that have been idle past IdleTTL.
// Called from Allow under b.mu. A negative idleTTL disables reaping.
//
// The reap is opportunistic — there is no background goroutine, so
// memory pressure from idle buckets persists until the next request
// triggers a reap. This trades worst-case memory for zero goroutines,
// which is the right call for a primitive that may be instantiated
// many times in tests and short-lived processes.
func (b *TokenBucket) maybeReapLocked(now time.Time) {
	if b.idleTTL < 0 {
		return
	}
	cutoff := now.Add(-b.idleTTL)
	for k, bk := range b.buckets {
		if bk.lastSeen.Before(cutoff) {
			delete(b.buckets, k)
		}
	}
}
