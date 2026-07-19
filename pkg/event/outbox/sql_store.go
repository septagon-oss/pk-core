// Package outbox — sql_store.go owns the database/sql-backed Store
// implementation. The caller supplies a *sql.DB (and therefore the
// driver); pk-core stays driver-agnostic. The SchemaSQL constant exports
// the CREATE TABLE statement so application bootstrap can run it once
// before first use.
//
// # Test coverage
//
// pk-core ships SQLStore as production code without integration tests in
// this repo to avoid pulling a SQL driver into the OSS test deps. The
// query strings are exercised end-to-end downstream (starter-saas wires
// SQLStore with modernc.org/sqlite); the contract is covered here via
// MemoryStore, which is the Store reference implementation.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/septagon-oss/pk-core/pkg/event"
)

// SchemaSQL is the CREATE TABLE statement for the outbox. Callers run
// this once at bootstrap; it is idempotent thanks to IF NOT EXISTS.
//
// The schema is intentionally minimal: one envelope per row, one
// idempotency-key uniqueness index, and an attempts/last_error pair for
// retry visibility. Downstream operators are free to add their own
// indexes (e.g., on delivered_at) without breaking the queries.
// delivered_at semantics: 0 = pending, > 0 = delivered (Unix nanos),
// DeadDeliveredAt (-1) = dead-lettered (ADR-0049 D6). claimed_until
// (Unix nanos, 0 = unclaimed) backs ClaimBatch's lease; existing
// rows require it.
const SchemaSQL = `
CREATE TABLE IF NOT EXISTS pk_event_outbox (
    id              TEXT PRIMARY KEY,
    type            TEXT NOT NULL,
    source          TEXT NOT NULL,
    subject         TEXT NOT NULL DEFAULT '',
    tenant_id       TEXT NOT NULL DEFAULT '',
    correlation_id  TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL DEFAULT '',
    media_type      TEXT NOT NULL DEFAULT '',
    data            BLOB,
    emitted_at      INTEGER NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT NOT NULL DEFAULT '',
    delivered_at    INTEGER NOT NULL DEFAULT 0,
    claimed_until   INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS pk_event_outbox_idem
    ON pk_event_outbox(idempotency_key)
    WHERE idempotency_key <> '';
CREATE INDEX IF NOT EXISTS pk_event_outbox_pending
    ON pk_event_outbox(delivered_at, emitted_at);
`

// DeadDeliveredAt is the delivered_at sentinel for dead-lettered rows:
// excluded from every batch like delivered rows, but distinguishable
// for inspection and replay (`WHERE delivered_at = -1`).
const DeadDeliveredAt = -1

// sqlStore implements Store on top of *sql.DB.
type sqlStore struct {
	db *sql.DB
}

// NewSQLStore returns a Store backed by db. The caller must run SchemaSQL
// before first use (typically in their migration pipeline).
func NewSQLStore(db *sql.DB) (Store, error) {
	if db == nil {
		return nil, errors.New("event/outbox: NewSQLStore requires non-nil *sql.DB")
	}
	return &sqlStore{db: db}, nil
}

const sqlInsert = `
INSERT INTO pk_event_outbox
    (id, type, source, subject, tenant_id, correlation_id, idempotency_key,
     media_type, data, emitted_at, attempts, last_error, delivered_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '', 0);
`

const sqlSelectByKey = `
SELECT id FROM pk_event_outbox WHERE idempotency_key = ? LIMIT 1;
`

const sqlMarkDelivered = `
UPDATE pk_event_outbox SET delivered_at = ? WHERE id = ?;
`

// sqlMarkFailed also releases any live claim (claimed_until = 0): the
// failed attempt is over and the entry must be available for the next
// pass per the Store contract — holding the claim would stretch every
// retry by the claim TTL.
const sqlMarkFailed = `
UPDATE pk_event_outbox
SET attempts = attempts + 1, last_error = ?, claimed_until = 0
WHERE id = ?;
`

const sqlClaimCandidates = `
SELECT id, type, source, subject, tenant_id, correlation_id, idempotency_key,
       media_type, data, emitted_at, attempts
FROM pk_event_outbox
WHERE delivered_at = 0 AND claimed_until <= ?
ORDER BY emitted_at ASC, id ASC
LIMIT ?;
`

// sqlClaimOne is the atomic per-row claim: only one concurrent
// dispatcher's UPDATE matches the unclaimed predicate, so RowsAffected
// arbitrates the race without FOR UPDATE SKIP LOCKED — keeping the
// store driver-agnostic (SQLite has no row locks; Postgres serializes
// the row write either way).
const sqlClaimOne = `
UPDATE pk_event_outbox
SET claimed_until = ?
WHERE id = ? AND delivered_at = 0 AND claimed_until <= ?;
`

