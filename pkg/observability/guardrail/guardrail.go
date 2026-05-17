// Package guardrail standardizes warning logs emitted when PlatformKit takes a
// safe fallback or degraded execution path to keep the system operating.
//
// guardrail.go provides the Warn family of helpers; every emitted record
// carries a stable schema (Standard) and a Mode tag so monitoring systems can
// aggregate guardrail events independently of business logs.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package guardrail

import (
	"context"
	"strings"

	"github.com/septagon-oss/pk-core/pkg/observability/logger"
)

// Standard identifies the current PlatformKit guardrail-warning schema.
const Standard = "platformkit.guardrail.v1"

// Mode classifies why a guardrail warning was emitted.
type Mode string

const (
	ModeFallback         Mode = "fallback"
	ModeDegraded         Mode = "degraded"
	ModeUnsupported      Mode = "unsupported"
	ModeConfigurationGap Mode = "configuration_gap"
	ModeSoftEmpty        Mode = "soft_empty"
)

// Warn emits a standardized guardrail warning. extraArgs follow the slog
// alternating key/value convention.
func Warn(ctx context.Context, log logger.Logger, mode Mode, code, message string, extraArgs ...any) {
	if log == nil {
		return
	}
	args := []any{
		"guardrail", true,
		"guardrail_standard", Standard,
		"guardrail_mode", strings.TrimSpace(string(mode)),
	}
	if trimmed := strings.TrimSpace(code); trimmed != "" {
		args = append(args, "guardrail_code", trimmed)
	}
	args = append(args, extraArgs...)
	log.Warn(ctx, message, args...)
}

// WarnFallback is a shorthand for Warn with ModeFallback.
func WarnFallback(ctx context.Context, log logger.Logger, code, message string, extraArgs ...any) {
	Warn(ctx, log, ModeFallback, code, message, extraArgs...)
}

// WarnDegraded is a shorthand for Warn with ModeDegraded.
func WarnDegraded(ctx context.Context, log logger.Logger, code, message string, extraArgs ...any) {
	Warn(ctx, log, ModeDegraded, code, message, extraArgs...)
}

// WarnUnsupported is a shorthand for Warn with ModeUnsupported.
func WarnUnsupported(ctx context.Context, log logger.Logger, code, message string, extraArgs ...any) {
	Warn(ctx, log, ModeUnsupported, code, message, extraArgs...)
}

// WarnConfigurationGap is a shorthand for Warn with ModeConfigurationGap.
func WarnConfigurationGap(ctx context.Context, log logger.Logger, code, message string, extraArgs ...any) {
	Warn(ctx, log, ModeConfigurationGap, code, message, extraArgs...)
}

// WarnSoftEmpty is a shorthand for Warn with ModeSoftEmpty.
func WarnSoftEmpty(ctx context.Context, log logger.Logger, code, message string, extraArgs ...any) {
	Warn(ctx, log, ModeSoftEmpty, code, message, extraArgs...)
}
