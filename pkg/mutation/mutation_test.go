// Validates: REQ-002.
// Per: ADR-0029.
// Discipline: C-14.

package mutation

// mutation_test.go validates mutation intent normalization, rule-gate
// precedence, context cancellation, and catalog diagnostics.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/authz"
	"github.com/septagon-oss/pk-core/pkg/registry"
)

func validIntent() Intent {
	return Intent{
		ModuleID:   "tenant_management",
		Entity:     "Tenant",
		EntityID:   "tenant-1",
		Operation:  OperationUpdate,
		ChangeType: "Tenant.security",
		Reason:     "rotate policy",
	}
}

func TestIntentNormalize(t *testing.T) {
	intent, err := (Intent{
		ModuleID:  " tenant_management ",
		Entity:    " Tenant ",
		Operation: OperationUpdate,
	}).Normalize()
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if intent.ChangeType != "update" {
		t.Fatalf("ChangeType = %q; want update default", intent.ChangeType)
	}
	if intent.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should default")
	}
}

func TestIntentNormalizeRejectsInvalid(t *testing.T) {
	tests := []Intent{
		{},
		{ModuleID: "m"},
		{ModuleID: "m", Entity: "E"},
		{ModuleID: "m", Entity: "E", Operation: "merge"},
		{ModuleID: "bad module", Entity: "E", Operation: OperationUpdate},
		{ModuleID: "m", Entity: "Bad Entity", Operation: OperationUpdate},
		{ModuleID: "m", Entity: "E", Operation: OperationUpdate, TenantID: "tenant-a", Actor: authz.Principal{TenantID: "tenant-b"}},
	}
	for _, intent := range tests {
		if _, err := intent.Normalize(); err == nil {
			t.Fatalf("Normalize(%#v) should fail", intent)
		}
	}
}

func TestRuleGatePriority(t *testing.T) {
	gate, err := NewRuleGate(
		Rule{ID: "allow", ModuleID: "tenant_management", Entity: "Tenant", Operations: []Operation{OperationUpdate}, Decision: GateAllow},
		Rule{ID: "review", ModuleID: "tenant_management", Entity: "Tenant", Operations: []Operation{OperationUpdate}, ChangeTypes: []string{"Tenant.security"}, Decision: GateRequiresReview},
		Rule{ID: "deny", ModuleID: "tenant_management", Entity: "Tenant", Operations: []Operation{OperationDelete}, Decision: GateDeny},
	)
	if err != nil {
		t.Fatalf("NewRuleGate error = %v", err)
	}

	result, err := gate.EvaluateMutation(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("EvaluateMutation error = %v", err)
	}
	if result.Decision != GateRequiresReview || result.RuleID != "review" {
		t.Fatalf("result = %#v; want review", result)
	}

	deleteIntent := validIntent()
	deleteIntent.Operation = OperationDelete
	deleteIntent.ChangeType = "Tenant.delete"
	result, err = gate.EvaluateMutation(context.Background(), deleteIntent)
	if err != nil {
		t.Fatalf("EvaluateMutation error = %v", err)
	}
	if result.Decision != GateDeny || result.RuleID != "deny" {
		t.Fatalf("result = %#v; want deny", result)
	}
}

func TestRuleGateDefaultsAllow(t *testing.T) {
	gate, err := NewRuleGate()
	if err != nil {
		t.Fatalf("NewRuleGate error = %v", err)
	}
	result, err := gate.EvaluateMutation(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("EvaluateMutation error = %v", err)
	}
	if result.Decision != GateAllow {
		t.Fatalf("Decision = %q; want allow", result.Decision)
	}
}

func TestRuleGateCanFailClosedWhenConfigured(t *testing.T) {
	gate, err := NewRuleGateWithOptions(nil, WithDefaultDecision(GateRequiresReview))
	if err != nil {
		t.Fatalf("NewRuleGateWithOptions error = %v", err)
	}
	result, err := gate.EvaluateMutation(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("EvaluateMutation error = %v", err)
	}
	if result.Decision != GateRequiresReview {
		t.Fatalf("Decision = %q; want requires_review", result.Decision)
	}

	_, err = NewRuleGateWithOptions(nil, WithDefaultDecision("maybe"))
	if err == nil || !strings.Contains(err.Error(), "default decision") {
		t.Fatalf("NewRuleGateWithOptions error = %v; want default decision validation", err)
	}
}

