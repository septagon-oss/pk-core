// Validates: REQ-002.
// Per: ADR-0009.
// Discipline: C-14.

package architecture_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/authz"
	"github.com/septagon-oss/pk-core/pkg/entity"
	"github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/mutation"
	"github.com/septagon-oss/pk-core/pkg/registry"
)

type auditSink interface {
	Record(ctx context.Context, event string) error
}

type billingModule struct {
	module.Module
	provider string
}

func TestOpenCoreExtensibilityFitness(t *testing.T) {
	t.Parallel()

	audit := module.NewBundle("oss.audit", []module.Entry{
		{
			ID: "audit",
			New: func() module.Composable {
				return module.Must(
					module.Metadata{ID: "audit", Name: "Audit"},
					module.WithProvides(module.Provide[auditSink]("1.0.0")),
				)
			},
		},
	}, []string{"audit"})
	billing := module.NewBundle("pro.billing", []module.Entry{
		{
			ID: "billing",
			New: func() module.Composable {
				base := module.Must(
					module.Metadata{ID: "billing", Name: "Billing"},
					module.WithDependencies(module.RequiresPort[auditSink](module.PortSpec{
						Version:           "^1.0.0",
						Purpose:           "audit invoice lifecycle events",
						PreferredProvider: "audit",
					})),
				)
				return &billingModule{Module: *base, provider: "stripe"}
			},
		},
	}, []string{"billing"})

	catalog := module.NewCatalog().Add(billing).Add(audit).MustBuild()
	plan, err := module.Compose(catalog)
	if err != nil {
		t.Fatalf("Compose error = %v", err)
	}
	if got := composedIDs(plan.Modules); !slices.Equal(got, []string{"audit", "billing"}) {
		t.Fatalf("module order = %v; want dependency order", got)
	}

	descriptorCatalog, err := entity.NewCatalog(entity.Descriptor{
		ModuleID:     "billing",
		Entity:       "Invoice",
		TenantScoped: true,
		Fields: []entity.Field{
			{Name: "amount", Type: "x.money", Required: true, Metadata: map[string]string{"currency": "eur"}},
			{Name: "status", Type: entity.FieldEnum, EnumValues: []string{"draft", "paid"}},
		},
		Policy: entity.Policy{
			Read:  []string{"billing.invoice.read"},
			Write: []string{"billing.invoice.write"},
		},
	})
	if err != nil {
		t.Fatalf("entity.NewCatalog error = %v", err)
	}
	invoice, ok := descriptorCatalog.Lookup(entity.Key{ModuleID: "billing", Entity: "Invoice"})
	if !ok {
		t.Fatal("expected billing invoice descriptor")
	}
	if got := invoice.RequiredPolicyTokens(); !slices.Equal(got, []string{"billing.invoice.read", "billing.invoice.write"}) {
		t.Fatalf("policy tokens = %v", got)
	}

	policies, err := authz.NewPolicyEvaluator(authz.Policy{
		ID:        "billing-read",
		ModuleID:  "billing",
		Effect:    authz.EffectAllow,
		Actions:   []string{"read"},
		Resources: []string{"Invoice"},
		Roles:     []string{"billing_admin"},
	})
	if err != nil {
		t.Fatalf("authz.NewPolicyEvaluator error = %v", err)
	}
	result, err := policies.Evaluate(context.Background(), authz.Request{
		Principal: authz.Principal{Roles: []string{"billing_admin"}},
		Action:    "read",
		Resource:  authz.Resource{Type: "Invoice", ModuleID: "billing"},
	})
	if err != nil {
		t.Fatalf("PolicyEvaluator error = %v", err)
	}
	if result.Decision != authz.DecisionAllow {
		t.Fatalf("authz decision = %q; want allow", result.Decision)
	}
	result, err = policies.Evaluate(context.Background(), authz.Request{
		Principal: authz.Principal{Roles: []string{"billing_admin"}},
		Action:    "read",
		Resource:  authz.Resource{Type: "Invoice", ModuleID: "commerce"},
	})
	if err != nil {
		t.Fatalf("PolicyEvaluator cross-module error = %v", err)
	}
	if result.Decision != authz.DecisionAbstain {
		t.Fatalf("cross-module decision = %q; want abstain", result.Decision)
	}

	gate, err := mutation.NewRuleGateWithOptions([]mutation.Rule{
		{
			ID:          "invoice-payment-review",
			ModuleID:    "billing",
			Entity:      "Invoice",
			Operations:  []mutation.Operation{mutation.OperationUpdate},
			ChangeTypes: []string{"Invoice.payment_terms"},
			Decision:    mutation.GateRequiresReview,
		},
	}, mutation.WithDefaultDecision(mutation.GateDeny))
	if err != nil {
		t.Fatalf("mutation.NewRuleGateWithOptions error = %v", err)
	}
	gateResult, err := gate.EvaluateMutation(context.Background(), mutation.Intent{
		ModuleID:   "billing",
		Entity:     "Invoice",
		EntityID:   "invoice-1",
		Operation:  mutation.OperationUpdate,
		ChangeType: "Invoice.payment_terms",
		TenantID:   "tenant-1",
		Actor:      authz.Principal{ID: "user-1", TenantID: "tenant-1"},
	})
	if err != nil {
		t.Fatalf("EvaluateMutation error = %v", err)
	}
	if gateResult.Decision != mutation.GateRequiresReview {
		t.Fatalf("mutation decision = %q; want requires_review", gateResult.Decision)
	}

	_, err = registry.NewCatalogBuilder[string, string](
		registry.WithCatalogSpec[string, string](registry.Spec{
			ID:           "pro.surface.widgets",
			Owner:        registry.OwnerPro,
			Contribution: "Widget",
			Key:          "widget ID",
		}),
	).Add("invoice.summary", "oss", "oss.widgets").
		Add("invoice.summary", "pro", "pro.widgets").
		Build()
	var diagnostic registry.DiagnosticError
	if !errors.As(err, &diagnostic) || diagnostic.Diagnostics[0].Code != registry.CodeDuplicateKey {
		t.Fatalf("duplicate extension contribution error = %v; want structured duplicate diagnostic", err)
	}
}

func composedIDs(modules []module.Composable) []string {
	ids := make([]string, 0, len(modules))
	for _, module := range modules {
		ids = append(ids, module.Metadata().ID)
	}
	return ids
}
