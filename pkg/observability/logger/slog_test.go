// Validates: REQ-009.
// Per: ADR-0029.
// Discipline: C-14.

package logger_test

// slog_test.go validates the slog-backed Logger: records appear in the buffer,
// attrs propagate via With(), and the level threshold is enforced.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"runtime"
	"strings"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/observability/logger"
)

func TestSlogLoggerEmitsInfo(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.NewSlog(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	l.Info(context.Background(), "hello", "k", "v")

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("unmarshal: %v; buf=%q", err, buf.String())
	}
	if record["msg"] != "hello" {
		t.Fatalf("msg = %v", record["msg"])
	}
	if record["k"] != "v" {
		t.Fatalf("k = %v", record["k"])
	}
}

func TestSlogLoggerWithInheritsAttrs(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.NewSlog(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	child := l.With("module", "user")
	child.Info(context.Background(), "did the thing")

	if !strings.Contains(buf.String(), `"module":"user"`) {
		t.Fatalf("missing inherited attr: %s", buf.String())
	}
}

func TestSlogLoggerHonorsLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.NewSlog(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	l.Debug(context.Background(), "should not appear")
	if buf.Len() != 0 {
		t.Fatalf("expected zero output, got %q", buf.String())
	}
}

func TestSlogLoggerAppliesContextExtractors(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	extractor := func(_ context.Context) []any {
		return []any{"request_id", "req-123", "tenant_id", "t1"}
	}
	l := logger.NewSlog(slog.NewJSONHandler(&buf, nil), extractor)
	l.Info(context.Background(), "ping")
	s := buf.String()
	if !strings.Contains(s, `"request_id":"req-123"`) {
		t.Fatalf("missing request_id: %s", s)
	}
	if !strings.Contains(s, `"tenant_id":"t1"`) {
		t.Fatalf("missing tenant_id: %s", s)
	}
}

func recordSource(t *testing.T, buf *bytes.Buffer) (file, function string, line int) {
	t.Helper()
	var rec struct {
		Source struct {
			File     string `json:"file"`
			Line     int    `json:"line"`
			Function string `json:"function"`
		} `json:"source"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("unmarshal: %v; buf=%q", err, buf.String())
	}
	return rec.Source.File, rec.Source.Function, rec.Source.Line
}

func TestSlogLoggerRecordsCallerAsSource(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.NewSlog(slog.NewJSONHandler(&buf, &slog.HandlerOptions{AddSource: true, Level: slog.LevelDebug}))
	_, _, wantLine, _ := runtime.Caller(0)
	l.Warn(context.Background(), "boom") // must resolve to THIS line (wantLine+1)

	file, function, line := recordSource(t, &buf)
	if !strings.HasSuffix(file, "slog_test.go") || line != wantLine+1 {
		t.Fatalf("source = %s:%d, want this test at slog_test.go:%d (not the wrapper)", file, line, wantLine+1)
	}
	if !strings.HasSuffix(function, ".TestSlogLoggerRecordsCallerAsSource") {
		t.Fatalf("source function = %q, want the test function", function)
	}
}

// TestSlogLoggerSkipsWrapperLayers is the real regression for the flagship bug:
// the logger is reached through transparent adapter layers (backend-kit's
// shared.Logger bridge in production), each adding a call frame and declaring it
// via WithCallerSkip(1). Source must resolve past ALL of them to the true call
// site, proving the fix composes across wrapper depth.
func TestSlogLoggerSkipsWrapperLayers(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	base := logger.NewSlog(slog.NewJSONHandler(&buf, &slog.HandlerOptions{AddSource: true, Level: slog.LevelDebug}))
	// Two stacked forwarders, mirroring caller -> shared.Logger -> pk-core.
	wrapped := logger.NewForwardingLogger(logger.NewForwardingLogger(base))
	_, _, wantLine, _ := runtime.Caller(0)
	wrapped.Warn(context.Background(), "boom") // must STILL resolve here (wantLine+1)

	file, function, line := recordSource(t, &buf)
	if !strings.HasSuffix(file, "slog_test.go") || line != wantLine+1 {
		t.Fatalf("source = %s:%d, want slog_test.go:%d; wrapper layers not skipped", file, line, wantLine+1)
	}
	if !strings.HasSuffix(function, ".TestSlogLoggerSkipsWrapperLayers") {
		t.Fatalf("source function = %q, want the test function", function)
	}
}

// TestWithCallerSkipShiftsAttribution pins the primitive: each WithCallerSkip(1)
// moves attribution up exactly one frame. A logger skipped by one, called
// directly, attributes to this function's own caller rather than to this line.
func TestWithCallerSkipShiftsAttribution(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	base := logger.NewSlog(slog.NewJSONHandler(&buf, &slog.HandlerOptions{AddSource: true, Level: slog.LevelDebug}))

	// Helper that logs through a skip-1 logger; with the extra skip, the record
	// is attributed to emit's caller (this test), not to emit itself.
	emit := func(l logger.Logger) { l.WithCallerSkip(1).Warn(context.Background(), "x") }
	_, _, wantLine, _ := runtime.Caller(0)
	emit(base) // attribution target: wantLine+1

	file, function, line := recordSource(t, &buf)
	if !strings.HasSuffix(file, "slog_test.go") || line != wantLine+1 {
		t.Fatalf("source = %s:%d, want the emit() call site slog_test.go:%d", file, line, wantLine+1)
	}
	if !strings.HasSuffix(function, ".TestWithCallerSkipShiftsAttribution") {
		t.Fatalf("source function = %q, want the outer test function (skip applied)", function)
	}
}

func TestSlogLoggerExtractorAttrsPrecedeUserArgs(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	extractor := func(_ context.Context) []any { return []any{"injected", "yes"} }
	l := logger.NewSlog(slog.NewJSONHandler(&buf, nil), extractor)
	l.Info(context.Background(), "ping", "user_key", "user_val")
	s := buf.String()
	if !strings.Contains(s, `"injected":"yes"`) || !strings.Contains(s, `"user_key":"user_val"`) {
		t.Fatalf("both attrs should be present: %s", s)
	}
}
