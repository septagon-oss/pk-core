// Implements: REQ-009.
// Per: ADR-0029.
// Discipline: C-14.

// Package tracing defines PlatformKit's provider-neutral tracing contract.
//
// tracing.go owns the Tracer and Span interfaces and the context-key plumbing
// used to thread the active span through call chains.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package tracing

import "context"

// Tracer creates spans.
type Tracer interface {
	// Start begins a new span. The returned context carries the span so it
	// can be retrieved by SpanFromContext deeper in the call chain. Callers
	// must call span.End() exactly once, typically via defer. Implementations
	// SHOULD apply the supplied attrs at span creation time so they are
	// visible to downstream samplers and exporters.
	Start(ctx context.Context, name string, attrs ...Attr) (context.Context, Span)
}

// Span is an in-flight operation record.
type Span interface {
	// SetAttr attaches a key/value attribute to the span.
	SetAttr(key string, value any)
	// SetStatus sets the span's final status code and a human-readable description.
	SetStatus(code StatusCode, description string)
	// RecordError records err as an event on the span.
	RecordError(err error)
	// End completes the span. It must be called exactly once.
	End()

	// Context returns the span's identity. Noop spans return zero SpanContext.
	Context() SpanContext
}

// SpanContext identifies a span in adapter-neutral form. TraceID and SpanID
// are W3C trace-context hex strings ("" if the span is a noop or untracked).
type SpanContext struct {
	TraceID string
	SpanID  string
}

// Attr is an initial span attribute provided at Start.
type Attr struct {
	Key   string
	Value any
}

// StatusCode describes a span's final status.
type StatusCode int

// Span status codes.
const (
	// StatusUnset is the default status before one is explicitly set.
	StatusUnset StatusCode = iota
	// StatusOK marks the span as completed successfully.
	StatusOK
	// StatusError marks the span as failed.
	StatusError
)

type spanKey struct{}

// ContextWithSpan returns ctx with span attached. Used by Tracer
// implementations; rarely needed by callers.
func ContextWithSpan(ctx context.Context, span Span) context.Context {
	return context.WithValue(ctx, spanKey{}, span)
}

// SpanFromContext returns the active Span from ctx, or a noop Span if none.
// Never returns nil.
func SpanFromContext(ctx context.Context) Span {
	if v, ok := ctx.Value(spanKey{}).(Span); ok && v != nil {
		return v
	}
	return noopSpan{}
}
