// Package guardrail emits standardized warnings when PlatformKit takes a
// fallback or degraded path. Monitoring systems can scrape on
// guardrail=true to surface "I kept working but something is wrong"
// events independent of regular warnings.
//
// Usage:
//
//	guardrail.WarnFallback(ctx, log,
//	    "user.lookup_email_fallback",
//	    "primary identity lookup failed; falling back to email match",
//	    "tenant_id", tid,
//	    "identity_id", iid,
//	)
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package guardrail
