// Implements: REQ-009.
// Per: ADR-0029.
// Discipline: C-14.

// Package tracing defines the PlatformKit OSS tracing contract.
//
// The default Tracer is Noop. OpenTelemetry, Jaeger, and Zipkin adapters live
// in downstream packages so the OSS kernel stays dependency-free; callers wire
// the adapter they want at composition time.
//
// Usage:
//
//	tr := tracing.Noop() // or an external adapter
//	ctx, span := tr.Start(ctx, "user.create",
//	    tracing.Attr{Key: "tenant_id", Value: tid})
//	defer span.End()
//	if err := doWork(ctx); err != nil {
//	    span.RecordError(err)
//	    span.SetStatus(tracing.StatusError, err.Error())
//	}
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package tracing
