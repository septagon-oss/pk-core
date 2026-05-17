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
	"strings"
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

func (e *expvarMetrics) Counter(name string, labels ...string) Counter {
	mustName(name)
	return &expvarCounter{f: e.float(encodeKey(name, labels))}
}

func (e *expvarMetrics) Gauge(name string, labels ...string) Gauge {
	mustName(name)
	return &expvarGauge{f: e.float(encodeKey(name, labels))}
}

// Histogram registers two expvar.Float keys: "<key>_sum" and "<key>_count",
// where <key> is the labeled metric key (see encodeKey). Callers must NOT
// register a separate Counter/Gauge/Histogram under any of "<key>",
// "<key>_sum", or "<key>_count" — the underlying expvar storage is shared by
// name, and a collision corrupts both metrics silently. This default is
// deliberately lean; collision detection lives in adapter implementations
// (Prometheus, OpenTelemetry) where the metric type system catches the mistake.
func (e *expvarMetrics) Histogram(name string, labels ...string) Histogram {
	mustName(name)
	key := encodeKey(name, labels)
	return &expvarHistogram{
		sum:   e.float(key + "_sum"),
		count: e.float(key + "_count"),
	}
}

// encodeKey produces a stable, observable storage key from a metric name plus
// alternating label key/value pairs. Empty labels list returns the raw name;
// non-empty labels produce "name{k1=v1,k2=v2,...}" (deterministic order, as
// supplied by the caller). An odd-length labels slice is a contract violation
// and panics immediately so misuse surfaces at registration time.
func encodeKey(name string, labels []string) string {
	if len(labels)%2 != 0 {
		panic("metrics: labels must be alternating key/value pairs (odd count: " + name + ")")
	}
	if len(labels) == 0 {
		return name
	}
	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('{')
	for i := 0; i+1 < len(labels); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(labels[i])
		b.WriteByte('=')
		b.WriteString(labels[i+1])
	}
	b.WriteByte('}')
	return b.String()
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
