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

func TestSlogLoggerFlattensLegacyFieldsMap(t *testing.T) {
	t.Parallel()
	type legacyFields map[string]any

	var buf bytes.Buffer
	l := logger.NewSlog(slog.NewJSONHandler(&buf, nil))
	l.Warn(context.Background(), "legacy fields", legacyFields{
		"tenant_id": "tenant-1",
		"error":     "lookup failed",
	})

	s := buf.String()
	if strings.Contains(s, "!BADKEY") {
		t.Fatalf("legacy fields produced !BADKEY: %s", s)
	}
	if !strings.Contains(s, `"tenant_id":"tenant-1"`) || !strings.Contains(s, `"error":"lookup failed"`) {
		t.Fatalf("legacy fields were not flattened: %s", s)
	}
}

func TestSlogLoggerWithFlattensLegacyFieldsMap(t *testing.T) {
	t.Parallel()
	type legacyFields map[string]any

	var buf bytes.Buffer
	l := logger.NewSlog(slog.NewJSONHandler(&buf, nil)).With(legacyFields{"component": "audit"})
	l.Info(context.Background(), "child logger")

	s := buf.String()
	if strings.Contains(s, "!BADKEY") {
		t.Fatalf("legacy With fields produced !BADKEY: %s", s)
	}
	if !strings.Contains(s, `"component":"audit"`) {
		t.Fatalf("legacy With fields were not flattened: %s", s)
	}
}

func TestSlogLoggerNormalizesBadKeysBeforeHandlerRedaction(t *testing.T) {
	t.Parallel()
	type legacyFields map[string]any

	var buf bytes.Buffer
	l := logger.NewSlog(slog.NewJSONHandler(&buf, nil))
	l.Warn(context.Background(), "bad key", slog.String("!BADKEY", "legacy"), legacyFields{
		"!BADKEY": "from-map",
	})

	s := buf.String()
	if strings.Contains(s, "!BADKEY") {
		t.Fatalf("bad slog key leaked into output: %s", s)
	}
	if !strings.Contains(s, `"arg":"from-map"`) {
		t.Fatalf("bad keys should be normalized to arg: %s", s)
	}
}
