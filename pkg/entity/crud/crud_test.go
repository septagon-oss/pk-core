// Validates: REQ-002.
// Per: ADR-0029.
// Discipline: C-14.

// Package crud_test exercises the generic CRUD handler block from
// outside the package: it imports only the published surface (Store,
// Handler, New, Options, sentinel errors) and validates all five REST
// endpoints, hook semantics, and error mapping through httptest +
// the stdlib ServeMux Router.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package crud_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/entity/crud"
	"github.com/septagon-oss/pk-core/pkg/infrastructure/router"
	"github.com/septagon-oss/pk-shared/pkg/apiwire"
)

// Widget is the test entity. ID + Name are enough to exercise the
// encode/decode roundtrip and to give the in-memory store a key.
type Widget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// widgetStore is an in-memory Store[Widget] used by every test case.
// It honors the sentinel-error contract so the default ErrorRenderer
// can map results to the expected HTTP statuses.
type widgetStore struct {
	mu     sync.Mutex
	m      map[string]*Widget
	lastQ  string // captured List filter Q for parser tests
	lastLF crud.ListFilter
}

func newWidgetStore() *widgetStore {
	return &widgetStore{m: make(map[string]*Widget)}
}

func (s *widgetStore) Create(_ context.Context, w *Widget) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.ID == "" {
		return fmt.Errorf("%w: missing id", crud.ErrValidation)
	}
	if _, exists := s.m[w.ID]; exists {
		return fmt.Errorf("%w: duplicate %s", crud.ErrConflict, w.ID)
	}
	cp := *w
	s.m[w.ID] = &cp
	return nil
}

func (s *widgetStore) Get(_ context.Context, id string) (*Widget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.m[id]
	if !ok {
		return nil, crud.ErrNotFound
	}
	cp := *w
	return &cp, nil
}

func (s *widgetStore) List(_ context.Context, f crud.ListFilter) ([]*Widget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastQ = f.Q
	s.lastLF = f
	ids := make([]string, 0, len(s.m))
	for id := range s.m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	all := make([]*Widget, 0, len(ids))
	for _, id := range ids {
		cp := *s.m[id]
		all = append(all, &cp)
	}
	// Apply offset / limit if set so TestListWithLimitOffset has bite.
	if f.Offset > len(all) {
		return []*Widget{}, nil
	}
	all = all[f.Offset:]
	if f.Limit > 0 && f.Limit < len(all) {
		all = all[:f.Limit]
	}
	return all, nil
}

func (s *widgetStore) Update(_ context.Context, id string, w *Widget) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return crud.ErrNotFound
	}
	cp := *w
	cp.ID = id
	s.m[id] = &cp
	return nil
}

func (s *widgetStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return crud.ErrNotFound
	}
	delete(s.m, id)
	return nil
}

// newServer mounts a Handler[Widget] at /widgets on a fresh router
// behind an httptest.Server. Test cases pass in the Options they need.
func newServer(t *testing.T, store *widgetStore, opts ...crud.Options[Widget]) *httptest.Server {
	t.Helper()
	r := router.NewServeMux()
	h := crud.New[Widget](store, opts...)
	h.Mount(r, "/widgets")
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func doJSON(t *testing.T, srv *httptest.Server, method, path string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do(%s %s): %v", method, path, err)
	}
	return resp
}

