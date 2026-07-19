// Validates: REQ-EVENT-001.
// Per: ADR-0029.
// Discipline: C-14.

package event_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/event"
)

func TestEnvelopeValidateRequired(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		env  event.Envelope
	}{
		{"missing ID", event.Envelope{Type: "user.created", Source: "user_management"}},
		{"missing Type", event.Envelope{ID: "evt-1", Source: "user_management"}},
		{"missing Source", event.Envelope{ID: "evt-1", Type: "user.created"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.env.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, event.ErrInvalidEnvelope) {
				t.Errorf("expected ErrInvalidEnvelope, got %v", err)
			}
		})
	}
}

func TestEnvelopeValidateNilReceiver(t *testing.T) {
	t.Parallel()
	var env *event.Envelope
	if err := env.Validate(); !errors.Is(err, event.ErrInvalidEnvelope) {
		t.Fatalf("nil receiver: got %v, want ErrInvalidEnvelope", err)
	}
}

func TestEnvelopeValidateNormalizesTimeZero(t *testing.T) {
	t.Parallel()
	env := event.Envelope{
		ID:     "evt-1",
		Type:   "user.created",
		Source: "user_management",
	}
	before := time.Now()
	if err := env.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	after := time.Now()
	if env.Time.IsZero() {
		t.Fatal("Time was not normalized; still zero")
	}
	if env.Time.Before(before) || env.Time.After(after) {
		t.Errorf("Time %v outside [%v,%v]", env.Time, before, after)
	}
}

func TestEnvelopeValidatePreservesNonZeroTime(t *testing.T) {
	t.Parallel()
	when := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	env := event.Envelope{
		ID:     "evt-1",
		Type:   "user.created",
		Source: "user_management",
		Time:   when,
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !env.Time.Equal(when) {
		t.Errorf("Time mutated: got %v, want %v", env.Time, when)
	}
}

func TestEnvelopeValidateSetsDefaultMediaType(t *testing.T) {
	t.Parallel()
	env := event.Envelope{
		ID:     "evt-1",
		Type:   "user.created",
		Source: "user_management",
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if env.DataMediaType != event.DefaultDataMediaType {
		t.Errorf("DataMediaType = %q, want %q", env.DataMediaType, event.DefaultDataMediaType)
	}
}

func TestEnvelopeValidatePreservesCustomMediaType(t *testing.T) {
	t.Parallel()
	env := event.Envelope{
		ID:            "evt-1",
		Type:          "user.created",
		Source:        "user_management",
		DataMediaType: "application/vnd.example+json",
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if env.DataMediaType != "application/vnd.example+json" {
		t.Errorf("DataMediaType overwritten: got %q", env.DataMediaType)
	}
}

func TestEnvelopeValidateIdempotent(t *testing.T) {
	t.Parallel()
	env := event.Envelope{
		ID:     "evt-1",
		Type:   "user.created",
		Source: "user_management",
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("first Validate: %v", err)
	}
	firstTime := env.Time
	firstMedia := env.DataMediaType
	if err := env.Validate(); err != nil {
		t.Fatalf("second Validate: %v", err)
	}
	if !env.Time.Equal(firstTime) {
		t.Errorf("Time mutated on second call: %v vs %v", env.Time, firstTime)
	}
	if env.DataMediaType != firstMedia {
		t.Errorf("DataMediaType mutated on second call: %q vs %q", env.DataMediaType, firstMedia)
	}
}

// TestHandlerSignatureCompiles is a compile-time assertion that the
// Handler type alias is callable in the shape callers depend on.
func TestHandlerSignatureCompiles(t *testing.T) {
	t.Parallel()
	var h event.Handler = func(ctx context.Context, env event.Envelope) error {
		return ctx.Err()
	}
	if err := h(context.Background(), event.Envelope{}); err != nil {
		t.Fatalf("handler returned %v", err)
	}
}

// TestSentinelErrorsAreDistinct guarantees ErrBusClosed and
// ErrInvalidEnvelope are not the same value (callers branch on them).
func TestSentinelErrorsAreDistinct(t *testing.T) {
	t.Parallel()
	if errors.Is(event.ErrBusClosed, event.ErrInvalidEnvelope) {
		t.Fatal("ErrBusClosed and ErrInvalidEnvelope must be distinct")
	}
	if errors.Is(event.ErrInvalidEnvelope, event.ErrBusClosed) {
		t.Fatal("ErrInvalidEnvelope and ErrBusClosed must be distinct")
	}
}
