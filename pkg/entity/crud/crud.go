// Implements: REQ-002.
// Per: ADR-0029.
// Discipline: C-14.

// Package crud — crud.go owns the public surface of the generic CRUD
// handler block: the Store[T] persistence contract, the ListFilter
// query shape, the sentinel error values, the Options[T] configuration
// struct, and the Handler[T] type with its New constructor and Mount
// method. Per-verb handler methods bind request decoding, hook
// invocation, Store dispatch, response encoding, and error rendering
// into a single generic flow.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package crud

import (
	"context"
	"errors"
	"net/http"

	"github.com/septagon-oss/pk-core/pkg/infrastructure/router"
	"github.com/septagon-oss/pk-shared/pkg/apiwire"
)

// Store is the persistence contract for entities of type T. CRUD
// handlers depend only on this interface; concrete persistence (memory,
// sqlite, postgres) plugs in by implementing the five methods.
//
// Implementations SHOULD return one of the sentinel errors below (or
// values wrapping them via fmt.Errorf("%w: …", ErrXxx, …)) so the
// default ErrorRenderer can pick the right HTTP status. Any other
// error renders as 500.
type Store[T any] interface {
	Create(ctx context.Context, entity *T) error
	Get(ctx context.Context, id string) (*T, error)
	List(ctx context.Context, filter ListFilter) ([]*T, error)
	Update(ctx context.Context, id string, entity *T) error
	Delete(ctx context.Context, id string) error
}

// ListFilter is the canonical query shape passed to Store.List. The
// default parser populates it via the shared wire vocabulary
// (pk-shared/pkg/apiwire): canonical keys page/page_size/offset/sort/
// order/search plus the legacy aliases limit/desc/q. A custom
// ListFilterParser may override that.
type ListFilter struct {
	Limit    int
	Offset   int
	SortBy   string
	SortDesc bool
	Q        string // free-text query; Store implementations may ignore
}

// Sentinel errors. Stores and Before hooks return these (or wrap them)
// to drive HTTP status codes through the default ErrorRenderer.
var (
	ErrNotFound   = errors.New("crud: not found")
	ErrValidation = errors.New("crud: validation failed")
	ErrConflict   = errors.New("crud: conflict")
)

// Options configures a Handler. The zero value is valid: every field
// has a sensible default applied by New. A caller may pass at most one
// Options value to New; passing more is a programmer error and panics
// so the misuse surfaces at startup rather than at request time.
type Options[T any] struct {
	// IDExtractor pulls the path-parameter id from a request. The
	// default uses r.PathValue("id") (Go 1.22+ ServeMux pattern).
	IDExtractor func(*http.Request) string

	// Per-verb hooks. Before hooks may short-circuit the operation by
	// returning a non-nil error (which flows through ErrorRenderer);
	// After hooks fire only after the Store call returns success.
	BeforeCreate func(ctx context.Context, entity *T) error
	AfterCreate  func(ctx context.Context, entity *T)
	BeforeUpdate func(ctx context.Context, id string, entity *T) error
	AfterUpdate  func(ctx context.Context, id string, entity *T)
	BeforeDelete func(ctx context.Context, id string) error
	AfterDelete  func(ctx context.Context, id string)

	// ListFilterParser overrides default query-string parsing.
	ListFilterParser func(*http.Request) ListFilter

	// ErrorRenderer writes the HTTP response for an error returned
	// from a hook or a Store call. The default maps:
	//   ErrNotFound   → 404 {"error":"not found"}
	//   ErrValidation → 400 {"error":<err.Error()>}
	//   ErrConflict   → 409 {"error":"conflict"}
	//   otherwise     → 500 {"error":"internal error"} (err.Error()
	//                   is intentionally not leaked).
	ErrorRenderer func(w http.ResponseWriter, err error)
}

// Handler is the generic CRUD HTTP handler for entities of type T.
// It is constructed via New and bound to a router via Mount.
type Handler[T any] struct {
	store Store[T]
	opts  Options[T]
}

// New constructs a Handler bound to store. At most one Options value
// may be supplied; missing fields are filled with package defaults.
func New[T any](store Store[T], opts ...Options[T]) *Handler[T] {
	if store == nil {
		panic("crud.New: store is nil")
	}
	if len(opts) > 1 {
		panic("crud.New: at most one Options value may be supplied")
	}
	var o Options[T]
	if len(opts) == 1 {
		o = opts[0]
	}
	if o.IDExtractor == nil {
		o.IDExtractor = defaultIDExtractor
	}
	if o.ListFilterParser == nil {
		o.ListFilterParser = defaultListFilterParser
	}
	if o.ErrorRenderer == nil {
		o.ErrorRenderer = defaultErrorRenderer
	}
	return &Handler[T]{store: store, opts: o}
}

