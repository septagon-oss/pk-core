// Package identity centralizes the Principal abstraction every PlatformKit
// authentication module reads from request context.
//
// # Why this package exists
//
// Authentication modules (cookies, bearer tokens, OAuth, SAML, SCIM,
// magic-link, API keys, mTLS, …) all answer the same question: "who is this
// request from?" Each historically attached its own struct to the request
// context with its own key, forcing downstream authorization code to know
// which authentication mechanism produced the caller. That coupling makes
// adding a new authentication mechanism a cross-module refactor.
//
// This package owns the contract instead. Every resolver — built-in or
// downstream — produces the same Principal value type and attaches it under
// the same package-private context key. Authorization code reads
// PrincipalFromContext and never names the authentication mechanism.
//
// # How it works
//
// A request enters the server. Middleware runs an IdentityResolver against
// it. The resolver returns either:
//
//   - a non-anonymous Principal — credentials were presented and accepted;
//   - an anonymous Principal (zero value) with nil error — no credentials
//     were presented; the request continues unauthenticated;
//   - a non-nil error — credentials were presented but invalid; the
//     middleware writes 401 and stops the chain.
//
// Multiple resolvers compose via Chain — try cookie, then bearer token,
// then API key, etc. The chain itself satisfies IdentityResolver, so it
// composes recursively.
//
// # Composable contract
//
// The Principal type is a value type with EXPORTED fields. Downstream Pro
// packages construct Principals directly when implementing custom
// resolvers. IdentityResolver is an interface; downstream resolvers
// (JWT, OAuth, SAML, SCIM, magic-link, mTLS) implement it without
// importing this package's internals. ResolverFunc adapts function
// literals to the interface for lightweight resolvers.
//
// # What this package does NOT cover
//
// Credential storage, password hashing, token signing, OAuth provider
// discovery, SAML assertion parsing, and session persistence are out of
// scope. Those concerns live in their own blocks (passhash, signature,
// cookies, …) or in downstream authentication modules. This package only
// owns the in-memory contract between "someone resolved who this request
// is from" and "an authorization decision needs the answer."
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package identity
