package health_test

// health_test.go validates the Registry: registering checkers, aggregating
// results, and producing a usable HTTP /healthz response.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	var got health.Result
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Status != health.StatusHealthy {
		t.Fatalf("body.Status = %v, want Healthy", got.Status)
	}
	if len(got.Components) != 1 || got.Components[0].Name != "db" {
		t.Fatalf("body.Components = %+v, want one entry named db", got.Components)
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

func TestRegistryRecoversFromPanickingChecker(t *testing.T) {
	t.Parallel()
	r := health.NewRegistry()
	r.Register("ok", health.CheckerFunc(func(context.Context) error { return nil }))
	r.Register("explodes", health.CheckerFunc(func(context.Context) error {
		panic("boom")
	}))

	res := r.Check(context.Background())
	if res.Status != health.StatusUnhealthy {
		t.Fatalf("Status = %v, want Unhealthy after panic", res.Status)
	}
	if len(res.Components) != 2 {
		t.Fatalf("Components len = %d, want 2", len(res.Components))
	}
	// Find the panicking component and verify its error message.
	var found *health.ComponentResult
	for i := range res.Components {
		if res.Components[i].Name == "explodes" {
			found = &res.Components[i]
			break
		}
	}
	if found == nil {
		t.Fatal("explodes component missing from result")
	}
	if found.Status != health.StatusUnhealthy {
		t.Fatalf("explodes.Status = %v, want Unhealthy", found.Status)
	}
	if found.Error == "" {
		t.Fatal("explodes.Error should be set after panic")
	}
}

func TestRegisterReplacesExistingByName(t *testing.T) {
	t.Parallel()
	r := health.NewRegistry()
	r.Register("db", health.CheckerFunc(func(context.Context) error { return nil }))
	r.Register("db", health.CheckerFunc(func(context.Context) error { return errors.New("now down") }))

	res := r.Check(context.Background())
	if len(res.Components) != 1 {
		t.Fatalf("Components len = %d, want 1 (re-register should replace, not append)", len(res.Components))
	}
	if res.Components[0].Status != health.StatusUnhealthy {
		t.Fatalf("Components[0].Status = %v, want Unhealthy (second registration should win)", res.Components[0].Status)
	}
}

func TestEmptyRegistryReportsHealthy(t *testing.T) {
	t.Parallel()
	r := health.NewRegistry()
	res := r.Check(context.Background())
	if res.Status != health.StatusHealthy {
		t.Fatalf("Status = %v, want Healthy for empty registry", res.Status)
	}
	if res.Components == nil {
		t.Fatal("Components must be non-nil so JSON marshals as [] not null")
	}
	if len(res.Components) != 0 {
		t.Fatalf("len(Components) = %d, want 0", len(res.Components))
	}
}

func TestRegistryEnforcesCheckerTimeout(t *testing.T) {
	t.Parallel()
	r := health.NewRegistry()
	r.Register("slow", health.CheckerFunc(func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return nil
		}
	}), health.WithTimeout(50*time.Millisecond))

	start := time.Now()
	res := r.Check(context.Background())
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Check took %s, expected ~50ms because of timeout", elapsed)
	}
	if res.Status != health.StatusUnhealthy {
		t.Fatalf("Status = %v, want Unhealthy after timeout", res.Status)
	}
	if !strings.Contains(res.Components[0].Error, "timed out") {
		t.Fatalf("Error = %q, want to contain 'timed out'", res.Components[0].Error)
	}
}

func TestRegistryNoTimeoutWhenOptionAbsent(t *testing.T) {
	t.Parallel()
	r := health.NewRegistry()
	r.Register("quick", health.CheckerFunc(func(ctx context.Context) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	}))
	res := r.Check(context.Background())
	if res.Status != health.StatusHealthy {
		t.Fatalf("Status = %v, want Healthy", res.Status)
	}
}
