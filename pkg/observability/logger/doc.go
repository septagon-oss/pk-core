// Implements: REQ-009.
// Per: ADR-0029.
// Discipline: C-14.

// Package logger defines the PlatformKit OSS structured logger contract.
//
// Usage:
//
//	import "log/slog"
//	import "github.com/septagon-oss/pk-core/pkg/observability/logger"
//
//	l := logger.NewSlog(slog.NewJSONHandler(os.Stderr, nil))
//	l.Info(ctx, "user signed in", "user_id", uid)
//
// For tests: logger.Noop().
//
// External adapters (OpenTelemetry, Datadog, Honeycomb) live in downstream
// packages; the OSS core depends only on the Go standard library.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package logger
