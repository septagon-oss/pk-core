// Validates: REQ-002.
// Per: ADR-0009.
// Discipline: C-14.

package registry

// spec_test.go validates registry metadata normalization and structured
// diagnostic errors for invalid registry contracts.
//
// ADR: ADR-0005 (no silent failures), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"strings"
	"testing"
)

func TestSpecNormalize(t *testing.T) {
	spec, err := (Spec{
		ID:           " core.entities ",
		Owner:        " core ",
		Contribution: " EntityDescriptor ",
		Key:          " module/entity ",
		Description:  " Entity metadata registry ",
	}).Normalize()
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if spec.ID != "core.entities" {
		t.Fatalf("ID = %q; want core.entities", spec.ID)
	}
	if spec.Owner != OwnerCore {
		t.Fatalf("Owner = %q; want core", spec.Owner)
	}
	if spec.Contribution != "EntityDescriptor" || spec.Key != "module/entity" {
		t.Fatalf("normalized spec = %#v", spec)
	}
}

func TestSpecNormalizeDiagnostics(t *testing.T) {
	_, err := (Spec{ID: "bad id", Owner: "unknown"}).Normalize()
	var diagnostic DiagnosticError
	if err == nil || !errors.As(err, &diagnostic) {
		t.Fatalf("error type = %T; want DiagnosticError", err)
	}
	if !diagnostic.Diagnostics.HasErrors() {
		t.Fatal("diagnostics should include errors")
	}
	if got := err.Error(); !strings.Contains(got, string(CodeInvalidSpec)) {
		t.Fatalf("error = %q; want invalid_spec code", got)
	}
	if len(diagnostic.Diagnostics) < 2 {
		t.Fatalf("diagnostics = %#v; want multiple invalid spec diagnostics", diagnostic.Diagnostics)
	}
}

func TestDiagnosticString(t *testing.T) {
	diagnostic := Diagnostic{
		Severity:   SeverityError,
		Code:       CodeDuplicateKey,
		RegistryID: "core.routes",
		Key:        "users.index",
		Source:     "user_management",
		Message:    "duplicate key",
	}
	got := diagnostic.String()
	for _, want := range []string{"registry=core.routes", "key=users.index", "source=user_management", "duplicate key"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Diagnostic.String() = %q; missing %q", got, want)
		}
	}
}
