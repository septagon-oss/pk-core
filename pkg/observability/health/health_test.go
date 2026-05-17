package health_test

// health_test.go validates the Registry: registering checkers, aggregating
// results, and producing a usable HTTP /healthz response.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/observability/health"
)

func TestRegistryReportsAllChecks(t *testing.T) {
	t.Parallel()
	r := health.NewRegistry()
	r.Register("db", health.CheckerFunc(func(context.Context) error { return nil }))
	r.Register("cache", health.CheckerFunc(func(context.Context) error { return errors.New("down") }))

	res := r.Check(context.Background())
	if res.Status != health.StatusUnhealthy {
		t.Fatalf("Status = %v, want Unhealthy", res.Status)
	}
	if len(res.Components) != 2 {
		t.Fatalf("Components len = %d", len(res.Components))
	}
}

func TestRegistryHealthyWhenAllPass(t *testing.T) {
	t.Parallel()
	r := health.NewRegistry()
	r.Register("db", health.CheckerFunc(func(context.Context) error { return nil }))

	res := r.Check(context.Background())
	if res.Status != health.StatusHealthy {
		t.Fatalf("Status = %v, want Healthy", res.Status)
	}
}

func TestHTTPHandlerReturnsJSON(t *testing.T) {
	t.Parallel()
	r := health.NewRegistry()
	r.Register("db", health.CheckerFunc(func(context.Context) error { return nil }))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	r.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestHTTPHandlerReturns503WhenUnhealthy(t *testing.T) {
	t.Parallel()
	r := health.NewRegistry()
	r.Register("db", health.CheckerFunc(func(context.Context) error { return errors.New("down") }))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	r.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
