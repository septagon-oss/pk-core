package registry

// spec.go owns registry contract metadata and structured diagnostics used by
// core and extension-layer registries.
//
// ADR: ADR-0005 (no silent failures), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"fmt"
	"strings"
)

// OwnerLayer identifies the layer that owns a registry's semantics.
type OwnerLayer string

const (
	OwnerCore    OwnerLayer = "core"
	OwnerModule  OwnerLayer = "module"
	OwnerDesign  OwnerLayer = "design"
	OwnerClient  OwnerLayer = "client"
	OwnerTools   OwnerLayer = "tools"
	OwnerRuntime OwnerLayer = "runtime"
	OwnerPro     OwnerLayer = "pro"
)

// Spec describes a registry contract without coupling core to the registry's
// domain behavior. It is intentionally metadata-only: the contribution value
// remains generic and belongs to the owning layer.
type Spec struct {
	ID           string
	Owner        OwnerLayer
	Contribution string
	Key          string
	Description  string
}

// Normalize trims human-authored fields and validates the minimum identity
// needed for diagnostics, manifests, and CLI output.
func (s Spec) Normalize() (Spec, error) {
	s.ID = strings.TrimSpace(s.ID)
	s.Owner = OwnerLayer(strings.TrimSpace(string(s.Owner)))
	s.Contribution = strings.TrimSpace(s.Contribution)
	s.Key = strings.TrimSpace(s.Key)
	s.Description = strings.TrimSpace(s.Description)

	var diagnostics Diagnostics
	if s.ID == "" {
		diagnostics = append(diagnostics, Diagnostic{
			Severity: SeverityError,
			Code:     CodeInvalidSpec,
			Message:  "registry spec ID is required",
		})
	}
	if strings.ContainsAny(s.ID, " \t\n\r") {
		diagnostics = append(diagnostics, Diagnostic{
			Severity:   SeverityError,
			Code:       CodeInvalidSpec,
			RegistryID: s.ID,
			Message:    "registry spec ID must not contain whitespace",
		})
	}
	if s.Owner == "" {
		diagnostics = append(diagnostics, Diagnostic{
			Severity:   SeverityError,
			Code:       CodeInvalidSpec,
			RegistryID: s.ID,
			Message:    "registry spec owner is required",
		})
	} else if !validOwnerLayer(s.Owner) {
		diagnostics = append(diagnostics, Diagnostic{
			Severity:   SeverityError,
			Code:       CodeInvalidSpec,
			RegistryID: s.ID,
			Message:    fmt.Sprintf("registry spec owner %q is not supported", s.Owner),
		})
	}
	if s.Contribution == "" {
		diagnostics = append(diagnostics, Diagnostic{
			Severity:   SeverityError,
			Code:       CodeInvalidSpec,
			RegistryID: s.ID,
			Message:    "registry spec contribution is required",
		})
	}
	if s.Key == "" {
		diagnostics = append(diagnostics, Diagnostic{
			Severity:   SeverityError,
			Code:       CodeInvalidSpec,
			RegistryID: s.ID,
			Message:    "registry spec key is required",
		})
	}
	if diagnostics.HasErrors() {
		return Spec{}, DiagnosticError{Diagnostics: diagnostics}
	}
	return s, nil
}

func validOwnerLayer(owner OwnerLayer) bool {
	switch owner {
	case OwnerCore, OwnerModule, OwnerDesign, OwnerClient, OwnerTools, OwnerRuntime, OwnerPro:
		return true
	default:
		return false
	}
}

// Severity classifies a registry diagnostic.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Code identifies a machine-readable registry diagnostic class.
type Code string

const (
	CodeInvalidSpec           Code = "invalid_spec"
	CodeInvalidConflictPolicy Code = "invalid_conflict_policy"
	CodeInvalidKey            Code = "invalid_key"
	CodeInvalidValue          Code = "invalid_value"
	CodeDuplicateKey          Code = "duplicate_key"
)

// Diagnostic is a structured validation message emitted by registry builders.
type Diagnostic struct {
	Severity   Severity
	Code       Code
	RegistryID string
	Key        string
	Source     string
	Message    string
}

// Diagnostics is a list of structured registry diagnostics.
type Diagnostics []Diagnostic

// HasErrors reports whether any diagnostic is an error.
func (d Diagnostics) HasErrors() bool {
	for _, diagnostic := range d {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Error implements error for Diagnostics.
func (d Diagnostics) Error() string {
	return DiagnosticError{Diagnostics: d}.Error()
}

// DiagnosticError wraps one or more structured diagnostics as an error.
type DiagnosticError struct {
	Diagnostics Diagnostics
}

// Error returns a compact human-readable diagnostic summary.
func (e DiagnosticError) Error() string {
	if len(e.Diagnostics) == 0 {
		return "registry diagnostics: no diagnostics"
	}
	if len(e.Diagnostics) == 1 {
		return e.Diagnostics[0].String()
	}
	return fmt.Sprintf("%s (+%d more)", e.Diagnostics[0].String(), len(e.Diagnostics)-1)
}

// String formats a single diagnostic for human-facing errors.
func (d Diagnostic) String() string {
	parts := make([]string, 0, 5)
	if d.RegistryID != "" {
		parts = append(parts, "registry="+d.RegistryID)
	}
	if d.Key != "" {
		parts = append(parts, "key="+d.Key)
	}
	if d.Source != "" {
		parts = append(parts, "source="+d.Source)
	}
	if d.Code != "" {
		parts = append(parts, "code="+string(d.Code))
	}
	if len(parts) == 0 {
		return d.Message
	}
	return strings.Join(parts, " ") + ": " + d.Message
}

func diagnosticError(diagnostic Diagnostic) error {
	if diagnostic.Severity == "" {
		diagnostic.Severity = SeverityError
	}
	return DiagnosticError{Diagnostics: Diagnostics{diagnostic}}
}
