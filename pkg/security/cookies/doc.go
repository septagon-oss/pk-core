// Package cookies centralizes HTTP cookie writing for PlatformKit.
//
// # Why this package exists
//
// Every place that wrote a session or CSRF cookie before this package
// re-implemented the same struct literal — and one of those literals (the
// session cookie) historically shipped without the Secure flag, leaving JWTs
// exposed over plaintext HTTP. Auditing N call sites for cookie hardening
// does not scale; centralizing the security profile per cookie purpose does.
//
// # How it works
//
// Callers do not construct http.Cookie structs. Instead they pick a Kind
// (KindSession, KindCSRF, …) and the package emits a cookie whose security
// flags (HttpOnly, Secure, SameSite, Path) come from a fixed per-Kind
// profile table. Secure is auto-derived from the request (TLS or
// X-Forwarded-Proto) with a Configure-time override for deployments behind
// a TLS-terminating proxy that does not propagate the original scheme.
//
// # Composable contract
//
// Public surface is a small set of free functions plus Option-style call
// overrides. The Kind enum and per-Kind profile table are the extension
// point: downstream Pro packages add new cookie purposes by contributing
// their own profile entries, never by mutating http.Cookie literals at the
// call site.
//
// # What this package does NOT cover
//
// Browser localStorage and sessionStorage. Those are pure client-side and
// have a different security model — same-origin only, exposed to XSS, no
// HttpOnly equivalent. They are deliberately out of scope here so the
// abstraction does not leak fields that are N/A on each side.
//
// Bearer tokens, API keys, and PII never travel through localStorage. The
// cookies package is the only path the platform sanctions for persisting
// authentication material in a browser.
//
// # Why Option is closed
//
// Option is intentionally typed func(*option) (private) rather than
// func(*http.Cookie). This prevents call-site Options from overriding the
// per-Kind security profile (HttpOnly, SameSite, Path) that the whole
// package exists to centralize. Downstream packages that need new behavior
// add new Kinds via RegisterKind; they do not override existing Kind
// profiles via Options.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package cookies
