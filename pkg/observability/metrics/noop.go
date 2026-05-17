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
// Calls with empty names still panic so misuse is caught early.
func Noop() Metrics { return noopMetrics{} }

func (noopMetrics) Counter(name string, _ ...string) Counter {
	mustName(name)
	return noopCounter{}
}

func (noopMetrics) Gauge(name string, _ ...string) Gauge {
	mustName(name)
	return noopGauge{}
}

func (noopMetrics) Histogram(name string, _ ...string) Histogram {
	mustName(name)
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
