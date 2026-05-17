package tracing_test

// tracing_test.go validates the Tracer contract: Start returns a child
// context that carries the active span and a non-nil Span that can record
// attrs, set status, and End without panicking under Noop.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/observability/tracing"
)

func TestNoopStartReturnsUsableSpan(t *testing.T) {
	t.Parallel()
	tr := tracing.Noop()
	ctx, span := tr.Start(context.Background(), "op")
	if ctx == nil {
		t.Fatal("nil ctx")
	}
	if span == nil {
		t.Fatal("nil span")
	}
	span.SetAttr("k", "v")
	span.SetStatus(tracing.StatusError, "boom")
	span.RecordError(errors.New("x"))
	span.End()
}

func TestSpanFromContextDefaultsToNoop(t *testing.T) {
	t.Parallel()
	span := tracing.SpanFromContext(context.Background())
	if span == nil {
		t.Fatal("nil span")
	}
	span.End()
}

func TestContextWithSpanRoundTrip(t *testing.T) {
	t.Parallel()
	fake := &fakeSpan{}
	ctx := tracing.ContextWithSpan(context.Background(), fake)
	got := tracing.SpanFromContext(ctx)
	if got != fake {
		t.Fatalf("SpanFromContext returned %v, want the fake span stored via ContextWithSpan", got)
	}
}

func TestStartPropagatesSpanThroughContext(t *testing.T) {
	t.Parallel()
	tr := tracing.Noop()
	ctx, span := tr.Start(context.Background(), "op")
	got := tracing.SpanFromContext(ctx)
	if got != span {
		t.Fatalf("SpanFromContext after Start returned %v, want the span returned by Start (%v)", got, span)
	}
}

func TestNoopSpanMethodsDirectCall(t *testing.T) {
	t.Parallel()
	// Resolve a noop span via the public path so the *concrete* noopSpan type
	// gets its methods exercised under Go's coverage instrumentation.
	span := tracing.SpanFromContext(context.Background())
	span.SetAttr("k", "v")
	span.SetStatus(tracing.StatusOK, "ok")
	span.SetStatus(tracing.StatusError, "err")
	span.SetStatus(tracing.StatusUnset, "")
	span.RecordError(nil)
	span.End()
}

// fakeSpan is a minimal Span used to verify context propagation. End/SetAttr/SetStatus/RecordError
// are no-ops but distinct from the package's internal noopSpan, so identity comparisons in
// TestContextWithSpanRoundTrip prove the same value goes in and comes out.
type fakeSpan struct{}

func (*fakeSpan) SetAttr(string, any)                  {}
func (*fakeSpan) SetStatus(tracing.StatusCode, string) {}
func (*fakeSpan) RecordError(error)                    {}
func (*fakeSpan) End()                                 {}
