// Implements: REQ-009.
// Per: ADR-0029.
// Discipline: C-14.

// Package observability defines PlatformKit's provider-neutral observability
// contracts: logger, metrics, tracing, health, and guardrail.
//
// Sub-packages own one concern each and ship stdlib-only default
// implementations. External adapters (OpenTelemetry, Prometheus, Datadog)
// belong in downstream/Pro packages so the OSS kernel stays dependency-free.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package observability
