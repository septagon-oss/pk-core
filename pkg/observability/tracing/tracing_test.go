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
