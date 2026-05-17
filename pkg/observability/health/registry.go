package health

// registry.go provides the default in-memory Registrar. Component results are
// emitted in registration order for deterministic output. Aggregate status is
// the worst component status: any Unhealthy → Unhealthy; otherwise any
// Degraded → Degraded; otherwise Healthy.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
)

type entry struct {
	name    string
	checker Checker
}

type registry struct {
	mu      sync.Mutex
	entries []entry
	indexed map[string]int
}

// NewRegistry returns a default in-memory Registrar.
func NewRegistry() Registrar {
	return &registry{indexed: make(map[string]int)}
}

func (r *registry) Register(name string, checker Checker) {
	if name == "" {
		panic("health: name must not be empty")
	}
	if checker == nil {
		panic("health: checker must not be nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if i, ok := r.indexed[name]; ok {
		r.entries[i].checker = checker
		return
	}
	r.indexed[name] = len(r.entries)
	r.entries = append(r.entries, entry{name: name, checker: checker})
}

func (r *registry) Check(ctx context.Context) Result {
	r.mu.Lock()
	snapshot := make([]entry, len(r.entries))
	copy(snapshot, r.entries)
	r.mu.Unlock()

	out := Result{Status: StatusHealthy, Components: make([]ComponentResult, 0, len(snapshot))}
	for _, e := range snapshot {
		cr := ComponentResult{Name: e.name, Status: StatusHealthy}
		if err := e.checker.Check(ctx); err != nil {
			cr.Status = StatusUnhealthy
			cr.Error = err.Error()
			out.Status = StatusUnhealthy
		}
		out.Components = append(out.Components, cr)
	}
	return out
}

func (r *registry) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		res := r.Check(req.Context())
		w.Header().Set("Content-Type", "application/json")
		if res.Status == StatusUnhealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_ = json.NewEncoder(w).Encode(res)
	})
}
