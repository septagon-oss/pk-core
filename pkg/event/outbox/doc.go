// Implements: REQ-PORTS-012.
// Per: ADR-0007.
// Discipline: C-14.

// Package outbox ships the durable transactional outbox primitive for the
// PlatformKit event bus: Publish writes envelopes to a Store first, then a
// background dispatcher reads claimed entries and forwards them to an
// inner event.Bus, marking each entry delivered or failed.
//
// # Why this package exists
//
// In-process buses lose events on crash. Cross-service buses (NATS,
// Kafka) deliver at most once unless the producer commits the event in
// the same transaction as the business change. The transactional outbox
// pattern decouples "produce the event" from "deliver the event": the
// emitter writes to a Store (typically the same SQL transaction as the
// business mutation), and a separate dispatcher takes responsibility for
// retrying delivery until success. The outbox in this package implements
// that pattern with two Store backings — an in-memory Store for tests and
// a database/sql Store for production — both wired through the same
// Outbox value.
//
// # Block scorecard
//
//   - Identity:        github.com/septagon-oss/pk-core/pkg/event/outbox
//   - Boundary:        New(inner, store, opts...) returns *Outbox. The
//     Store interface and Option type are exported; the
//     Outbox struct fields stay private.
//   - Contract:        event.Bus (Publish, Subscribe, Close) +
//     Start/Stop lifecycle + Store interface (Save,
//     ClaimBatch, MarkDelivered, MarkFailed, MarkDead) +
//     ErrDuplicate sentinel for idempotent Save.
//   - Composition:     Outbox WRAPS an inner event.Bus, so Outbox(memory)
//     and Outbox(nats) compose without changing the
//     contract. Stores swap independently of buses.
//   - Replacement:     Any Bus implementation drops in as inner; any
//     Store implementation drops in as the durable store.
//   - Extension:       Pro Stores (Postgres, MySQL, JetStream KV) live
//     downstream and implement the same Store interface.
//   - Runtime binding: Application composition wires the Store
//     (Memory for tests, SQL for production) and starts
//     the dispatcher via Start(ctx).
//   - Evidence:        outbox_test.go covers Save -> dispatcher ->
//     MarkDelivered round trips, MarkFailed on handler
//     error, idempotent duplicate suppression, Stop
//     draining, and Publish-after-Stop rejection.
//
// # Chainable laws
//
//   - Identity:         a Store that always succeeds plus an inner bus
//     that delivers exactly emits exactly-once-effective
//     semantics for handlers that are idempotent.
//   - Type compat:     Outbox satisfies event.Bus, so callers compose it
//     transparently above or below other Bus wrappers.
//   - Context preserve: Publish honors the caller's ctx for the Save call;
//     the dispatcher uses its own lifecycle ctx so it
//     survives the Publisher's request scope.
//   - Error algebra:   Save errors return to the caller (so the caller
//     can fail the surrounding transaction). Dispatcher
//     errors mark the entry failed and retry on the next
//     pass.
//   - Cancellation:    Stop cancels the dispatcher's lifecycle ctx, waits
//     for the worker goroutine to exit, and rejects
//     further Publish calls with ErrBusClosed.
//
// # External dependencies
//
// None. The SQL Store uses stdlib database/sql; the caller supplies the
// driver. The in-memory Store has no dependencies at all.
//
// # SQL Store testing
//
// SQLStore is shipped as production code without integration tests in
// pk-core. Its query strings are exercised downstream (starter-saas runs
// them under modernc.org/sqlite); pk-core unit tests cover the contract
// via MemoryStore, which is also the Store reference implementation.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package outbox
