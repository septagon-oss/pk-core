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
	"fmt"
	"net/http"
	"sync"
)

type entry struct {
	name    string
	checker Checker
	cfg     config
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

func (r *registry) Register(name string, checker Checker, opts ...Option) {
	if name == "" {
		panic("health: name must not be empty")
	}
	if checker == nil {
		panic("health: checker must not be nil")
	}
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if i, ok := r.indexed[name]; ok {
		r.entries[i].checker = checker
		r.entries[i].cfg = cfg
		return
	}
	r.indexed[name] = len(r.entries)
	r.entries = append(r.entries, entry{name: name, checker: checker, cfg: cfg})
}

func (r *registry) Check(ctx context.Context) Result {
	r.mu.Lock()
	snapshot := make([]entry, len(r.entries))
	copy(snapshot, r.entries)
	r.mu.Unlock()

	out := Result{Status: StatusHealthy, Components: make([]ComponentResult, 0, len(snapshot))}
	for _, e := range snapshot {
		cr := ComponentResult{Name: e.name, Status: StatusHealthy}
		if err := runChecker(ctx, e.checker, e.cfg); err != nil {
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
		switch res.Status {
		case StatusUnhealthy:
			w.WriteHeader(http.StatusServiceUnavailable)
		case StatusDegraded:
			// Degraded means the service is still reachable but partially
			// impaired. Map to 200 so liveness/readiness probes keep the pod
			// in service; clients can read res.Components for detail.
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
		_ = json.NewEncoder(w).Encode(res)
	})
}

// runChecker invokes c with panic recovery and optional per-checker timeout.
// A panicked checker degrades only its own component (error "checker panicked:
// <msg>"); a timed-out check returns "checker timed out after <d>" so the
// aggregate report stays deterministic even when adapter code hangs.
func runChecker(ctx context.Context, c Checker, cfg config) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("checker panicked: %v", p)
		}
	}()

	if cfg.timeout > 0 {
		runCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
		defer cancel()

		done := make(chan error, 1)
		go func() {
			defer func() {
				if p := recover(); p != nil {
					done <- fmt.Errorf("checker panicked: %v", p)
					return
				}
			}()
			done <- c.Check(runCtx)
		}()

		select {
		case err = <-done:
			return err
		case <-runCtx.Done():
			return fmt.Errorf("checker timed out after %s", cfg.timeout)
		}
	}

	return c.Check(ctx)
}
