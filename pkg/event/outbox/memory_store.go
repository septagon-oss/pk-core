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
// memory until they are GC'd by a subsequent NextBatch pass; this keeps
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

// NextBatch returns up to limit not-yet-delivered entries in insertion
// order.
func (s *memoryStore) NextBatch(ctx context.Context, limit int) ([]PendingEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]PendingEntry, 0, limit)
	for _, e := range s.entries {
		if e.delivered {
			continue
		}
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
// The entry remains available for future NextBatch calls.
func (s *memoryStore) MarkFailed(ctx context.Context, envelopeID, errMsg string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.byID[envelopeID]; ok {
		e.attempts++
		e.lastError = errMsg
	}
	return nil
}
