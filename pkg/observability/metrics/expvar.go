package metrics

// expvar.go provides a stdlib expvar-backed Metrics implementation.
// Metric handles are cached by name on the configured *expvar.Map so repeated
// lookups return the same underlying expvar.Float. Histograms are recorded as
// running sums and counts under "<name>_sum" / "<name>_count" keys to avoid
// pulling in a histogram dependency.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"expvar"
	"sync"
)

type expvarMetrics struct {
	mu  sync.Mutex
	m   *expvar.Map
	cnt map[string]*expvar.Float
}

// NewExpvar returns a Metrics that records into the given expvar.Map.
// Callers typically pass expvar.NewMap("pk") at process start so the metrics
// surface at /debug/vars.
func NewExpvar(m *expvar.Map) Metrics {
	if m == nil {
		panic("metrics.NewExpvar: map must not be nil")
	}
	return &expvarMetrics{m: m, cnt: make(map[string]*expvar.Float)}
}

func (e *expvarMetrics) Counter(name string, _ ...string) Counter {
	mustName(name)
	return &expvarCounter{f: e.float(name)}
}

func (e *expvarMetrics) Gauge(name string, _ ...string) Gauge {
	mustName(name)
	return &expvarGauge{f: e.float(name)}
}

// Histogram registers two expvar.Float keys: "<name>_sum" and "<name>_count".
// Callers must NOT register a separate Counter/Gauge/Histogram under any of
// "<name>", "<name>_sum", or "<name>_count" — the underlying expvar storage
// is shared by metric name, and a collision corrupts both metrics silently.
// This default is deliberately lean; collision detection lives in adapter
// implementations (Prometheus, OpenTelemetry) where the metric type system
// catches the mistake.
func (e *expvarMetrics) Histogram(name string, _ ...string) Histogram {
	mustName(name)
	return &expvarHistogram{
		sum:   e.float(name + "_sum"),
		count: e.float(name + "_count"),
	}
}

func (e *expvarMetrics) float(name string) *expvar.Float {
	e.mu.Lock()
	defer e.mu.Unlock()
	if f, ok := e.cnt[name]; ok {
		return f
	}
	f := new(expvar.Float)
	e.m.Set(name, f)
	e.cnt[name] = f
	return f
}

type expvarCounter struct{ f *expvar.Float }
type expvarGauge struct{ f *expvar.Float }
type expvarHistogram struct{ sum, count *expvar.Float }

func (c *expvarCounter) Add(delta float64) { c.f.Add(delta) }
func (g *expvarGauge) Set(value float64)   { g.f.Set(value) }
func (h *expvarHistogram) Observe(v float64) {
	h.sum.Add(v)
	h.count.Add(1)
}
