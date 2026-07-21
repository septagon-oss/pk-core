// Implements: REQ-002.
// Per: ADR-0029.
// Discipline: C-14.

// Package crud provides a generic, stdlib-only HTTP CRUD handler block
// for entities of any type T. A Handler is parameterized by a Store[T]
// implementation and mounted on a router.Router; the five canonical REST
// routes (list, create, get, update, delete) are auto-wired with JSON
// encoding on top of the Go 1.22+ ServeMux pattern syntax.
//
// # Why this package exists
//
// Every business module exposes a roughly identical set of REST endpoints
// over its primary entities: list, create, get-by-id, update, delete.
// Hand-rolling those five endpoints per entity costs hundreds of lines
// per module and forks subtly across teams. This package collapses the
// common shape into one generic handler so each module supplies only a
// typed Store[T] (the persistence contract) and optional hooks; the
// handler owns request decoding, response encoding, error mapping, and
// route registration through the kernel's neutral Router contract.
//
// # Block scorecard
//
//   - Identity:        github.com/septagon-oss/pk-core/pkg/entity/crud
//   - Boundary:        Store[T] is the only exported interface a module
//     implements; encoding helpers, default option
//     fillers, and route registration internals stay
//     private to this package.
//   - Contract:        Store[T] + Handler[T] + Options[T] + sentinel
//     errors (ErrNotFound, ErrValidation, ErrConflict).
//   - Composition:     Many Handler[T] instances coexist on a single
//     Router; each owns its own base path and entity
//     type. Hooks compose via Options.
//   - Replacement:     Swap Store[T] (in-memory, sqlite, postgres) with
//     zero handler changes; swap ErrorRenderer to render
//     module-specific error envelopes.
//   - Extension:       Options exposes Before*/After* hooks per verb,
//     an ID extractor, a list filter parser, and an
//     error renderer.
//   - Runtime binding: App composition wires concrete stores and mounts
//     handlers: crud.New[*User](store).Mount(r, "/api/v1/users").
//   - Evidence:        crud_test.go exercises all five endpoints, every
//     hook variant, custom ID extractor, custom list
//     filter parser, custom error renderer, and the
//     three sentinel error → status mappings.
//
// # Chainable laws
//
//   - Identity:        a nil hook is a valid composition element; the
//     handler treats unset Options fields as no-ops.
//   - Type compat:     Store and Handler are typed by T; the encoding
//     path is parameterized so any JSON-serializable T
//     plugs in without reflection beyond encoding/json.
//   - Context preserve: r.Context() flows unchanged from each handler
//     into the Store call and the After hook.
//   - Error algebra:   Store and Before hook errors classify via
//     errors.Is into 404/400/409/500 by default; the
//     ErrorRenderer option overrides totally.
//   - Cancellation:    request ctx cancellation aborts in-flight Store
//     ops the same way it would in any stdlib handler.
//
// # External dependencies
//
// None. The package is pure stdlib; encoding/json, errors, net/http,
// net/url, strconv, and the pk-core router contract are all that is
// required.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package crud
