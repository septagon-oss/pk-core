package metrics_test

// expvar_test.go validates the expvar-backed Metrics: counters survive
// repeated lookups, values increment, and metric names are exported on the
// configured Map.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"expvar"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/observability/metrics"
)

func TestExpvarCounterIncrements(t *testing.T) {
	t.Parallel()
	mp := new(expvar.Map).Init()
	m := metrics.NewExpvar(mp)
	c := m.Counter("requests_total")
	c.Add(1)
	c.Add(2)

	c2 := m.Counter("requests_total")
	c2.Add(1)

	got := mp.Get("requests_total").(*expvar.Float).Value()
	if got != 4 {
		t.Fatalf("requests_total = %v, want 4 (cache should share storage)", got)
	}
}

func TestExpvarGaugeSets(t *testing.T) {
	t.Parallel()
	mp := new(expvar.Map).Init()
	m := metrics.NewExpvar(mp)
	g := m.Gauge("queue_depth")
	g.Set(7)
	g.Set(3)

	got := mp.Get("queue_depth").(*expvar.Float).Value()
	if got != 3 {
		t.Fatalf("queue_depth = %v, want 3 (last Set wins)", got)
	}
}

func TestExpvarHistogramSumAndCount(t *testing.T) {
	t.Parallel()
	mp := new(expvar.Map).Init()
	m := metrics.NewExpvar(mp)
	h := m.Histogram("latency_ms")
	h.Observe(10)
	h.Observe(20)
	h.Observe(30)

	sum, ok := mp.Get("latency_ms_sum").(*expvar.Float)
	if !ok || sum == nil {
		t.Fatalf("latency_ms_sum not registered as *expvar.Float")
	}
	if sum.Value() != 60 {
		t.Fatalf("sum = %v, want 60", sum.Value())
	}

	count, ok := mp.Get("latency_ms_count").(*expvar.Float)
	if !ok || count == nil {
		t.Fatalf("latency_ms_count not registered as *expvar.Float")
	}
	if count.Value() != 3 {
		t.Fatalf("count = %v, want 3", count.Value())
	}
}

func TestExpvarLabelsProduceDistinctStorage(t *testing.T) {
	t.Parallel()
	mp := new(expvar.Map).Init()
	m := metrics.NewExpvar(mp)
	m.Counter("requests", "method", "GET").Add(1)
	m.Counter("requests", "method", "GET").Add(2)
	m.Counter("requests", "method", "POST").Add(7)

	if got := mp.Get("requests{method=GET}").(*expvar.Float).Value(); got != 3 {
		t.Fatalf("GET counter = %v, want 3", got)
	}
	if got := mp.Get("requests{method=POST}").(*expvar.Float).Value(); got != 7 {
		t.Fatalf("POST counter = %v, want 7", got)
	}
}

func TestExpvarOddLabelsPanic(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for odd-length labels")
		}
	}()
	mp := new(expvar.Map).Init()
	metrics.NewExpvar(mp).Counter("requests", "method")
}