func decodeBody[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestCreatePOSTReturns201(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodPost, "/widgets", Widget{ID: "w1", Name: "Gear"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	got := decodeBody[apiwire.Item[Widget]](t, resp)
	if got.Data.ID != "w1" || got.Data.Name != "Gear" {
		t.Errorf("body = %+v, want data {w1 Gear}", got)
	}
	if _, err := store.Get(context.Background(), "w1"); err != nil {
		t.Errorf("store missing w1 after create: %v", err)
	}
}

func TestCreatePOSTInvalidJSON400(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	srv := newServer(t, store)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/widgets", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCreateBeforeHookCanReject(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	srv := newServer(t, store, crud.Options[Widget]{
		BeforeCreate: func(_ context.Context, w *Widget) error {
			if w.Name == "" {
				return fmt.Errorf("%w: name required", crud.ErrValidation)
			}
			return nil
		},
	})

	resp := doJSON(t, srv, http.MethodPost, "/widgets", Widget{ID: "w1"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	if _, err := store.Get(context.Background(), "w1"); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("store should not contain rejected entity, got err=%v", err)
	}
}

func TestCreateAfterHookFires(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	var afterCalls atomic.Int32
	srv := newServer(t, store, crud.Options[Widget]{
		AfterCreate: func(_ context.Context, _ *Widget) {
			afterCalls.Add(1)
		},
	})
	resp := doJSON(t, srv, http.MethodPost, "/widgets", Widget{ID: "w1", Name: "n"})
	resp.Body.Close()
	if got := afterCalls.Load(); got != 1 {
		t.Errorf("AfterCreate calls = %d, want 1", got)
	}
}

func TestGetExistingReturns200(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	_ = store.Create(context.Background(), &Widget{ID: "w1", Name: "Alpha"})
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodGet, "/widgets/w1", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeBody[apiwire.Item[Widget]](t, resp)
	if got.Data.Name != "Alpha" {
		t.Errorf("name = %q, want Alpha", got.Data.Name)
	}
}

func TestGetNotFoundReturns404(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodGet, "/widgets/missing", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestListReturnsAllEntities(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	for _, w := range []*Widget{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}, {ID: "c", Name: "C"}} {
		_ = store.Create(context.Background(), w)
	}
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodGet, "/widgets", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeBody[apiwire.List[Widget]](t, resp)
	if len(got.Data) != 3 {
		t.Errorf("len = %d, want 3", len(got.Data))
	}
}

func TestListWithLimitOffset(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		_ = store.Create(context.Background(), &Widget{ID: id, Name: id})
	}
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodGet, "/widgets?limit=2&offset=1", nil)
	got := decodeBody[apiwire.List[Widget]](t, resp)
	if len(got.Data) != 2 {
		t.Fatalf("len = %d, want 2", len(got.Data))
	}
	if got.Data[0].ID != "b" || got.Data[1].ID != "c" {
		t.Errorf("ids = %s,%s, want b,c", got.Data[0].ID, got.Data[1].ID)
	}
}

func TestListWithCanonicalPageParams(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		_ = store.Create(context.Background(), &Widget{ID: id, Name: id})
	}
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodGet, "/widgets?page=2&page_size=2", nil)
	got := decodeBody[apiwire.List[Widget]](t, resp)
	if len(got.Data) != 2 {
		t.Fatalf("len = %d, want 2", len(got.Data))
	}
	if got.Data[0].ID != "c" || got.Data[1].ID != "d" {
		t.Errorf("ids = %s,%s, want c,d", got.Data[0].ID, got.Data[1].ID)
	}
	if got.Metadata == nil || got.Metadata.Page != 2 || got.Metadata.PageSize != 2 {
		t.Errorf("metadata = %+v, want page 2 size 2", got.Metadata)
	}
}

func TestListEmptyReturnsJSONArray(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodGet, "/widgets", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	body := strings.TrimSpace(string(b))
	// data must be [] (never null) so clients can range over it unconditionally.
	if !strings.Contains(body, `"data":[]`) {
		t.Errorf("body = %q, want a %q list envelope", body, `"data":[]`)
	}
}

