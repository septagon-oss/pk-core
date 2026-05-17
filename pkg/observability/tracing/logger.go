// Package tracing — logger.go provides a logger.ContextExtractor that emits
// the active span's trace_id and span_id as structured log attrs.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package tracing

import "context"

// LoggerExtractor pulls trace_id and span_id from the active span on ctx, if any.
// Both keys are emitted only when the span reports non-empty IDs (i.e. a real
// tracer is wired). Use as: logger.NewSlog(handler, tracing.LoggerExtractor).
func LoggerExtractor(ctx context.Context) []any {
	span := SpanFromContext(ctx)
	sc := span.Context()
	args := make([]any, 0, 4)
	if sc.TraceID != "" {
		args = append(args, "trace_id", sc.TraceID)
	}
	if sc.SpanID != "" {
		args = append(args, "span_id", sc.SpanID)
	}
	return args
}
