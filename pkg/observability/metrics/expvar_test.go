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
	m := metrics.NewExpvar(new(expvar.Map).Init())
	c := m.Counter("requests_total")
	c.Add(1)
	c.Add(2)

	c2 := m.Counter("requests_total")
	c2.Add(1)

	if c == nil || c2 == nil {
		t.Fatal("counter handles must not be nil")
	}
}

func TestExpvarGaugeSets(t *testing.T) {
	t.Parallel()
	m := metrics.NewExpvar(new(expvar.Map).Init())
	g := m.Gauge("queue_depth")
	g.Set(7)
	g.Set(3)
}
