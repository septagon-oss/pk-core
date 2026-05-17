// Package security defines PlatformKit's provider-neutral security primitives.
//
// Sub-packages own one concern each:
//   - passhash: password hashing (bcrypt, argon2id, short-secret pepper)
//   - cookies:  per-purpose HTTP cookie security profiles
//   - signature: HMAC payload signing
//   - headers: response security headers and CSP nonce plumbing
//   - cors: origin allowlist middleware
//   - csrf: double-submit cookie validation
//   - identity: request principal context and resolver chains
//   - ratelimit: limiter contracts, token bucket, and HTTP middleware
//
// External deps used by this package are whitelisted in
// pk-core/pkg/architecture/oss_deps_test.go. Pro/downstream adapters
// (HSM-backed hashers, KMS signers, OAuth/SAML auth providers) live in
// downstream packages so the OSS kernel remains slim and audit-friendly.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package security
