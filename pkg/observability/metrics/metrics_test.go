package metrics_test

// metrics_test.go validates the Metrics contract: Counter increments, Gauge
// sets, Histogram observes, and Noop preserves registration guardrails.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"testing"

	"github.com/septagon-oss/pk-core/pkg/observability/metrics"
)

func TestNoopAcceptsAllOperations(t *testing.T) {
	t.Parallel()
	m := metrics.Noop()
	m.Counter("requests_total", "method", "GET").Add(1)
	m.Gauge("queue_depth").Set(42)
	m.Histogram("latency_ms").Observe(12.3)
}

func TestNoopDoesNotExposeHTTP(t *testing.T) {
	t.Parallel()
	if _, ok := metrics.HTTPHandler(metrics.Noop()); ok {
		t.Fatal("noop metrics should not expose an HTTP handler")
	}
}

func TestCounterRequiresNameNotEmpty(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty name")
		}
	}()
	metrics.Noop().Counter("")
}

func TestNoopRequiresPairedLabels(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on odd label count")
		}
	}()
	metrics.Noop().Counter("requests_total", "method")
}
