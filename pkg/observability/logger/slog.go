// Implements: REQ-009.
// Per: ADR-0029.
// Discipline: C-14.

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
	inner      *slog.Logger
	extractors []ContextExtractor
}

// NewSlog returns a Logger backed by *slog.Logger.
// Passing a nil handler is invalid; callers must supply a handler explicitly
// to make the destination and level threshold a deliberate choice.
//
// Optional extractors run on every log call in declaration order; their output
// is prepended to user-supplied args so trace/request-scoped attrs land before
// per-call attrs in the structured record.
func NewSlog(handler slog.Handler, extractors ...ContextExtractor) Logger {
	if handler == nil {
		panic("logger.NewSlog: handler must not be nil")
	}
	return &slogLogger{inner: slog.New(handler), extractors: extractors}
}

// NewSlogFromLogger adapts an already-constructed *slog.Logger.
// See NewSlog for extractor semantics.
func NewSlogFromLogger(l *slog.Logger, extractors ...ContextExtractor) Logger {
	if l == nil {
		panic("logger.NewSlogFromLogger: l must not be nil")
	}
	return &slogLogger{inner: l, extractors: extractors}
}

func (s *slogLogger) Debug(ctx context.Context, msg string, args ...any) {
	s.inner.DebugContext(ctx, msg, s.applyExtractors(ctx, args)...)
}

func (s *slogLogger) Info(ctx context.Context, msg string, args ...any) {
	s.inner.InfoContext(ctx, msg, s.applyExtractors(ctx, args)...)
}

func (s *slogLogger) Warn(ctx context.Context, msg string, args ...any) {
	s.inner.WarnContext(ctx, msg, s.applyExtractors(ctx, args)...)
}

func (s *slogLogger) Error(ctx context.Context, msg string, args ...any) {
	s.inner.ErrorContext(ctx, msg, s.applyExtractors(ctx, args)...)
}

func (s *slogLogger) With(args ...any) Logger {
	return &slogLogger{inner: s.inner.With(args...), extractors: s.extractors}
}

func (s *slogLogger) Enabled(ctx context.Context, level slog.Level) bool {
	return s.inner.Enabled(ctx, level)
}

// applyExtractors prepends the output of all registered extractors to args.
// Extractor attrs land first so a downstream handler sees trace/request-scoped
// context before any per-call attrs. Returns the input args unchanged when no
// extractors are registered to avoid an allocation on the hot path.
func (s *slogLogger) applyExtractors(ctx context.Context, args []any) []any {
	if len(s.extractors) == 0 {
		return args
	}
	out := make([]any, 0, len(args)+8*len(s.extractors))
	for _, ex := range s.extractors {
		out = append(out, ex(ctx)...)
	}
	return append(out, args...)
}
