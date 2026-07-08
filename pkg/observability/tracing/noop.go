package tracing

// noop.go provides a zero-allocation Tracer for tests and disabled paths.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "context"

type (
	noopTracer struct{}
	noopSpan   struct{}
)

// Noop returns a Tracer whose spans record nothing.
func Noop() Tracer { return noopTracer{} }

func (noopTracer) Start(ctx context.Context, _ string, _ ...Attr) (context.Context, Span) {
	span := noopSpan{}
	return ContextWithSpan(ctx, span), span
}

func (noopSpan) SetAttr(string, any)          {}
func (noopSpan) SetStatus(StatusCode, string) {}
func (noopSpan) RecordError(error)            {}
func (noopSpan) End()                         {}
func (noopSpan) Context() SpanContext         { return SpanContext{} }
