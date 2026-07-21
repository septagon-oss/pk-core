// Validates: REQ-009.
// Per: ADR-0029.
// Discipline: C-14.

package tracing_test

// logger_test.go validates that LoggerExtractor returns trace/span ids when a
// real span is active, and zero-length args when the active span is noop.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/observability/logger"
	"github.com/septagon-oss/pk-core/pkg/observability/tracing"
)

// Compile-time assertion: LoggerExtractor satisfies logger.ContextExtractor.
var _ logger.ContextExtractor = tracing.LoggerExtractor

func TestLoggerExtractorEmptyForNoopSpan(t *testing.T) {
	t.Parallel()
	args := tracing.LoggerExtractor(context.Background())
	if len(args) != 0 {
		t.Fatalf("noop span should yield no attrs, got %v", args)
	}
}

func TestLoggerExtractorEmitsIDsFromFakeSpan(t *testing.T) {
	t.Parallel()
	fake := &fakeSpanWithIDs{traceID: "abc", spanID: "def"}
	ctx := tracing.ContextWithSpan(context.Background(), fake)
	args := tracing.LoggerExtractor(ctx)
	want := []any{"trace_id", "abc", "span_id", "def"}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %v, want %v", i, args[i], want[i])
		}
	}
}

func TestLoggerExtractorOmitsEmptyIDs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpanWithIDs{traceID: "", spanID: "only-span"}
	ctx := tracing.ContextWithSpan(context.Background(), fake)
	args := tracing.LoggerExtractor(ctx)
	if len(args) != 2 || args[0] != "span_id" || args[1] != "only-span" {
		t.Fatalf("args = %v, want [span_id only-span]", args)
	}
}

// fakeSpanWithIDs is a minimal Span carrying trace/span IDs for context tests.
type fakeSpanWithIDs struct {
	traceID string
	spanID  string
}

func (s *fakeSpanWithIDs) SetAttr(string, any)                  {}
func (s *fakeSpanWithIDs) SetStatus(tracing.StatusCode, string) {}
func (s *fakeSpanWithIDs) RecordError(error)                    {}
func (s *fakeSpanWithIDs) End()                                 {}
func (s *fakeSpanWithIDs) Context() tracing.SpanContext {
	return tracing.SpanContext{TraceID: s.traceID, SpanID: s.spanID}
}
