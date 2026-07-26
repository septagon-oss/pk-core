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
	"runtime"
	"time"
)

// callerSkipBase is the number of frames between runtime.Callers (called inside
// log) and the caller of the exported Debug/Info/Warn/Error method, for a
// Logger used directly: [runtime.Callers, log, Debug/Info/Warn/Error]. A
// slogLogger's own skip is added to this to account for transparent wrapper
// layers above it (see WithCallerSkip). runtime.Callers counts logical frames,
// so this stays correct under inlining — the same reason stdlib slog's own
// fixed skip works.
const callerSkipBase = 3

type slogLogger struct {
	inner      *slog.Logger
	extractors []ContextExtractor
	skip       int // additional frames contributed by wrapper layers above this one
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
	s.log(ctx, slog.LevelDebug, msg, args...)
}

func (s *slogLogger) Info(ctx context.Context, msg string, args ...any) {
	s.log(ctx, slog.LevelInfo, msg, args...)
}

func (s *slogLogger) Warn(ctx context.Context, msg string, args ...any) {
	s.log(ctx, slog.LevelWarn, msg, args...)
}

func (s *slogLogger) Error(ctx context.Context, msg string, args ...any) {
	s.log(ctx, slog.LevelError, msg, args...)
}

// log builds and dispatches the record with a source location that points at
// the real call site rather than at this wrapper file.
//
// The standard library's Logger.log captures the caller PC with a fixed frame
// skip that assumes it is invoked directly, so records reached through this
// wrapper — and through any transparent adapter above it — would be attributed
// to the wrapper. We capture the PC ourselves with an equivalent fixed skip
// plus s.skip, the frame count declared by wrapper layers via WithCallerSkip.
// This is a single-PC capture (no stack scan, no symbolication) and is robust
// to inlining because runtime.Callers counts logical frames.
func (s *slogLogger) log(ctx context.Context, level slog.Level, msg string, args ...any) {
	// Evaluate extractors first, unconditionally and with the caller's context
	// as-is, matching the prior argument-evaluation order and the "every log
	// call" extractor contract (side-effecting extractors must still run).
	merged := s.applyExtractors(ctx, args)
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.inner.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(callerSkipBase+s.skip, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(merged...)
	_ = s.inner.Handler().Handle(ctx, r)
}

func (s *slogLogger) With(args ...any) Logger {
	return &slogLogger{inner: s.inner.With(args...), extractors: s.extractors, skip: s.skip}
}

// WithCallerSkip returns a child that adds skip frames to source resolution.
// Negative arguments are clamped to zero; skips accumulate across layers.
func (s *slogLogger) WithCallerSkip(skip int) Logger {
	if skip < 0 {
		skip = 0
	}
	return &slogLogger{inner: s.inner, extractors: s.extractors, skip: s.skip + skip}
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
