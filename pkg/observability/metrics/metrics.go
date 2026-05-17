// Package metrics defines PlatformKit's provider-neutral metrics interface.
//
// metrics.go owns the public Metrics contract and the three primitive types:
// Counter (monotonic add), Gauge (settable), Histogram (observable
// distribution).
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package metrics

import "net/http"

// Metrics is the provider-neutral metrics contract.
//
// Metric names share a single namespace across Counter/Gauge/Histogram in the
// default implementations; callers must use distinct names per metric kind.
// Labels are alternating key/value pairs and DO produce distinct underlying
// storage in the default implementations (so "requests{method=GET}" and
// "requests{method=POST}" are tracked independently). Odd-length label lists
// panic at registration time. Adapter implementations (Prometheus, OpenTelemetry)
// may enforce additional rules.
//
// Implementations must be safe for concurrent use and must panic on empty
// metric names so misuse is caught at registration time, not runtime.
type Metrics interface {
	Counter(name string, labels ...string) Counter
	Gauge(name string, labels ...string) Gauge
	Histogram(name string, labels ...string) Histogram
}

// HTTPExporter is the optional capability implemented by metrics adapters that
// can expose their current values through HTTP.
//
// It is intentionally separate from Metrics so push-based adapters can record
// metrics without pretending to own an HTTP projection.
type HTTPExporter interface {
	HTTPHandler() http.Handler
}

// HTTPHandler returns m's HTTP exporter when the adapter provides one.
func HTTPHandler(m Metrics) (http.Handler, bool) {
	exporter, ok := m.(HTTPExporter)
	if !ok {
		return nil, false
	}
	return exporter.HTTPHandler(), true
}

// Counter is a monotonically-increasing scalar metric.
// Callers must pass delta >= 0. Passing a negative delta is a contract
// violation; behavior is implementation-defined and may produce nonsense
// readings or panic in stricter adapters.
type Counter interface {
	// Add increments the counter by delta. delta must be >= 0.
	Add(delta float64)
}

// Gauge is a settable scalar metric.
type Gauge interface {
	Set(value float64)
}

// Histogram observes value distributions.
type Histogram interface {
	Observe(value float64)
}
