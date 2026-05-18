// Package database_test exercises the database wrapper from outside the
// package using a tiny stdlib-only mock driver so we exercise the full
// Open / DB / Ping / Close lifecycle without adding a real driver dep.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package database_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/infrastructure/database"
)

// --- mock driver --------------------------------------------------------
//
// mockDriver registers under the name "pkcoremockdb" via init() below.
// It supports Open (returns a mockConn), Close, and Pinger so we can
// exercise database.Ping. Prepare/Begin/Exec/Query intentionally return
// "not implemented" — the wrapper under test does not touch them.

type mockDriver struct{}

// shared global state across mockConn instances. atomic so the -race
// detector stays clean across goroutines (sql.DB pools may dial from
// any goroutine).
var (
	mockOpenCount  atomic.Int32
	mockCloseCount atomic.Int32
	mockPingErr    atomic.Pointer[error]
)

func (mockDriver) Open(name string) (driver.Conn, error) {
	mockOpenCount.Add(1)
	return &mockConn{}, nil
}

type mockConn struct {
	closed bool
}

func (m *mockConn) Prepare(query string) (driver.Stmt, error) {
	return nil, errors.New("mockdb: Prepare not implemented")
}

func (m *mockConn) Close() error {
	mockCloseCount.Add(1)
	m.closed = true
	return nil
}

func (m *mockConn) Begin() (driver.Tx, error) {
	return nil, errors.New("mockdb: Begin not implemented")
}

// Ping satisfies driver.Pinger so sql.DB.PingContext routes here.
func (m *mockConn) Ping(ctx context.Context) error {
	if errPtr := mockPingErr.Load(); errPtr != nil && *errPtr != nil {
		return *errPtr
	}
	if m.closed {
		return io.ErrClosedPipe
	}
	return nil
}

func init() {
	sql.Register("pkcoremockdb", mockDriver{})
}

// --- tests --------------------------------------------------------------

func TestOpenInvalidDriverErrors(t *testing.T) {
	t.Parallel()
	_, err := database.Open("definitely-not-a-driver", "")
	if err == nil {
		t.Fatal("Open: nil error for unregistered driver")
	}
}

func TestDBReturnsUnderlyingHandle(t *testing.T) {
	t.Parallel()
	db, err := database.Open("pkcoremockdb", "ignored")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	raw := db.DB()
	if raw == nil {
		t.Fatal("DB() returned nil")
	}
	// The returned handle should be a *sql.DB usable by the stdlib API.
	if err := raw.PingContext(context.Background()); err != nil {
		t.Errorf("raw.PingContext: %v", err)
	}
}

func TestPing(t *testing.T) {
	t.Parallel()
	db, err := database.Open("pkcoremockdb", "ignored")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Clear any prior ping override.
	mockPingErr.Store(nil)

	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestClose(t *testing.T) {
	t.Parallel()
	db, err := database.Open("pkcoremockdb", "ignored")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Force one connection to exist so Close has something to release.
	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// After Close, the underlying *sql.DB should refuse queries.
	if err := db.DB().PingContext(context.Background()); err == nil {
		t.Fatal("PingContext after Close: nil error")
	}
}

func TestOptionsApply(t *testing.T) {
	t.Parallel()
	db, err := database.Open(
		"pkcoremockdb", "ignored",
		database.WithMaxOpenConns(7),
		database.WithMaxIdleConns(3),
		database.WithConnMaxLifetime(42*time.Second),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	stats := db.DB().Stats()
	if stats.MaxOpenConnections != 7 {
		t.Errorf("MaxOpenConnections = %d, want 7", stats.MaxOpenConnections)
	}
	// MaxIdleConns and ConnMaxLifetime are not exposed via Stats; we
	// can only assert the open-conns knob propagated. Their presence
	// would require reflection, which the tightening fitness gate
	// disallows. The fact that Open() returned without error after
	// the option ran is sufficient evidence of "did not panic / did
	// not error" for the other two.
	_ = stats
}

func TestPingPropagatesDriverError(t *testing.T) {
	t.Parallel()
	db, err := database.Open("pkcoremockdb", "ignored")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		mockPingErr.Store(nil)
		_ = db.Close()
	})

	want := errors.New("ping failed")
	mockPingErr.Store(&want)
	err = db.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping: nil error when driver reports failure")
	}
}
