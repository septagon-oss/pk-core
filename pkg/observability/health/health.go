// Package health defines PlatformKit's provider-neutral health-check contract.
//
// health.go owns the public types: Status, Checker, ComponentResult, Result,
// Registrar. The default implementation lives in registry.go.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package health

import (
	"context"
	"net/http"
)

// Status is the aggregate or per-component health state.
type Status string

// Aggregate status values.
//
// The default registrar in this package emits only StatusHealthy and
// StatusUnhealthy. StatusDegraded is reserved for custom registrars and
// adapter implementations that distinguish partial-failure conditions; the
// wire format is fixed in v0.0.0 so consumers can rely on it.
const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// Checker is a single health probe.
type Checker interface {
	Check(ctx context.Context) error
}

// CheckerFunc adapts an ordinary function to the Checker interface.
type CheckerFunc func(ctx context.Context) error

// Check satisfies Checker.
func (f CheckerFunc) Check(ctx context.Context) error { return f(ctx) }

// ComponentResult is the outcome of a single component's health check.
type ComponentResult struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Result is the aggregate health report.
type Result struct {
	Status     Status            `json:"status"`
	Components []ComponentResult `json:"components"`
}

// Registrar registers health checkers and produces aggregate reports.
type Registrar interface {
	Register(name string, checker Checker)
	Check(ctx context.Context) Result
	HTTPHandler() http.Handler
}
