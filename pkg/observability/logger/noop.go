// Implements: REQ-009.
// Per: ADR-0029.
// Discipline: C-14.

package logger

// noop.go provides a zero-allocation, always-disabled Logger for tests and
// callers that intentionally want logging off.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"log/slog"
)

type noopLogger struct{}

// Noop returns a Logger that discards every record. Safe for concurrent use.
func Noop() Logger { return noopLogger{} }

func (noopLogger) Debug(context.Context, string, ...any)    {}
func (noopLogger) Info(context.Context, string, ...any)     {}
func (noopLogger) Warn(context.Context, string, ...any)     {}
func (noopLogger) Error(context.Context, string, ...any)    {}
func (noopLogger) With(...any) Logger                       { return noopLogger{} }
func (noopLogger) Enabled(context.Context, slog.Level) bool { return false }
