// Implements: REQ-002.
// Per: ADR-0029.
// Discipline: C-14.

// Package crud — encoding.go owns the private JSON encode/decode and
// ID-extraction helpers, plus the default Options implementations
// (IDExtractor, ListFilterParser, ErrorRenderer). Keeping these out of
// crud.go isolates request-shape concerns from the public type surface
// and gives a single place to evolve the wire encoding.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package crud

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// jsonContentType is the response Content-Type for every JSON body the
// handler emits.
const jsonContentType = "application/json; charset=utf-8"

// writeJSON encodes v as JSON with the given status. HTML escaping is
// disabled so embedded angle brackets travel intact (the response is
// not destined for an HTML context).
func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		// Encoding a value the handler itself produced cannot recover
		// to a meaningful body; emit a minimal 500 envelope.
		w.Header().Set("Content-Type", jsonContentType)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"internal error"}`)
		return
	}
	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// decodeJSON decodes the request body into v with strict semantics:
// unknown fields are rejected so payload mistakes surface as 400 rather
// than silently drop data.
func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

// wrapValidation tags a decoding error as a validation failure so the
// default ErrorRenderer renders it as 400.
func wrapValidation(err error) error {
	return fmt.Errorf("%w: %s", ErrValidation, err.Error())
}

// defaultIDExtractor reads the {id} path parameter via Go 1.22+ stdlib
// ServeMux support.
func defaultIDExtractor(r *http.Request) string {
	return r.PathValue("id")
}

// defaultListFilterParser pulls limit/offset/sort/desc/q from the URL
// query string. Invalid integers fall back to zero; absent keys leave
// the corresponding field at its zero value.
func defaultListFilterParser(r *http.Request) ListFilter {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	desc := false
	switch q.Get("desc") {
	case "1", "true", "TRUE", "True":
		desc = true
	}
	return ListFilter{
		Limit:    limit,
		Offset:   offset,
		SortBy:   q.Get("sort"),
		SortDesc: desc,
		Q:        q.Get("q"),
	}
}

// defaultErrorRenderer maps sentinel errors to HTTP status codes. The
// 500 branch deliberately omits err.Error() from the response body so
// store-internal details do not leak across the kernel boundary.
func defaultErrorRenderer(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "conflict")
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// writeError emits a minimal JSON error envelope. It is intentionally
// schema-light so callers can override ErrorRenderer to richer shapes
// without fighting a wide default.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(status)
	body := struct {
		Error string `json:"error"`
	}{Error: msg}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}
