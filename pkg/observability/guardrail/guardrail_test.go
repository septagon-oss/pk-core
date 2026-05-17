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