func TestRuleGateRespectsContextCancellation(t *testing.T) {
	gate, err := NewRuleGate()
	if err != nil {
		t.Fatalf("NewRuleGate error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gate.EvaluateMutation(ctx, validIntent()); !errors.Is(err, context.Canceled) {
		t.Fatalf("EvaluateMutation error = %v; want context.Canceled", err)
	}
}

func TestRuleNormalizeRejectsInvalid(t *testing.T) {
	tests := []Rule{
		{},
		{ID: "r", Decision: GateAllow, Operations: []Operation{OperationUpdate}, Entity: "Tenant"},
		{ID: "r", ModuleID: "m", Decision: GateAllow, Operations: []Operation{OperationUpdate}},
		{ID: "r", ModuleID: "m", Entity: "Tenant", Decision: GateAllow},
		{ID: "r", ModuleID: "m", Entity: "Tenant", Decision: GateAllow, Operations: []Operation{"merge"}},
		{ID: "r", ModuleID: "m", Entity: "Tenant", Decision: "maybe", Operations: []Operation{OperationUpdate}},
		{ID: "bad rule", ModuleID: "m", Entity: "Tenant", Decision: GateAllow, Operations: []Operation{OperationUpdate}},
		{ID: "r", ModuleID: "bad module", Entity: "Tenant", Decision: GateAllow, Operations: []Operation{OperationUpdate}},
		{ID: "r", ModuleID: "m", Entity: "Bad Entity", Decision: GateAllow, Operations: []Operation{OperationUpdate}},
	}
	for _, rule := range tests {
		if _, err := rule.Normalize(); err == nil {
			t.Fatalf("Normalize(%#v) should fail", rule)
		}
	}
}

func TestRuleMatchNormalizesAndRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	rule := Rule{
		ID:          " review ",
		ModuleID:    " tenant_management ",
		Entity:      " Tenant ",
		Operations:  []Operation{" update "},
		ChangeTypes: []string{" Tenant.security "},
		Decision:    GateRequiresReview,
	}
	intent := Intent{
		ModuleID:   " tenant_management ",
		Entity:     " Tenant ",
		Operation:  " update ",
		ChangeType: " Tenant.security ",
	}
	if !rule.Match(intent) {
		t.Fatal("Rule.Match should normalize direct rule and intent inputs")
	}
	if (Rule{ID: "bad", ModuleID: "m", Entity: "E", Decision: "maybe", Operations: []Operation{OperationUpdate}}).Match(intent) {
		t.Fatal("Rule.Match should reject invalid rules")
	}
	if rule.Match(Intent{ModuleID: "m", Entity: "E"}) {
		t.Fatal("Rule.Match should reject invalid intents")
	}
}

func TestNewCatalog(t *testing.T) {
	rule := Rule{ID: "review-security", ModuleID: "tenant_management", Entity: "Tenant", Operations: []Operation{OperationUpdate}, ChangeTypes: []string{"Tenant.security"}, Decision: GateRequiresReview}
	catalog, err := NewCatalog(rule)
	if err != nil {
		t.Fatalf("NewCatalog error = %v", err)
	}
	spec, ok := catalog.Spec()
	if !ok {
		t.Fatal("mutation catalog should expose spec")
	}
	if spec.ID != "core.mutation_rules" || spec.Owner != registry.OwnerCore {
		t.Fatalf("spec = %#v", spec)
	}
	got, ok := catalog.Lookup(Key{ModuleID: "tenant_management", Entity: "Tenant", RuleID: "review-security"})
	if !ok {
		t.Fatal("expected mutation rule")
	}
	if got.Decision != GateRequiresReview {
		t.Fatalf("Decision = %q; want review", got.Decision)
	}

	_, err = NewCatalog(rule, rule)
	if err == nil {
		t.Fatal("duplicate mutation rules should fail")
	}
}
