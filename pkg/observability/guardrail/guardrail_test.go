package guardrail_test

// guardrail_test.go validates that every Warn call emits the standard
// metadata (guardrail=true, schema id, mode, code) plus the caller-supplied
// extra args, via a capturing Logger.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"log/slog"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/observability/guardrail"
	"github.com/septagon-oss/pk-core/pkg/observability/logger"
)

type captureLogger struct {
	gotMessage string
	gotArgs    []any
}

func (c *captureLogger) Debug(context.Context, string, ...any) {}
func (c *captureLogger) Info(context.Context, string, ...any)  {}
func (c *captureLogger) Error(context.Context, string, ...any) {}
func (c *captureLogger) With(...any) logger.Logger             { return c }
func (c *captureLogger) Enabled(context.Context, slog.Level) bool {
	return true
}
func (c *captureLogger) Warn(_ context.Context, message string, args ...any) {
	c.gotMessage = message
	c.gotArgs = args
}

func TestWarnFallbackAddsMetadata(t *testing.T) {
	t.Parallel()
	log := &captureLogger{}
	guardrail.WarnFallback(context.Background(), log,
		"auth.lookup_email_fallback",
		"falling back to email lookup",
		"tenant_id", "t1",
	)
	if log.gotMessage != "falling back to email lookup" {
		t.Fatalf("message = %q", log.gotMessage)
	}
	expected := []any{
		"guardrail", true,
		"guardrail_standard", guardrail.Standard,
		"guardrail_mode", string(guardrail.ModeFallback),
		"guardrail_code", "auth.lookup_email_fallback",
		"tenant_id", "t1",
	}
	if len(log.gotArgs) != len(expected) {
		t.Fatalf("len(args) = %d, want %d; args=%v", len(log.gotArgs), len(expected), log.gotArgs)
	}
	for i := range expected {
		if log.gotArgs[i] != expected[i] {
			t.Fatalf("args[%d] = %#v, want %#v", i, log.gotArgs[i], expected[i])
		}
	}
}

func TestWarnNilLoggerIsNoop(t *testing.T) {
	t.Parallel()
	// Must not panic.
	guardrail.Warn(context.Background(), nil, guardrail.ModeDegraded, "x", "msg")
}

func TestWarnTrimsAndOmitsEmptyCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		mode        guardrail.Mode
		code        string
		wantArgsLen int // 6 if code omitted, 8 if present
		wantMode    string
		wantCode    string // empty if omitted
	}{
		{"empty code omitted", guardrail.ModeFallback, "", 6, "fallback", ""},
		{"whitespace code omitted", guardrail.ModeDegraded, "   ", 6, "degraded", ""},
		{"whitespace-padded code trimmed", guardrail.ModeFallback, "  x.y  ", 8, "fallback", "x.y"},
		{"whitespace-padded mode trimmed", guardrail.Mode("  fallback  "), "x", 8, "fallback", "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			log := &captureLogger{}
			guardrail.Warn(context.Background(), log, tc.mode, tc.code, "msg")
			if len(log.gotArgs) != tc.wantArgsLen {
				t.Fatalf("len(args) = %d, want %d; args=%v", len(log.gotArgs), tc.wantArgsLen, log.gotArgs)
			}
			if log.gotArgs[5] != tc.wantMode {
				t.Fatalf("guardrail_mode = %v, want %q", log.gotArgs[5], tc.wantMode)
			}
			if tc.wantCode != "" {
				if log.gotArgs[6] != "guardrail_code" {
					t.Fatalf("expected guardrail_code key at index 6, got %v", log.gotArgs[6])
				}
				if log.gotArgs[7] != tc.wantCode {
					t.Fatalf("guardrail_code = %v, want %q", log.gotArgs[7], tc.wantCode)
				}
			}
		})
	}
}

func TestShorthandHelpersUseCorrectMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		call func(context.Context, logger.Logger)
		want string
	}{
		{"fallback", func(ctx context.Context, l logger.Logger) { guardrail.WarnFallback(ctx, l, "c", "m") }, "fallback"},
		{"degraded", func(ctx context.Context, l logger.Logger) { guardrail.WarnDegraded(ctx, l, "c", "m") }, "degraded"},
		{"unsupported", func(ctx context.Context, l logger.Logger) { guardrail.WarnUnsupported(ctx, l, "c", "m") }, "unsupported"},
		{"configuration_gap", func(ctx context.Context, l logger.Logger) { guardrail.WarnConfigurationGap(ctx, l, "c", "m") }, "configuration_gap"},
		{"soft_empty", func(ctx context.Context, l logger.Logger) { guardrail.WarnSoftEmpty(ctx, l, "c", "m") }, "soft_empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			log := &captureLogger{}
			tc.call(context.Background(), log)
			if log.gotArgs[5] != tc.want {
				t.Fatalf("mode = %v, want %q", log.gotArgs[5], tc.want)
			}
		})
	}
}