func TestUpdatePUTReturns200(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	_ = store.Create(context.Background(), &Widget{ID: "w1", Name: "old"})
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodPut, "/widgets/w1", Widget{Name: "new"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	stored, _ := store.Get(context.Background(), "w1")
	if stored.Name != "new" {
		t.Errorf("name = %q, want new", stored.Name)
	}
}

func TestUpdateNotFoundReturns404(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodPut, "/widgets/missing", Widget{Name: "x"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDeleteReturns204(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	_ = store.Create(context.Background(), &Widget{ID: "w1", Name: "n"})
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodDelete, "/widgets/w1", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
	if _, err := store.Get(context.Background(), "w1"); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("entity still present after delete: %v", err)
	}
}

func TestDeleteNotFoundReturns404(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodDelete, "/widgets/missing", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestCreateConflictReturns409(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	_ = store.Create(context.Background(), &Widget{ID: "w1", Name: "first"})
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodPost, "/widgets", Widget{ID: "w1", Name: "second"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestCustomErrorRendererInvoked(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	var called atomic.Int32
	srv := newServer(t, store, crud.Options[Widget]{
		ErrorRenderer: func(w http.ResponseWriter, err error) {
			called.Add(1)
			w.Header().Set("X-Custom-Error", "1")
			w.WriteHeader(http.StatusTeapot)
			_, _ = io.WriteString(w, `{"custom":"yes"}`)
		},
	})

	resp := doJSON(t, srv, http.MethodGet, "/widgets/missing", nil)
	defer resp.Body.Close()
	if got := called.Load(); got != 1 {
		t.Errorf("custom renderer calls = %d, want 1", got)
	}
	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want 418", resp.StatusCode)
	}
	if resp.Header.Get("X-Custom-Error") != "1" {
		t.Errorf("missing custom error header")
	}
}

func TestCustomIDExtractor(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	_ = store.Create(context.Background(), &Widget{ID: "from-header", Name: "n"})
	srv := newServer(t, store, crud.Options[Widget]{
		IDExtractor: func(r *http.Request) string {
			return r.Header.Get("X-Entity-ID")
		},
	})

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/widgets/ignored", nil)
	req.Header.Set("X-Entity-ID", "from-header")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeBody[apiwire.Item[Widget]](t, resp)
	if got.Data.ID != "from-header" {
		t.Errorf("id = %q, want from-header", got.Data.ID)
	}
}

func TestListFilterParserOverride(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	_ = store.Create(context.Background(), &Widget{ID: "a", Name: "n"})
	srv := newServer(t, store, crud.Options[Widget]{
		ListFilterParser: func(r *http.Request) crud.ListFilter {
			return crud.ListFilter{Q: "custom:" + r.Header.Get("X-Q")}
		},
	})

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/widgets", nil)
	req.Header.Set("X-Q", "search-term")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if store.lastQ != "custom:search-term" {
		t.Errorf("store saw Q=%q, want custom:search-term", store.lastQ)
	}
}

func TestDefaultListFilterParserParsesQuery(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	_ = store.Create(context.Background(), &Widget{ID: "a", Name: "n"})
	srv := newServer(t, store)

	resp := doJSON(t, srv, http.MethodGet, "/widgets?limit=5&offset=2&sort=name&desc=true&q=hello", nil)
	resp.Body.Close()
	got := store.lastLF
	if got.Limit != 5 || got.Offset != 2 || got.SortBy != "name" || !got.SortDesc || got.Q != "hello" {
		t.Errorf("filter = %+v, want {Limit:5 Offset:2 SortBy:name SortDesc:true Q:hello}", got)
	}

	// The canonical dialect (REQ-021) must normalize to the same filter shape.
	resp = doJSON(t, srv, http.MethodGet, "/widgets?page=2&page_size=5&sort=name&order=desc&search=hello", nil)
	resp.Body.Close()
	got = store.lastLF
	if got.Limit != 5 || got.Offset != 5 || got.SortBy != "name" || !got.SortDesc || got.Q != "hello" {
		t.Errorf("canonical filter = %+v, want {Limit:5 Offset:5 SortBy:name SortDesc:true Q:hello}", got)
	}
}

// failingStore returns an arbitrary (non-sentinel) error from every
// method so the default ErrorRenderer's 500 branch can be exercised
// without leaking err.Error() into the response body.
type failingStore struct{ widgetStore }

func (s *failingStore) Get(context.Context, string) (*Widget, error) {
	return nil, errors.New("boom internal detail")
}

func TestUnknownStoreErrorReturns500(t *testing.T) {
	t.Parallel()
	store := &failingStore{widgetStore: *newWidgetStore()}
	r := router.NewServeMux()
	crud.New[Widget](store).Mount(r, "/widgets")
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp := doJSON(t, srv, http.MethodGet, "/widgets/anything", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(b), "boom internal detail") {
		t.Errorf("500 body leaked store error detail: %q", string(b))
	}
}

func TestBeforeUpdateHookCanReject(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	_ = store.Create(context.Background(), &Widget{ID: "w1", Name: "old"})
	srv := newServer(t, store, crud.Options[Widget]{
		BeforeUpdate: func(_ context.Context, _ string, _ *Widget) error {
			return fmt.Errorf("%w: locked", crud.ErrValidation)
		},
	})
	resp := doJSON(t, srv, http.MethodPut, "/widgets/w1", Widget{Name: "x"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	stored, _ := store.Get(context.Background(), "w1")
	if stored.Name != "old" {
		t.Errorf("name = %q, want old (rejected update applied)", stored.Name)
	}
}

func TestBeforeDeleteHookCanReject(t *testing.T) {
	t.Parallel()
	store := newWidgetStore()
	_ = store.Create(context.Background(), &Widget{ID: "w1", Name: "n"})
	srv := newServer(t, store, crud.Options[Widget]{
		BeforeDelete: func(_ context.Context, _ string) error {
			return fmt.Errorf("%w: protected", crud.ErrConflict)
		},
	})
	resp := doJSON(t, srv, http.MethodDelete, "/widgets/w1", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
	if _, err := store.Get(context.Background(), "w1"); err != nil {
		t.Errorf("entity should still exist: %v", err)
	}
}
