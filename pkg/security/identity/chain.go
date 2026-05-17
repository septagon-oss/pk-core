// Package identity — chain.go owns the Chain composition helper. Chain
// returns an IdentityResolver that consults its children in order and
// returns the first non-anonymous principal, allowing apps to mix
// authentication mechanisms without each resolver knowing about the
// others.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package identity

import "net/http"

// chain composes multiple IdentityResolvers. Held as a slice so the chain
// can iterate cheaply; the value type satisfies IdentityResolver directly
// so chains can be nested via Chain(Chain(a, b), c).
type chain []IdentityResolver

// Resolve walks the chain in order. The first child returning a non-nil
// error short-circuits the chain with that error. The first child
// returning a non-anonymous Principal short-circuits with that Principal.
// If every child returns an anonymous Principal with nil error the chain
// returns the zero Principal with nil error — the request is treated as
// unauthenticated.
func (c chain) Resolve(r *http.Request) (Principal, error) {
	for _, resolver := range c {
		if resolver == nil {
			continue
		}
		p, err := resolver.Resolve(r)
		if err != nil {
			return Principal{}, err
		}
		if !p.IsAnonymous() {
			return p, nil
		}
	}
	return Principal{}, nil
}

// Chain composes multiple IdentityResolvers into one. Common pattern:
// try the cookie-based session resolver first, fall back to a bearer
// token resolver for API clients, finally fall back to anonymous. Nil
// resolvers in the argument list are skipped so callers can build chains
// from optional configuration without nil-guarding each entry.
//
// The returned resolver itself satisfies IdentityResolver, so chains can
// be composed of chains.
func Chain(resolvers ...IdentityResolver) IdentityResolver {
	// Defensive copy so later mutations to resolvers do not affect the
	// returned chain.
	c := make(chain, 0, len(resolvers))
	c = append(c, resolvers...)
	return c
}
