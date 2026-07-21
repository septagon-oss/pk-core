// Implements: REQ-002.
// Per: ADR-0029.
// Discipline: C-14.

// Package router defines the provider-neutral HTTP router contract for
// the PlatformKit OSS kernel and ships a stdlib net/http.ServeMux
// default that exploits the Go 1.22+ method+pattern syntax.
//
// # Why this package exists
//
// Modules register HTTP routes from many places (CRUD handlers, custom
// endpoints, admin pages) and must not bind the kernel to a specific
// router library. chi, gorilla/mux, gin, and stdlib ServeMux all expose
// different APIs; pinning to any one of them prevents Pro adapters from
// swapping the router for performance, middleware ergonomics, or
// feature parity reasons. This package owns a 3-method Router interface
// (the stdlib http.Handler embed plus Method + Use) so call sites code
// against the contract; the OSS default is ServeMux, and Pro adapters
// plug in by implementing Router.
//
// # Block scorecard
//
//   - Identity:        github.com/septagon-oss/pk-core/pkg/infrastructure/router
//   - Boundary:        Router is an interface that embeds http.Handler;
//     concrete implementations are returned through the
//     interface so callers never depend on ServeMux directly.
//   - Contract:        Router { ServeHTTP, Method, Use } + NewServeMux
//     constructor.
//   - Composition:     Middleware stacks via Use; each middleware is a
//     standard func(http.Handler) http.Handler so any
//     stdlib-shaped middleware composes.
//   - Replacement:     Any type implementing Router is a drop-in.
//   - Extension:       Pro / downstream adapters publish their own
//     Router constructors (chi, gin, etc.).
//   - Runtime binding: Application composition picks the constructor;
//     handlers register methods + patterns the same way
//     regardless of backend.
//   - Evidence:        router_test.go covers GET/POST/PUT/DELETE
//     routing, middleware chaining, and Go 1.22's native
//     405 method-not-allowed response.
//
// # Chainable laws
//
//   - Identity:        a no-op middleware that returns the next handler
//     unchanged is a valid composition element.
//   - Type compat:     every middleware is func(http.Handler) http.Handler;
//     every Router serves http.Handler so layering is the
//     stdlib happy path.
//   - Context preserve: handlers receive the request's ctx unchanged;
//     middleware may wrap it without breaking the contract.
//   - Error algebra:   HTTP errors flow via ResponseWriter status codes;
//     the router does not invent its own error type.
//   - Cancellation:    request ctx cancellation flows through stdlib
//     net/http; no router-specific hook needed.
//
// # External dependencies
//
// None. The contract and OSS default are pure stdlib so the kernel
// stays audit-friendly.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package router