const sqlMarkDead = `
UPDATE pk_event_outbox
SET delivered_at = ?, last_error = ?
WHERE id = ?;
`

// Save persists env. Dedupe is enforced via the unique idempotency-key
// index AND an explicit SELECT to keep the error path driver-agnostic.
func (s *sqlStore) Save(ctx context.Context, env event.Envelope) error {
	if err := env.Validate(); err != nil {
		return err
	}
	if env.IdempotencyKey != "" {
		var existing string
		err := s.db.QueryRowContext(ctx, sqlSelectByKey, env.IdempotencyKey).Scan(&existing)
		switch {
		case err == nil:
			return ErrDuplicate
		case errors.Is(err, sql.ErrNoRows):
			// fall through to insert
		default:
			return fmt.Errorf("event/outbox: select dedupe: %w", err)
		}
	}
	_, err := s.db.ExecContext(
		ctx, sqlInsert,
		env.ID, env.Type, env.Source, env.Subject, env.TenantID, env.CorrelationID,
		env.IdempotencyKey, env.DataMediaType, env.Data, env.Time.UnixNano(),
	)
	if err != nil {
		// A concurrent insert may have lost the dedupe race; expose it as
		// ErrDuplicate when the IdempotencyKey is set.
		if env.IdempotencyKey != "" {
			var existing string
			if e2 := s.db.QueryRowContext(ctx, sqlSelectByKey, env.IdempotencyKey).Scan(&existing); e2 == nil && existing != "" {
				return ErrDuplicate
			}
		}
		return fmt.Errorf("event/outbox: insert: %w", err)
	}
	return nil
}

// MarkDelivered stamps the row with the current Unix-nano timestamp.
func (s *sqlStore) MarkDelivered(ctx context.Context, envelopeID string) error {
	_, err := s.db.ExecContext(ctx, sqlMarkDelivered, time.Now().UnixNano(), envelopeID)
	if err != nil {
		return fmt.Errorf("event/outbox: mark delivered: %w", err)
	}
	return nil
}

// MarkFailed increments attempts, records errMsg, and releases any live claim.
func (s *sqlStore) MarkFailed(ctx context.Context, envelopeID, errMsg string) error {
	_, err := s.db.ExecContext(ctx, sqlMarkFailed, errMsg, envelopeID)
	if err != nil {
		return fmt.Errorf("event/outbox: mark failed: %w", err)
	}
	return nil
}

// ClaimBatch selects unclaimed
// pending candidates, then claim each with an atomic compare-and-set
// UPDATE; rows lost to a concurrent dispatcher are silently skipped.
// Requires the claimed_until column from SchemaSQL.
func (s *sqlStore) ClaimBatch(ctx context.Context, limit int, ttl time.Duration) ([]PendingEntry, error) {
	if limit <= 0 {
		return nil, nil
	}
	now := time.Now()
	rows, err := s.db.QueryContext(ctx, sqlClaimCandidates, now.UnixNano(), limit)
	if err != nil {
		return nil, fmt.Errorf("event/outbox: claim candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]PendingEntry, 0, limit)
	for rows.Next() {
		var (
			env       event.Envelope
			emittedAt int64
			attempts  int
		)
		if err := rows.Scan(
			&env.ID, &env.Type, &env.Source, &env.Subject, &env.TenantID,
			&env.CorrelationID, &env.IdempotencyKey, &env.DataMediaType,
			&env.Data, &emittedAt, &attempts,
		); err != nil {
			return nil, fmt.Errorf("event/outbox: claim scan: %w", err)
		}
		env.Time = time.Unix(0, emittedAt)
		candidates = append(candidates, PendingEntry{Envelope: env, Attempts: attempts})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("event/outbox: claim rows: %w", err)
	}

	claimedUntil := now.Add(ttl).UnixNano()
	out := make([]PendingEntry, 0, len(candidates))
	for _, candidate := range candidates {
		res, err := s.db.ExecContext(ctx, sqlClaimOne, claimedUntil, candidate.Envelope.ID, now.UnixNano())
		if err != nil {
			return nil, fmt.Errorf("event/outbox: claim row: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("event/outbox: claim rows affected: %w", err)
		}
		if affected == 1 {
			out = append(out, candidate)
		}
	}
	return out, nil
}

// MarkDead parks the row outside the
// pending queue permanently (delivered_at = DeadDeliveredAt) with its
// final error preserved for inspection and replay.
func (s *sqlStore) MarkDead(ctx context.Context, envelopeID, errMsg string) error {
	_, err := s.db.ExecContext(ctx, sqlMarkDead, DeadDeliveredAt, errMsg, envelopeID)
	if err != nil {
		return fmt.Errorf("event/outbox: mark dead: %w", err)
	}
	return nil
}
