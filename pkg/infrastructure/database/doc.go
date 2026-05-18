// Package database wraps *sql.DB behind a Driver-agnostic contract so
// OSS callers can construct, ping, and close a database the same way
// across sqlite, postgres, mysql, and any future driver.
//
// # Why this package exists
//
// Modules need a small, common entry point for SQL storage that does not
// bind the kernel to a specific driver. database/sql already abstracts
// drivers, but every caller still has to repeat the pool-tuning recipe
// (MaxOpenConns, MaxIdleConns, ConnMaxLifetime) and the lifecycle
// boilerplate (Ping on startup, Close on shutdown). This package owns
// that recipe behind a 3-method interface so wiring code stays one line:
// `db, err := database.Open(driver, dsn, database.WithMaxOpenConns(25))`.
//
// # Block scorecard
//
//   - Identity:        github.com/septagon-oss/pk-core/pkg/infrastructure/database
//   - Boundary:        Database is a 3-method interface; the OSS default
//     wraps *sql.DB and is returned through the interface so
//     callers code against the contract.
//   - Contract:        Database { DB, Ping, Close } + Open constructor +
//     functional options (WithMaxOpenConns, WithMaxIdleConns,
//     WithConnMaxLifetime).
//   - Composition:     Database wrappers stack — middleware can wrap a
//     Database to add metrics, tracing, or read replicas
//     without changing the contract.
//   - Replacement:     Any type implementing Database is a drop-in.
//   - Extension:       Pro / downstream adapters publish their own
//     Database constructors (sharded, multi-region, etc.).
//   - Runtime binding: Application composition picks the driver + DSN at
//     startup; this package never imports a concrete driver.
//   - Evidence:        database_test.go covers invalid-driver errors, DB
//     pass-through, Ping success, Close, and verifies the
//     three pool options propagate to the underlying *sql.DB.
//
// # Chainable laws
//
//   - Identity:        a no-op wrapper that delegates to an inner Database
//     is a valid composition element.
//   - Type compat:     every Database exposes the same DB / Ping / Close
//     shape so wrappers compose without adapters.
//   - Context preserve: Ping accepts a ctx so middleware can propagate
//     deadlines and cancellation; DB() returns *sql.DB so
//     query-level context flows through stdlib directly.
//   - Error algebra:   Open returns an error if sql.Open fails or if Ping
//     fails during startup probe.
//   - Cancellation:    Ping honors ctx.Done() via sql.DB.PingContext.
//
// # External dependencies
//
// None. The contract and OSS default are pure stdlib so the kernel stays
// audit-friendly. Callers MUST register a driver via blank-import
// (e.g., `_ "modernc.org/sqlite"`) before calling Open.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package database
