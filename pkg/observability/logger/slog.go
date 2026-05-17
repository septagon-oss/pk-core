package logger

// slog.go wraps *slog.Logger to satisfy the OSS Logger contract using only the
// standard library.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"log/slog"
)

type slogLogger struct {
	inner *slog.Logger
}

// NewSlog returns a Logger backed by *slog.Logger.
// Passing a nil handler is invalid; callers must supply a handler explicitly
// to make the destination and level threshold a deliberate choice.
func NewSlog(handler slog.Handler) Logger {
	if handler == nil {
		panic("logger.NewSlog: handler must not be nil")
	}
	return &slogLogger{inner: slog.New(handler)}
}

// NewSlogFromLogger adapts an already-constructed *slog.Logger.
func NewSlogFromLogger(l *slog.Logger) Logger {
	if l == nil {
		panic("logger.NewSlogFromLogger: l must not be nil")
	}
	return &slogLogger{inner: l}
}

func (s *slogLogger) Debug(ctx context.Context, msg string, args ...any) {
	s.inner.DebugContext(ctx, msg, args...)
}
func (s *slogLogger) Info(ctx context.Context, msg string, args ...any) {
	s.inner.InfoContext(ctx, msg, args...)
}
func (s *slogLogger) Warn(ctx context.Context, msg string, args ...any) {
	s.inner.WarnContext(ctx, msg, args...)
}
func (s *slogLogger) Error(ctx context.Context, msg string, args ...any) {
	s.inner.ErrorContext(ctx, msg, args...)
}
func (s *slogLogger) With(args ...any) Logger {
	return &slogLogger{inner: s.inner.With(args...)}
}
func (s *slogLogger) Enabled(ctx context.Context, level slog.Level) bool {
	return s.inner.Enabled(ctx, level)
}
