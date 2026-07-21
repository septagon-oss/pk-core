// Implements: REQ-002.
// Per: ADR-0029.
// Discipline: C-14.

// Package database — database.go owns the public surface of the
// database wrapper: the Database interface, the Open constructor, the
// Option type, and the pool-tuning option constructors.
//
// The OSS default implementation is also defined here (sqlDatabase) as
// a thin wrapper around *sql.DB; Pro adapters in downstream modules
// satisfy Database without importing this file.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Database wraps *sql.DB behind a Driver-agnostic boundary.
//
// Implementations MUST be safe for concurrent use (the underlying
// *sql.DB is). DB() exposes the raw handle for query-time use; Ping
// probes connectivity; Close releases pooled connections.
type Database interface {
	// DB returns the underlying *sql.DB. Callers use it for queries,
	// transactions, and prepared statements through the standard
	// database/sql API.
	DB() *sql.DB

	// Ping verifies the database is reachable. It is typically called
	// at application startup as a fail-fast probe.
	Ping(ctx context.Context) error

	// Close releases pooled connections. After Close, DB().Query and
	// other operations return errors from database/sql.
	Close() error
}

// Option configures the connection pool at construction time.
type Option func(*config)

// config holds the resolved Options.
type config struct {
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration

	// Track which knobs were explicitly set so we only call the
	// corresponding *sql.DB setter when the caller asked us to.
	maxOpenConnsSet    bool
	maxIdleConnsSet    bool
	connMaxLifetimeSet bool
}

// WithMaxOpenConns caps the number of open connections to the database.
// Mirrors sql.DB.SetMaxOpenConns. A value <= 0 means unlimited.
func WithMaxOpenConns(n int) Option {
	return func(c *config) {
		c.maxOpenConns = n
		c.maxOpenConnsSet = true
	}
}

// WithMaxIdleConns caps the size of the idle-connection pool. Mirrors
// sql.DB.SetMaxIdleConns. A value <= 0 means no idle connections.
func WithMaxIdleConns(n int) Option {
	return func(c *config) {
		c.maxIdleConns = n
		c.maxIdleConnsSet = true
	}
}

// WithConnMaxLifetime sets the maximum lifetime of a connection.
// Mirrors sql.DB.SetConnMaxLifetime. A zero or negative value means
// connections live forever (the database/sql default).
func WithConnMaxLifetime(d time.Duration) Option {
	return func(c *config) {
		c.connMaxLifetime = d
		c.connMaxLifetimeSet = true
	}
}

// Open returns a Database backed by sql.Open(driverName, dsn) with the
// supplied pool options applied. The driver MUST be registered via
// blank-import before Open is called.
//
// Open does NOT Ping the database; callers decide whether to probe at
// startup. This keeps Open trivially mockable and avoids hiding a
// network round-trip behind a constructor.
func Open(driverName, dsn string, opts ...Option) (Database, error) {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("database.Open: sql.Open(%q): %w", driverName, err)
	}

	if cfg.maxOpenConnsSet {
		db.SetMaxOpenConns(cfg.maxOpenConns)
	}
	if cfg.maxIdleConnsSet {
		db.SetMaxIdleConns(cfg.maxIdleConns)
	}
	if cfg.connMaxLifetimeSet {
		db.SetConnMaxLifetime(cfg.connMaxLifetime)
	}

	return &sqlDatabase{db: db}, nil
}

// sqlDatabase is the OSS default implementation: a thin wrapper around
// *sql.DB that satisfies Database.
type sqlDatabase struct {
	db *sql.DB
}

// DB returns the underlying *sql.DB.
func (s *sqlDatabase) DB() *sql.DB { return s.db }

// Ping probes the database via sql.DB.PingContext.
func (s *sqlDatabase) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Close releases pooled connections.
func (s *sqlDatabase) Close() error { return s.db.Close() }
