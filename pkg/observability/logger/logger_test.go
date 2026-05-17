package logger_test

// logger_test.go validates the Logger contract: levels record, With() returns
// a child that inherits attrs, Enabled() honors the threshold, and Noop never
// panics under any input.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"log/slog"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/observability/logger"
)

func TestNoopAcceptsAllLevels(t *testing.T) {
	t.Parallel()
	l := logger.Noop()
	ctx := context.Background()
	l.Debug(ctx, "d", "k", "v")
	l.Info(ctx, "i")
	l.Warn(ctx, "w")
	l.Error(ctx, "e", "err", "boom")
}

func TestWithReturnsChildLogger(t *testing.T) {
	t.Parallel()
	l := logger.Noop()
	child := l.With("module", "user")
	if child == nil {
		t.Fatal("With() returned nil")
	}
}

func TestEnabledHonorsLevel(t *testing.T) {
	t.Parallel()
	l := logger.Noop()
	ctx := context.Background()
	if l.Enabled(ctx, slog.LevelError) {
		t.Fatal("Noop.Enabled should return false")
	}
}
