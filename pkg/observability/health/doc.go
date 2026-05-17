// Package health defines the PlatformKit OSS health-check contract.
//
// Usage:
//
//	r := health.NewRegistry()
//	r.Register("db", health.CheckerFunc(func(ctx context.Context) error {
//	    return db.PingContext(ctx)
//	}))
//	http.Handle("/healthz", r.HTTPHandler())
//
// Aggregate status is the worst component status: any Unhealthy → Unhealthy;
// otherwise any Degraded → Degraded; otherwise Healthy.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package health
