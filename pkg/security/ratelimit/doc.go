// Package ratelimit provides the Limiter contract, an in-memory
// TokenBucket implementation, and HTTP middleware that combines them.
//
// # Why this package exists
//
// Every PlatformKit deployment has at least three places that need rate
// limiting — login endpoints (slow brute-force), password reset endpoints
// (anti-spam), and public API endpoints (capacity protection) — and each
// runs at a different rate, with a different key (client IP, account ID,
// API key). Building a bespoke limiter at each site forks the wheel and
// risks subtle bugs (e.g., racing token-bucket math). Centralizing the
// primitive — and exposing it behind an interface — lets every site reuse
// the same audited implementation while remaining free to swap to a
// Redis-backed or otherwise distributed implementation later without
// touching the call sites.
//
// # How it works
//
// The package exposes Limiter, a small interface whose only method is
// Allow(key string) (allowed bool, retryAfter time.Duration). TokenBucket
// is the default in-memory implementation: each key gets its own bucket
// holding up to Burst tokens, refilling at Rate tokens per second.
// Buckets are reaped from memory after IdleTTL of inactivity to bound
// memory under high-cardinality keys (one per IP).
//
// HTTP middleware wraps a Limiter with a Config that controls how the key
// is extracted, when to bypass the limiter, and a callback for
// observability. Limited requests get HTTP 429 + Retry-After.
//
// # Composable contract
//
// Limiter is the extension surface. Downstream Pro packages implement it
// for Redis (cross-process), leaky-bucket semantics, sliding-window
// counters, or per-tenant fairness — and the same Middleware works against
// any of them. LimiterFunc adapts function literals for one-off limiters
// (tests, in-app closures). Config.KeyFunc/SkipFunc/OnLimited are
// extension hooks: custom keying (per-account, per-API-key), whitelisting
// (allow internal monitors through), and observability (emit metrics on
// limited requests).
//
// # What this package does NOT cover
//
// Token-bucket math, leaky-bucket math, sliding-window counters, GCRA,
// and distributed coordination via Redis or Memcached. TokenBucket is the
// only implementation shipped here; everything else is downstream by
// implementing Limiter.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package ratelimit
