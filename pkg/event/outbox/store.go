// Package outbox — store.go owns the Store interface that backs the
// transactional outbox and the ErrDuplicate sentinel used by Save to
// communicate idempotent suppression. Concrete Store implementations
// (MemoryStore, SQLStore) live in sibling files; downstream adapters
// implement Store directly without touching this contract.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/septagon-oss/pk-core/pkg/event"
)

// ErrDuplicate is returned by Store.Save when an envelope with the same
// IdempotencyKey already exists. Callers that care about dedupe (e.g.,
// long-running ingestion pipelines) can branch on it; the Outbox itself
// treats ErrDuplicate as a non-error because the prior Save already
// scheduled delivery.
var ErrDuplicate = errors.New("event/outbox: duplicate idempotency key")

// PendingEntry is the unit a Store returns from ClaimBatch: an Envelope
// plus the Attempts counter the dispatcher uses to track retries.
//
// PendingEntry is a value type. Store implementations may build it from
// arbitrary backing storage (slice, SQL row, KV value); the Outbox only
// reads the public fields.
type PendingEntry struct {
	// Envelope is the original event payload, ready for dispatch.
	Envelope event.Envelope
	// Attempts is the number of prior delivery attempts (0 on first try).
	Attempts int
}

// Store persists outbox entries durably and coordinates the dispatcher's
// claim-deliver-mark workflow. Implementations MUST be safe for
// concurrent use; the Outbox calls Save from publisher goroutines and
// ClaimBatch / MarkDelivered / MarkFailed / MarkDead from dispatcher goroutines.
//
// Lifecycle contract:
//
//	Save(env)                  -> entry persisted, ready for dispatch
//	ClaimBatch(limit, ttl)     -> atomically reserve up to limit entries
//	MarkDelivered(env.ID)      -> entry removed (or marked terminal)
//	MarkFailed(env.ID, errStr) -> attempts++, scheduled for retry
//	MarkDead(env.ID, errStr)   -> terminally parked for operator redrive
//
// Idempotency: when env.IdempotencyKey != "" and a prior entry with the
// same key exists, Save MUST return ErrDuplicate. The Outbox treats
// ErrDuplicate as success (because the prior Save already scheduled
// delivery) so downstream code does not need to special-case it.
type Store interface {
	// Save persists env. Returns ErrDuplicate if env.IdempotencyKey is
	// non-empty and a prior entry already used it; any other non-nil
	// error indicates a real persistence failure that callers should
	// surface to their transaction.
	Save(ctx context.Context, env event.Envelope) error

	// ClaimBatch atomically reserves the entries it returns until ttl elapses.
	ClaimBatch(ctx context.Context, limit int, ttl time.Duration) ([]PendingEntry, error)

	// MarkDelivered records successful delivery of envelopeID.
	// Implementations MAY hard-delete the row or flip a status flag.
	MarkDelivered(ctx context.Context, envelopeID string) error

	// MarkFailed records that envelopeID failed delivery with errMsg.
	// Implementations MUST increment the attempts counter and leave the
	// entry available for the next ClaimBatch call.
	MarkFailed(ctx context.Context, envelopeID string, errMsg string) error

	// MarkDead removes envelopeID from the pending queue permanently while
	// preserving the row and terminal error for operator inspection/redrive.
	MarkDead(ctx context.Context, envelopeID string, errMsg string) error
}
