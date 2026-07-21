// Implements: REQ-009.
// Per: ADR-0029.
// Discipline: C-14.

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
	"time"
)

// Status is the aggregate or per-component health state.
type Status string

// Aggregate status values.
//
// The default registrar in this package emits only StatusHealthy and
// StatusUnhealthy. StatusDegraded is reserved for custom registrars and
// adapter implementations that distinguish partial-failure conditions; the
// wire format is fixed in v0.1.0 so consumers can rely on it.
const (
	// StatusHealthy reports that all components are operating normally.
	StatusHealthy Status = "healthy"
	// StatusDegraded reports partial impairment; reserved for custom registrars.
	StatusDegraded Status = "degraded"
	// StatusUnhealthy reports that at least one component has failed.
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

// Option configures Registry behavior at registration time.
type Option func(*config)

type config struct {
	timeout time.Duration
}

// WithTimeout configures a max duration for the checker. If the checker does
// not return within the timeout, its result is reported as Unhealthy with an
// "checker timed out after <d>" error. Zero or negative duration disables the
// timeout (default).
func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

// Registrar registers health checkers and produces aggregate reports.
type Registrar interface {
	Register(name string, checker Checker, opts ...Option)
	Check(ctx context.Context) Result
	HTTPHandler() http.Handler
}
