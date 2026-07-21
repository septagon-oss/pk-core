// Implements: REQ-009.
// Per: ADR-0029.
// Discipline: C-14.

// Package logger defines PlatformKit's structured logger interface.
//
// logger.go owns the public Logger contract: level-aware structured logging
// with parent/child relationships and context propagation.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package logger

import (
	"context"
	"log/slog"
)

// Logger is the provider-neutral structured logger contract.
//
// Implementations must accept any slog-compatible attr arguments (alternating
// key/value pairs or pre-built slog.Attr values). Implementations must be safe
// for concurrent use.
type Logger interface {
	Debug(ctx context.Context, msg string, args ...any)
	Info(ctx context.Context, msg string, args ...any)
	Warn(ctx context.Context, msg string, args ...any)
	Error(ctx context.Context, msg string, args ...any)

	// With returns a child Logger that inherits the given attrs on every record.
	With(args ...any) Logger

	// Enabled reports whether the Logger will emit at the given level.
	// Implementations may use this to short-circuit expensive arg construction.
	Enabled(ctx context.Context, level slog.Level) bool
}

// ContextExtractor pulls log attrs from a context.Context. Implementations
// typically read a tracing span, request ID, tenant ID, or other request-scoped
// values and return them as slog-style alternating key/value pairs.
type ContextExtractor func(ctx context.Context) []any
