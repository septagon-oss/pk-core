// Package outbox — memory_store.go owns the in-memory Store
// implementation used by tests and small single-process deployments.
// State lives in a slice (preserving insertion order for FIFO dispatch)
// plus a map keyed by IdempotencyKey for O(1) dedupe lookup; a single
// sync.Mutex guards both.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package outbox

import (
	"context"
	"sync"
	"time"

	"github.com/septagon-oss/pk-core/pkg/event"
)

// memoryEntry is the in-memory representation of a not-yet-delivered
// envelope. The dedupe map points at the same value so updates (attempts,
// lastError) are visible from both sides without copying.
type memoryEntry struct {
	envelope  event.Envelope
	attempts  int
	lastError string
	delivered bool
	// dead marks a terminally-parked entry (ADR-0049
	// D6): excluded from every batch, retained for inspection/replay.
	dead bool
	// claimedUntil excludes the entry from other dispatchers' batches
	// until the claim window elapses (ADR-0049 D6).
	claimedUntil time.Time
}

// memoryStore is the in-memory Store reference implementation.
type memoryStore struct {
	mu      sync.Mutex
	entries []*memoryEntry          // ordered, includes delivered until GC
	byKey   map[string]*memoryEntry // IdempotencyKey -> entry
	byID    map[string]*memoryEntry // env.ID -> entry
}

// NewMemoryStore returns an in-memory Store suitable for tests and
// single-process OSS deployments. The store keeps delivered entries in
// memory until explicit cleanup; this keeps
// MarkDelivered O(1) and lets tests inspect history.
func NewMemoryStore() Store {
	return &memoryStore{
		byKey: make(map[string]*memoryEntry),
		byID:  make(map[string]*memoryEntry),
	}
}

// Save appends env to the store. If env.IdempotencyKey is non-empty and a
// prior entry already used it, Save returns ErrDuplicate without changing
// state.
func (s *memoryStore) Save(ctx context.Context, env event.Envelope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := env.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if env.IdempotencyKey != "" {
		if _, exists := s.byKey[env.IdempotencyKey]; exists {
			return ErrDuplicate
		}
	}
	entry := &memoryEntry{envelope: env}
	s.entries = append(s.entries, entry)
	s.byID[env.ID] = entry
	if env.IdempotencyKey != "" {
		s.byKey[env.IdempotencyKey] = entry
	}
	return nil
}

// ClaimBatch atomically reserves the
// entries it returns for ttl, so concurrent dispatchers cannot receive
// the same entry within a claim window (ADR-0049 D6).
func (s *memoryStore) ClaimBatch(ctx context.Context, limit int, ttl time.Duration) ([]PendingEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	out := make([]PendingEntry, 0, limit)
	for _, e := range s.entries {
		if e.delivered || e.dead || e.claimedUntil.After(now) {
			continue
		}
		e.claimedUntil = now.Add(ttl)
		out = append(out, PendingEntry{
			Envelope: e.envelope,
			Attempts: e.attempts,
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// MarkDead parks the entry outside the pending
// queue permanently but stays in memory with its final error for
// inspection (ADR-0049 D6).
func (s *memoryStore) MarkDead(ctx context.Context, envelopeID, errMsg string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.byID[envelopeID]; ok {
		e.dead = true
		e.lastError = errMsg
	}
	return nil
}

// MarkDelivered flags envelopeID as delivered. Returns nil even for
// unknown IDs (the dispatcher may race a manual cleanup).
func (s *memoryStore) MarkDelivered(ctx context.Context, envelopeID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.byID[envelopeID]; ok {
		e.delivered = true
	}
	return nil
}

// MarkFailed increments the attempts counter and records the error string.
// The entry remains available for future ClaimBatch calls — which means
// it also RELEASES any live claim (ADR-0049 D6): the failed attempt is
// over, so holding the claim until its TTL would silently stretch every
// retry by the claim window.
func (s *memoryStore) MarkFailed(ctx context.Context, envelopeID, errMsg string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.byID[envelopeID]; ok {
		e.attempts++
		e.lastError = errMsg
		e.claimedUntil = time.Time{}
	}
	return nil
}
