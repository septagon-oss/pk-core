// Package memory ships the in-process event.Bus implementation: a fan-out
// publisher that holds subscriptions in a map under sync.RWMutex and
// delivers envelopes either synchronously on the publisher's goroutine
// (default) or asynchronously through a bounded worker pool (WithAsync).
//
// # Why this package exists
//
// Every PlatformKit module needs an event bus from the moment it boots —
// tests in particular cannot wait for a network broker. The in-process bus
// is the smallest possible implementation that satisfies the
// pkg/event.Bus contract: no external dependencies, deterministic delivery
// in sync mode, bounded concurrency in async mode, and a Close that drains
// in-flight handlers before returning. Pro deployments swap this bus for
// NATS, JetStream, or Kafka adapters; OSS deployments wrap it with the
// outbox for durability.
//
// # Block scorecard
//
//   - Identity:        github.com/septagon-oss/pk-core/pkg/event/memory
//   - Boundary:        New(opts...) returns event.Bus — the only exported
//     constructor; the bus struct stays private.
//   - Contract:        event.Bus (Publish, Subscribe, Close) + functional
//     Options (WithAsync, WithErrorReporter).
//   - Composition:     Stacks with the outbox (Outbox(memory)) and with
//     resilience wrappers; sync mode preserves error
//     propagation back to the publisher.
//   - Replacement:     Any other event.Bus implementation drops in.
//   - Extension:       Options expose async dispatch and error reporting
//     without changing the public surface; future options
//     stay additive.
//   - Runtime binding: Application composition picks sync vs async per
//     deployment.
//   - Evidence:        memory_test.go covers sync delivery, async dispatch,
//     cancellation, close semantics, concurrent publish
//     (-race), and error propagation in both modes.
//
// # Chainable laws
//
//   - Identity:         a no-op subscriber preserves bus behavior.
//   - Type compat:     Publish(env) -> handlers see the same Envelope
//     (defensive copy); no schema rewriting.
//   - Context preserve: handlers run with the publisher's ctx in sync mode;
//     async mode uses the bus's lifecycle ctx so workers
//     can shut down even if the original publisher's ctx
//     has already returned.
//   - Error algebra:   sync mode returns the first non-nil handler error;
//     async mode forwards every error to the ErrorReporter.
//   - Cancellation:    Close stops accepting Publish, then drains in-flight
//     dispatch before returning.
//
// # External dependencies
//
// None. Pure stdlib (context, errors, sync).
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package memory
