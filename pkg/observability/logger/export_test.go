// Validates: REQ-009.
// Per: ADR-0029.
// Discipline: C-14.

package logger

// export_test.go exposes a test-only forwarding Logger that lives in this
// (logging-infrastructure) package, so its call frames carry the same package
// marker as real adapter layers. External tests use it to prove that source
// resolution skips arbitrary wrapper depth, not just this package's own frames.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"log/slog"
)

// NewForwardingLogger returns a Logger that adds one extra call frame in this
// package before delegating to inner — a stand-in for downstream bridges such
// as backend-kit's shared.Logger adapter. It declares that added frame via
// WithCallerSkip(1) so source resolution still points at the real caller.
// Stack these to simulate N layers.
func NewForwardingLogger(inner Logger) Logger {
	return &forwardingLogger{inner: inner.WithCallerSkip(1)}
}

type forwardingLogger struct{ inner Logger }

func (f *forwardingLogger) Debug(ctx context.Context, msg string, args ...any) {
	f.inner.Debug(ctx, msg, args...)
}

func (f *forwardingLogger) Info(ctx context.Context, msg string, args ...any) {
	f.inner.Info(ctx, msg, args...)
}

func (f *forwardingLogger) Warn(ctx context.Context, msg string, args ...any) {
	f.inner.Warn(ctx, msg, args...)
}

func (f *forwardingLogger) Error(ctx context.Context, msg string, args ...any) {
	f.inner.Error(ctx, msg, args...)
}

func (f *forwardingLogger) With(args ...any) Logger {
	return &forwardingLogger{inner: f.inner.With(args...)}
}

func (f *forwardingLogger) WithCallerSkip(skip int) Logger {
	return &forwardingLogger{inner: f.inner.WithCallerSkip(skip)}
}

func (f *forwardingLogger) Enabled(ctx context.Context, level slog.Level) bool {
	return f.inner.Enabled(ctx, level)
}
