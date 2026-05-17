// Package metrics defines PlatformKit's provider-neutral metrics interface.
//
// metrics.go owns the public Metrics contract and the three primitive types:
// Counter (monotonic add), Gauge (settable), Histogram (observable
// distribution).
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package metrics

// Metrics is the provider-neutral metrics contract. Implementations must be
// safe for concurrent use and must panic on empty metric names so misuse is
// caught at registration time, not runtime.
type Metrics interface {
	Counter(name string, labels ...string) Counter
	Gauge(name string, labels ...string) Gauge
	Histogram(name string, labels ...string) Histogram
}

// Counter is a monotonically-increasing scalar metric.
type Counter interface {
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
