package metrics

// noop.go provides a zero-allocation Metrics for tests and disabled paths.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

type noopMetrics struct{}
type noopCounter struct{}
type noopGauge struct{}
type noopHistogram struct{}

// Noop returns a Metrics whose Counter/Gauge/Histogram operations are no-ops.
// Calls with invalid names or labels still panic so registration misuse is
// caught even when metrics emission is disabled.
func Noop() Metrics { return noopMetrics{} }

func (noopMetrics) Counter(name string, labels ...string) Counter {
	mustName(name)
	mustLabels(name, labels)
	return noopCounter{}
}

func (noopMetrics) Gauge(name string, labels ...string) Gauge {
	mustName(name)
	mustLabels(name, labels)
	return noopGauge{}
}

func (noopMetrics) Histogram(name string, labels ...string) Histogram {
	mustName(name)
	mustLabels(name, labels)
	return noopHistogram{}
}

func (noopCounter) Add(float64)       {}
func (noopGauge) Set(float64)         {}
func (noopHistogram) Observe(float64) {}

func mustName(name string) {
	if name == "" {
		panic("metrics: name must not be empty")
	}
}

func mustLabels(name string, labels []string) {
	if len(labels)%2 != 0 {
		panic("metrics: labels must be alternating key/value pairs (odd count: " + name + ")")
	}
}
