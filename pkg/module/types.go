// Package module provides the minimal public PlatformKit module composition
// framework.
package module

import (
	"fmt"
	"reflect"
	"strings"
)

// Metadata identifies a module in catalogs, logs, and generated docs.
type Metadata struct {
	ID          string
	Name        string
	Description string
	Version     string
}

// Normalize fills small metadata defaults and validates the stable module ID.
func (m Metadata) Normalize() (Metadata, error) {
	m.ID = strings.TrimSpace(m.ID)
	if m.ID == "" {
		return Metadata{}, fmt.Errorf("module metadata ID is required")
	}
	if strings.ContainsAny(m.ID, " \t\n\r") {
		return Metadata{}, fmt.Errorf("module metadata ID %q must not contain whitespace", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		m.Name = m.ID
	}
	if strings.TrimSpace(m.Version) == "" {
		m.Version = "0.0.0"
	}
	return m, nil
}

// Port names a typed contract that one module provides and another module
// consumes. Name is derived from a Go interface type by PortOf.
type Port struct {
	Name    string
	Version string
}

// PortOf returns the stable public port name for interface T.
func PortOf[T any](version string) Port {
	var zero *T
	t := reflect.TypeOf(zero)
	if t == nil {
		panic("module.PortOf: unresolved type parameter")
	}
	elem := t.Elem()
	if elem.Kind() != reflect.Interface {
		panic(fmt.Sprintf("module.PortOf[%s]: T must be an interface", elem.String()))
	}
	return Port{Name: elem.PkgPath() + "." + elem.Name(), Version: strings.TrimSpace(version)}
}

// Dependency declares that a module consumes a port.
type Dependency struct {
	Port              Port
	Required          bool
	Purpose           string
	PreferredProvider string
}

// DependencySpec is the human-authored part of a dependency declaration.
type DependencySpec struct {
	Version           string
	Required          bool
	Purpose           string
	PreferredProvider string
}

// Require declares a required dependency on interface T.
func Require[T any](spec DependencySpec) Dependency {
	spec.Required = true
	return dependencyOf[T](spec)
}

// Optional declares an optional dependency on interface T.
func Optional[T any](spec DependencySpec) Dependency {
	spec.Required = false
	return dependencyOf[T](spec)
}

func dependencyOf[T any](spec DependencySpec) Dependency {
	return Dependency{
		Port:              PortOf[T](spec.Version),
		Required:          spec.Required,
		Purpose:           strings.TrimSpace(spec.Purpose),
		PreferredProvider: strings.TrimSpace(spec.PreferredProvider),
	}
}

// Provide declares that a module provides interface T.
func Provide[T any](version string) Port {
	return PortOf[T](version)
}

// Composable is the minimal read-only contract for a PlatformKit module.
//
// Providers and Invocations are intentionally typed as any so the OSS seed does
// not force a dependency injection implementation. Adapters can translate this
// contract to Fx, Wire, Dig, or a custom container.
type Composable interface {
	Metadata() Metadata
	Dependencies() []Dependency
	Provides() []Port
	Providers() []any
	Invocations() []any
}

// Module is the concrete base module. Pro and downstream modules can embed it
// and add their own fields or methods while still satisfying Composable:
//
//	type BillingModule struct {
//	    module.Module
//	    StripeAccount string
//	}
type Module struct {
	metadata     Metadata
	dependencies []Dependency
	provides     []Port
	providers    []any
	invocations  []any
}

// New constructs a Module.
func New(metadata Metadata, opts ...Option) (*Module, error) {
	normalized, err := metadata.Normalize()
	if err != nil {
		return nil, err
	}
	m := &Module{metadata: normalized}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	return m, nil
}

// Must constructs a Module and panics on invalid metadata.
func Must(metadata Metadata, opts ...Option) *Module {
	m, err := New(metadata, opts...)
	if err != nil {
		panic(err)
	}
	return m
}

// Option configures a Module.
type Option func(*Module)

// WithDependencies appends dependency declarations.
func WithDependencies(deps ...Dependency) Option {
	return func(m *Module) {
		m.dependencies = append(m.dependencies, deps...)
	}
}

// WithProvides appends provided port declarations.
func WithProvides(ports ...Port) Option {
	return func(m *Module) {
		m.provides = append(m.provides, ports...)
	}
}

// WithProviders appends dependency-injection provider values.
func WithProviders(providers ...any) Option {
	return func(m *Module) {
		m.providers = append(m.providers, providers...)
	}
}

// WithInvocations appends dependency-injection invocation values.
func WithInvocations(invocations ...any) Option {
	return func(m *Module) {
		m.invocations = append(m.invocations, invocations...)
	}
}

func (m *Module) Metadata() Metadata {
	return m.metadata
}

func (m *Module) Dependencies() []Dependency {
	return append([]Dependency(nil), m.dependencies...)
}

func (m *Module) Provides() []Port {
	return append([]Port(nil), m.provides...)
}

func (m *Module) Providers() []any {
	return append([]any(nil), m.providers...)
}

func (m *Module) Invocations() []any {
	return append([]any(nil), m.invocations...)
}
