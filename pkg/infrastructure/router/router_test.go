// Package router_test exercises the router contract from outside the
// package: it imports only the published surface (Router, NewServeMux)
// and validates routing + middleware behavior with httptest.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package router_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/infrastructure/router"
)

func newServer(t *testing.T, configure func(r router.Router)) *httptest.Server {
	t.Helper()
	r := router.NewServeMux()
	configure(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, srv *httptest.Server, method, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do(%s %s): %v", method, path, err)
	}
	return resp
}

func bodyString(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(b)
}

func TestGETRoute(t *testing.T) {
	t.Parallel()
	srv := newServer(t, func(r router.Router) {
		r.Method("GET", "/users", func(w http.ResponseWriter, req *http.Request) {
			_, _ = io.WriteString(w, "users-list")
		})
	})
	resp := do(t, srv, "GET", "/users")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := bodyString(t, resp); got != "users-list" {
		t.Errorf("body = %q, want %q", got, "users-list")
	}
}

func TestPOSTRoute(t *testing.T) {
	t.Parallel()
	srv := newServer(t, func(r router.Router) {
		r.Method("POST", "/users", func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusCreated)
		})
	})
	resp := do(t, srv, "POST", "/users")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
}

func TestPUTRoute(t *testing.T) {
	t.Parallel()
	srv := newServer(t, func(r router.Router) {
		r.Method("PUT", "/users/{id}", func(w http.ResponseWriter, req *http.Request) {
			_, _ = io.WriteString(w, "updated:"+req.PathValue("id"))
		})
	})
	resp := do(t, srv, "PUT", "/users/42")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := bodyString(t, resp); got != "updated:42" {
		t.Errorf("body = %q, want %q", got, "updated:42")
	}
}

func TestDELETERoute(t *testing.T) {
	t.Parallel()
	srv := newServer(t, func(r router.Router) {
		r.Method("DELETE", "/users/{id}", func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	})
	resp := do(t, srv, "DELETE", "/users/7")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

func TestMiddlewareWrapsAllRoutes(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	addHeader := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			calls.Add(1)
			w.Header().Set("X-Mw", "1")
			next.ServeHTTP(w, req)
		})
	}
	srv := newServer(t, func(r router.Router) {
		r.Use(addHeader)
		r.Method("GET", "/a", func(w http.ResponseWriter, req *http.Request) {
			_, _ = io.WriteString(w, "A")
		})
		r.Method("GET", "/b", func(w http.ResponseWriter, req *http.Request) {
			_, _ = io.WriteString(w, "B")
		})
	})

	for _, path := range []string{"/a", "/b"} {
		resp := do(t, srv, "GET", path)
		if resp.Header.Get("X-Mw") != "1" {
			t.Errorf("path %s: middleware header missing", path)
		}
		resp.Body.Close()
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("middleware invocation count = %d, want 2", got)
	}
}

func TestMiddlewareOrderingOutermostFirst(t *testing.T) {
	t.Parallel()
	var order []string
	mw := func(label string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				order = append(order, "enter:"+label)
				next.ServeHTTP(w, req)
				order = append(order, "exit:"+label)
			})
		}
	}
	srv := newServer(t, func(r router.Router) {
		r.Use(mw("outer"))
		r.Use(mw("inner"))
		r.Method("GET", "/x", func(w http.ResponseWriter, req *http.Request) {
			order = append(order, "handler")
		})
	})
	resp := do(t, srv, "GET", "/x")
	resp.Body.Close()

	got := strings.Join(order, ",")
	want := "enter:outer,enter:inner,handler,exit:inner,exit:outer"
	if got != want {
		t.Errorf("middleware order = %q, want %q", got, want)
	}
}

func TestMethodMismatchReturns405(t *testing.T) {
	t.Parallel()
	srv := newServer(t, func(r router.Router) {
		r.Method("GET", "/only-get", func(w http.ResponseWriter, req *http.Request) {
			_, _ = io.WriteString(w, "ok")
		})
	})
	resp := do(t, srv, "POST", "/only-get")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestUnknownRouteReturns404(t *testing.T) {
	t.Parallel()
	srv := newServer(t, func(r router.Router) {
		r.Method("GET", "/known", func(w http.ResponseWriter, req *http.Request) {})
	})
	resp := do(t, srv, "GET", "/unknown")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