// Mount registers the five REST routes under basePath on r:
//
//	GET    {basePath}      → List
//	POST   {basePath}      → Create
//	GET    {basePath}/{id} → Get
//	PUT    {basePath}/{id} → Update
//	DELETE {basePath}/{id} → Delete
func (h *Handler[T]) Mount(r router.Router, basePath string) {
	if r == nil {
		panic("crud.Mount: router is nil")
	}
	r.Method(http.MethodGet, basePath, h.listHandler)
	r.Method(http.MethodPost, basePath, h.createHandler)
	r.Method(http.MethodGet, basePath+"/{id}", h.getHandler)
	r.Method(http.MethodPut, basePath+"/{id}", h.updateHandler)
	r.Method(http.MethodDelete, basePath+"/{id}", h.deleteHandler)
}

// listHandler implements GET {basePath}. The page of entities travels in
// the shared list envelope (REQ-021): {"data":[...],"metadata":{...}}.
func (h *Handler[T]) listHandler(w http.ResponseWriter, r *http.Request) {
	filter := h.opts.ListFilterParser(r)
	entities, err := h.store.List(r.Context(), filter)
	if err != nil {
		h.opts.ErrorRenderer(w, err)
		return
	}
	if entities == nil {
		entities = []*T{}
	}
	meta := &apiwire.ListMetadata{Page: 1, PageSize: filter.Limit}
	if filter.Limit > 0 {
		meta.Page = filter.Offset/filter.Limit + 1
	}
	writeJSON(w, http.StatusOK, apiwire.List[*T]{Data: entities, Metadata: meta})
}

// createHandler implements POST {basePath}.
func (h *Handler[T]) createHandler(w http.ResponseWriter, r *http.Request) {
	entity := new(T)
	if err := decodeJSON(r, entity); err != nil {
		h.opts.ErrorRenderer(w, wrapValidation(err))
		return
	}
	ctx := r.Context()
	if h.opts.BeforeCreate != nil {
		if err := h.opts.BeforeCreate(ctx, entity); err != nil {
			h.opts.ErrorRenderer(w, err)
			return
		}
	}
	if err := h.store.Create(ctx, entity); err != nil {
		h.opts.ErrorRenderer(w, err)
		return
	}
	if h.opts.AfterCreate != nil {
		h.opts.AfterCreate(ctx, entity)
	}
	writeJSON(w, http.StatusCreated, apiwire.Item[*T]{Data: entity})
}

// getHandler implements GET {basePath}/{id}.
func (h *Handler[T]) getHandler(w http.ResponseWriter, r *http.Request) {
	id := h.opts.IDExtractor(r)
	entity, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.opts.ErrorRenderer(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiwire.Item[*T]{Data: entity})
}

// updateHandler implements PUT {basePath}/{id}.
func (h *Handler[T]) updateHandler(w http.ResponseWriter, r *http.Request) {
	id := h.opts.IDExtractor(r)
	entity := new(T)
	if err := decodeJSON(r, entity); err != nil {
		h.opts.ErrorRenderer(w, wrapValidation(err))
		return
	}
	ctx := r.Context()
	if h.opts.BeforeUpdate != nil {
		if err := h.opts.BeforeUpdate(ctx, id, entity); err != nil {
			h.opts.ErrorRenderer(w, err)
			return
		}
	}
	if err := h.store.Update(ctx, id, entity); err != nil {
		h.opts.ErrorRenderer(w, err)
		return
	}
	if h.opts.AfterUpdate != nil {
		h.opts.AfterUpdate(ctx, id, entity)
	}
	writeJSON(w, http.StatusOK, apiwire.Item[*T]{Data: entity})
}

// deleteHandler implements DELETE {basePath}/{id}.
func (h *Handler[T]) deleteHandler(w http.ResponseWriter, r *http.Request) {
	id := h.opts.IDExtractor(r)
	ctx := r.Context()
	if h.opts.BeforeDelete != nil {
		if err := h.opts.BeforeDelete(ctx, id); err != nil {
			h.opts.ErrorRenderer(w, err)
			return
		}
	}
	if err := h.store.Delete(ctx, id); err != nil {
		h.opts.ErrorRenderer(w, err)
		return
	}
	if h.opts.AfterDelete != nil {
		h.opts.AfterDelete(ctx, id)
	}
	w.WriteHeader(http.StatusNoContent)
}
