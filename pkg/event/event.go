// Implements: REQ-PORTS-012.
// Per: ADR-0029.
// Discipline: C-14.

// Package event — event.go owns the public surface of the event-bus
// contract: the Envelope value type, its Validate normalization, the
// Handler callable, the Subscription handle, the Bus interface, and the
// package-level sentinel errors (ErrBusClosed, ErrInvalidEnvelope).
//
// Concrete bus implementations live in sub-packages (event/memory,
// event/outbox); pro adapters (NATS, JetStream, Kafka, CloudEvents-HTTP)
// plug in by implementing the same Bus interface published here.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package event

import (
	"context"
	"errors"
	"time"
)

// DefaultDataMediaType is the media type Validate assigns when an Envelope
// arrives with an empty DataMediaType. JSON is the OSS default because
// every adapter speaks it natively and CloudEvents canonicalizes around it.
const DefaultDataMediaType = "application/json"

// Envelope is the CloudEvents-flavored container for one event delivery.
//
// All fields are public so callers can construct, mutate, and copy an
// envelope without reflection. The bus treats Envelope as a value: handlers
// receive a defensive copy from the bus implementation, never a pointer to
// shared mutable state. Handlers read fields by name and ignore fields they do
// not consume, so the envelope remains schema-extensible.
type Envelope struct {
	// ID is the unique event identifier (UUID recommended). Required.
	// The bus does not generate IDs; emitters own that decision.
	ID string

	// Type is the event type string, e.g. "user.created". Required.
	// Subscribers register by exact match on Type in this OSS contract;
	// pattern matching is adapter territory.
	Type string

	// Source identifies the emitter, e.g. "user_management". Required.
	Source string

	// Subject optionally identifies the entity the event is about, e.g.
	// "user:42". Free-form; downstream consumers may parse it.
	Subject string

	// Time is the emission timestamp. Validate defaults a zero Time to
	// time.Now() so callers can omit it for convenience.
	Time time.Time

	// TenantID optionally carries multi-tenant context.
	TenantID string

	// CorrelationID optionally chains an event into a tracing or saga flow.
	CorrelationID string

	// IdempotencyKey optionally identifies a logical event for dedupe.
	// The outbox uses this to suppress duplicate Saves; in-process buses
	// ignore it but pass it through to handlers verbatim.
	IdempotencyKey string

	// Data is the event payload, encoded as bytes. Callers control encoding
	// (JSON, protobuf, anything) and declare it via DataMediaType. The bus
	// does not parse Data.
	Data []byte

	// DataMediaType describes the content type of Data. Validate defaults
	// an empty value to DefaultDataMediaType ("application/json").
	DataMediaType string
}

// Validate normalizes env in place and reports whether the required fields
// are present.
//
// Normalization (mutates env):
//   - Time:          zero -> time.Now()
//   - DataMediaType: "" -> DefaultDataMediaType
//
// Required (returns ErrInvalidEnvelope if missing):
//   - ID
//   - Type
//   - Source
//
// Validate is idempotent: calling it twice on the same Envelope produces
// the same result as calling it once.
func (env *Envelope) Validate() error {
	if env == nil {
		return ErrInvalidEnvelope
	}
	if env.ID == "" {
		return errors.Join(ErrInvalidEnvelope, errors.New("event: Envelope.ID is required"))
	}
	if env.Type == "" {
		return errors.Join(ErrInvalidEnvelope, errors.New("event: Envelope.Type is required"))
	}
	if env.Source == "" {
		return errors.Join(ErrInvalidEnvelope, errors.New("event: Envelope.Source is required"))
	}
	if env.Time.IsZero() {
		env.Time = time.Now()
	}
	if env.DataMediaType == "" {
		env.DataMediaType = DefaultDataMediaType
	}
	return nil
}

// Handler processes a delivered event. Returning a non-nil error signals
// the bus that delivery failed; whether the bus retries depends on the
// implementation (the in-process bus surfaces the error to the publisher
// in sync mode; the outbox marks the envelope failed and lets the
// dispatcher retry on the next pass).
//
// Handlers MUST honor ctx cancellation. The bus passes the publisher's ctx
// through unchanged so deadlines, traces, and tenant context all propagate.
type Handler func(ctx context.Context, env Envelope) error

// Subscription is the handle returned by Bus.Subscribe. Calling Cancel
// removes the handler from the bus. Cancel is safe to call any number of
// times; subsequent calls are no-ops.
type Subscription interface {
	// Cancel removes the associated handler from the bus. After Cancel
	// returns, the bus guarantees no new invocations of the handler; an
	// in-flight invocation runs to completion.
	Cancel()
}

// Bus is the provider-neutral event-bus contract. Implementations MUST be
// safe for concurrent use.
//
// Publish delivers env to every subscriber registered for env.Type. The
// dispatch semantics (sync vs async, at-most-once vs at-least-once, retry
// behavior) are implementation choices; the contract here is that Publish
// returns nil if the bus accepted the envelope and a non-nil error if it
// could not.
//
// Subscribe registers handler for exact-match envelopes whose Type equals
// eventType. Pattern matching (wildcards, hierarchies) is intentionally
// out of scope for the OSS contract — adapters that need it expose their
// own pattern-aware Subscribe via implementation-specific methods.
//
// Close stops the bus and rejects future Publish calls with ErrBusClosed.
// Implementations SHOULD drain in-flight handlers respectfully before
// returning.
type Bus interface {
	Publish(ctx context.Context, env Envelope) error
	Subscribe(eventType string, handler Handler) (Subscription, error)
	Close() error
}

// Sentinel errors. Callers compare via errors.Is.

// ErrBusClosed is returned by Publish (and Subscribe) after Close.
var ErrBusClosed = errors.New("event: bus closed")

// ErrInvalidEnvelope is returned by Validate (and Publish) when an Envelope
// is missing required fields or is otherwise malformed.
var ErrInvalidEnvelope = errors.New("event: invalid envelope")
