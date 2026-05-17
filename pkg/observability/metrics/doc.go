// Package metrics defines the PlatformKit OSS metrics contract.
//
// Three primitives — Counter, Gauge, Histogram — keyed by name and optional
// labels. Adapters that can expose their current values over HTTP implement the
// optional HTTPExporter capability. The default implementations are Noop (for
// tests) and expvar (for /debug/vars). Prometheus, OpenTelemetry, and StatsD
// adapters live in downstream packages so the OSS kernel stays dependency-free.
//
// Usage:
//
//	import "expvar"
//	import "github.com/septagon-oss/pk-core/pkg/observability/metrics"
//
//	m := metrics.NewExpvar(expvar.NewMap("pk"))
//	requests := m.Counter("http_requests_total", "method", "GET")
//	requests.Add(1)
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package metrics
