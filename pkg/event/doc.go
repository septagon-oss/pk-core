// Package event defines the provider-neutral event-bus contract for the
// PlatformKit OSS kernel: the Envelope value type that carries event
// metadata, the Handler callable that processes deliveries, the Subscription
// handle that owns a registration, and the Bus interface that publishers and
// subscribers depend on.
//
// # Why this package exists
//
// Modules need an asynchronous fan-out primitive to communicate without
// reaching across module boundaries via direct imports. A wire-level
// implementation (NATS, JetStream, Kafka, CloudEvents-HTTP) belongs in
// downstream adapters, but every module — and every test — needs the same
// small, defensive contract: a Bus that accepts Envelopes and routes them to
// Handlers. This package owns that contract; concrete buses live in
// pkg/event/memory (in-process) and pkg/event/outbox (durable transactional
// outbox). Pro providers plug in by implementing the same Bus interface.
//
// # Block scorecard
//
//   - Identity:        github.com/septagon-oss/pk-core/pkg/event
//   - Boundary:        Envelope is a value type with public fields; Bus,
//     Subscription, and Handler are interfaces / function
//     types so concrete implementations stay swappable.
//   - Contract:        Bus { Publish, Subscribe, Close }, Handler signature,
//     Subscription handle, ErrBusClosed / ErrInvalidEnvelope
//     sentinels.
//   - Composition:     Buses chain (memory wraps a function, outbox wraps a
//     memory bus, resilience wrappers can stack on either);
//     subscribers register independently and never reach
//     into each other.
//   - Replacement:     Any type implementing Bus is a drop-in.
//   - Extension:       Pro / downstream adapters register their bus
//     implementations; new envelope fields can be added
//     additively without breaking handlers because the
//     value type is read by name.
//   - Runtime binding: Application composition decides which Bus instance
//     wires into modules (commonly outbox(memory) for OSS,
//     outbox(nats) for hosted deployments).
//   - Evidence:        event_test.go covers Validate's required-field rules,
//     zero-time normalization, and default media type.
//
// # Chainable laws
//
//   - Identity:         a no-op Handler is a valid composition element.
//   - Type compat:     every Bus exposes the same Publish / Subscribe / Close
//     shape so wrappers (outbox, resilience) compose without
//     adapters.
//   - Context preserve: Handlers receive the ctx that the publisher passed,
//     including deadlines and cancellation signals.
//   - Error algebra:   Handlers return error; bus implementations decide
//     whether to retry, surface, or both. The outbox uses
//     Envelope.IdempotencyKey to deduplicate replays.
//   - Cancellation:    ctx.Done() aborts in-flight handlers cooperatively.
//
// # External dependencies
//
// None. The contract is pure stdlib so the kernel stays audit-friendly.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package event
