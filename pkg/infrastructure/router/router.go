// Package router — router.go owns the public surface of the router
// contract: the Router interface, the NewServeMux constructor, and the
// unexported serveMuxRouter implementation that satisfies Router using
// stdlib net/http.ServeMux + the Go 1.22+ pattern syntax.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package router

import (
	"net/http"
	"strings"
	"sync"
)

// Router is the provider-neutral HTTP router contract.
//
// The OSS default is stdlib net/http.ServeMux (NewServeMux); Pro
// adapters plug in chi, gorilla/mux, gin, etc., by implementing the
// same three operations.
//
// Implementations MUST be safe for concurrent Method/Use registration
// before ServeHTTP starts serving, and safe for concurrent ServeHTTP
// calls thereafter. The stdlib default satisfies both naturally.
type Router interface {
	// http.Handler: ServeHTTP routes incoming requests through the
	// configured middleware chain and then to the registered handler
	// for (method, pattern).
	http.Handler

	// Method registers handler for the given HTTP method + pattern.
	// The OSS default uses Go 1.22+ pattern syntax which natively
	// handles wildcards and per-method dispatch; implementations MAY
	// extend this with their own routing features behind the same
	// signature.
	Method(method, pattern string, handler http.HandlerFunc)

	// Use appends middlewares to the chain wrapping ServeHTTP. The
	// outermost middleware (last in the chain when reading the
	// request flow) is the first one registered, mirroring the
	// "Russian doll" pattern that chi, gorilla, and gin all share.
	Use(middlewares ...func(http.Handler) http.Handler)
}

// NewServeMux returns a Router backed by stdlib http.ServeMux.
//
// The router exploits Go 1.22+ method+pattern syntax: registering
// "GET /users" routes only GET requests to that handler, and an
// unmatched method against a matched path returns 405 Method Not
// Allowed natively (the stdlib mux handles this for us).
func NewServeMux() Router {
	return &serveMuxRouter{
		mux: http.NewServeMux(),
	}
}

// serveMuxRouter wraps http.ServeMux + a middleware chain. The mux
// owns route storage; the wrapper owns the middleware list and the
// composed handler that ServeHTTP delegates to.
type serveMuxRouter struct {
	mux *http.ServeMux

	mu          sync.RWMutex
	middlewares []func(http.Handler) http.Handler

	// composedOnce + composed cache the middleware-wrapped handler so
	// every request does not re-walk the slice. Method/Use both
	// invalidate the cache by zeroing composed and resetting once.
	composedOnce sync.Once
	composed     http.Handler
}

// Method registers handler for "<METHOD> <pattern>", which the Go 1.22+
// ServeMux parses into a method-specific route. Pattern syntax follows
// the stdlib documentation; e.g., "/users/{id}" captures a path param.
func (r *serveMuxRouter) Method(method, pattern string, handler http.HandlerFunc) {
	method = strings.ToUpper(strings.TrimSpace(method))
	pattern = strings.TrimSpace(pattern)
	r.mux.HandleFunc(method+" "+pattern, handler)
	r.invalidate()
}

// Use appends middlewares to the chain.
func (r *serveMuxRouter) Use(middlewares ...func(http.Handler) http.Handler) {
	r.mu.Lock()
	r.middlewares = append(r.middlewares, middlewares...)
	r.mu.Unlock()
	r.invalidate()
}

// ServeHTTP dispatches w/req through the cached middleware chain to
// the underlying mux.
func (r *serveMuxRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.composedOnce.Do(r.build)
	r.composed.ServeHTTP(w, req)
}

// build composes the middleware chain around the mux. Called lazily on
// first ServeHTTP and after every invalidate.
//
// The chain wraps inside-out: the last middleware registered runs
// closest to the mux; the first registered runs first on the way in.
func (r *serveMuxRouter) build() {
	r.mu.RLock()
	mws := append([]func(http.Handler) http.Handler(nil), r.middlewares...)
	r.mu.RUnlock()

	var h http.Handler = r.mux
	// Iterate in reverse so middlewares[0] ends up outermost.
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	r.composed = h
}

// invalidate resets the composed-handler cache so the next ServeHTTP
// rebuilds the chain. Method and Use both call this; under heavy live
// reconfiguration there is a tiny window where requests may see a
// stale chain, which is acceptable for the OSS contract — routing is
// configured at startup, not per-request.
func (r *serveMuxRouter) invalidate() {
	r.mu.Lock()
	r.composedOnce = sync.Once{}
	r.composed = nil
	r.mu.Unlock()
}
