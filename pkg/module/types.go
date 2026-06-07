// Package module provides the minimal public PlatformKit module composition
// framework.
package module

// types.go owns the embeddable OSS module type and typed port vocabulary that
// downstream modules extend without changing the core architectural model.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-10 (shared builders return errors), C-14 (file purpose declaration).

import (
	"fmt"
	"reflect"
	"slices"
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
	// Name is the stable package-qualified Go interface name.
	Name string

	// Version is interpreted by context: provided ports use a concrete
	// producer version, dependency ports use a version constraint.
	Version string
}

// PortOf returns the stable public port name for interface T.
func PortOf[T any](version string) Port {
	return Port{Name: portNameOf[T]("module.PortOf"), Version: strings.TrimSpace(version)}
}

func portNameOf[T any](caller string) string {
	t := reflect.TypeFor[*T]()
	elem := t.Elem()
	if elem.Kind() != reflect.Interface {
		panic(fmt.Sprintf("%s[%s]: T must be an interface", caller, elem.String()))
	}
	if elem.PkgPath() == "" || elem.Name() == "" {
		panic(fmt.Sprintf("%s[%s]: T must be a named interface", caller, elem.String()))
	}
	return elem.PkgPath() + "." + elem.Name()
}

// Dependency declares that a module consumes a port.
type Dependency struct {
	Port              Port               `json:"port"`
	Required          bool               `json:"required"`
	Purpose           string             `json:"purpose,omitempty"`
	Category          DependencyCategory `json:"category,omitempty"`
	SubCategory       string             `json:"sub_category,omitempty"`
	PreferredProvider string             `json:"preferred_provider,omitempty"`
	FallbackProviders []string           `json:"fallback_providers,omitempty"`
}

// Provide declares that a module provides interface T. It intentionally only
// records the human-authored version string; Compose/Validate surface malformed
// versions as ordinary errors at the composition boundary.
func Provide[T any](version string) Port {
	return Port{Name: portNameOf[T]("module.Provide"), Version: strings.TrimSpace(version)}
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
//
// Panics if New returns an error (for example, metadata with an empty ID or an
// ID containing whitespace). Use New to handle invalid metadata as an error.
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
		m.dependencies = append(m.dependencies, copyDependencies(deps)...)
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

// Metadata returns the module's identifying metadata.
func (m *Module) Metadata() Metadata {
	return m.metadata
}

// Dependencies returns a copy of the module's declared port dependencies.
func (m *Module) Dependencies() []Dependency {
	return copyDependencies(m.dependencies)
}

// Provides returns a copy of the ports the module provides.
func (m *Module) Provides() []Port {
	return slices.Clone(m.provides)
}

// Providers returns a copy of the module's dependency-injection provider values.
func (m *Module) Providers() []any {
	return slices.Clone(m.providers)
}

// Invocations returns a copy of the module's dependency-injection invocation values.
func (m *Module) Invocations() []any {
	return slices.Clone(m.invocations)
}

func copyDependencies(deps []Dependency) []Dependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]Dependency, len(deps))
	for i, dep := range deps {
		out[i] = dep
		out[i].FallbackProviders = slices.Clone(dep.FallbackProviders)
	}
	return out
}
