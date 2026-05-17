package authz

// authz_test.go validates policy normalization, policy decisions, aggregate
// evaluator precedence, and context cancellation behavior.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPolicyNormalize(t *testing.T) {
	policy, err := (Policy{
		ID:        " read-users ",
		ModuleID:  " user_management ",
		Effect:    " allow ",
		Actions:   []string{"read", "read", ""},
		Resources: []string{"User"},
		Roles:     []string{"admin", "admin"},
	}).Normalize()
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if policy.ID != "read-users" || policy.ModuleID != "user_management" {
		t.Fatalf("normalized policy = %#v", policy)
	}
	if len(policy.Actions) != 1 || policy.Actions[0] != "read" {
		t.Fatalf("actions = %#v; want [read]", policy.Actions)
	}
	if len(policy.Roles) != 1 || policy.Roles[0] != "admin" {
		t.Fatalf("roles = %#v; want [admin]", policy.Roles)
	}
}

func TestPolicyNormalizeRejectsInvalid(t *testing.T) {
	tests := []Policy{
		{},
		{ID: "p", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"User"}},
		{ID: "p", ModuleID: "m", Effect: "maybe", Actions: []string{"read"}, Resources: []string{"User"}},
		{ID: "p", ModuleID: "m", Effect: EffectAllow, Resources: []string{"User"}},
		{ID: "p", ModuleID: "m", Effect: EffectAllow, Actions: []string{"read"}},
	}
	for _, policy := range tests {
		if _, err := policy.Normalize(); err == nil {
			t.Fatalf("Normalize(%#v) should fail", policy)
		}
	}
}

func TestPolicyEvaluatorDenyWins(t *testing.T) {
	evaluator, err := NewPolicyEvaluator(
		Policy{ID: "allow", ModuleID: "user", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"User"}, Roles: []string{"admin"}},
		Policy{ID: "deny", ModuleID: "user", Effect: EffectDeny, Actions: []string{"read"}, Resources: []string{"User"}, Conditions: map[string]string{"blocked": "true"}},
	)
	if err != nil {
		t.Fatalf("NewPolicyEvaluator error = %v", err)
	}

	result, err := evaluator.Evaluate(context.Background(), Request{
		Principal: Principal{Roles: []string{"admin"}},
		Action:    "read",
		Resource:  Resource{Type: "User", ModuleID: "user"},
		Context:   map[string]string{"blocked": "true"},
	})
	if err != nil {
		t.Fatalf("Evaluate error = %v", err)
	}
	if result.Decision != DecisionDeny || result.PolicyID != "deny" {
		t.Fatalf("result = %#v; want deny policy", result)
	}
}

func TestPolicyEvaluatorAllowAndAbstain(t *testing.T) {
	evaluator, err := NewPolicyEvaluator(
		Policy{ID: "allow", ModuleID: "user", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"User"}, Roles: []string{"admin"}},
	)
	if err != nil {
		t.Fatalf("NewPolicyEvaluator error = %v", err)
	}

	result, err := evaluator.Evaluate(context.Background(), Request{
		Principal: Principal{Roles: []string{"admin"}},
		Action:    "read",
		Resource:  Resource{Type: "User", ModuleID: "user"},
	})
	if err != nil {
		t.Fatalf("Evaluate error = %v", err)
	}
	if result.Decision != DecisionAllow {
		t.Fatalf("Decision = %q; want allow", result.Decision)
	}

	result, err = evaluator.Evaluate(context.Background(), Request{
		Principal: Principal{Roles: []string{"member"}},
		Action:    "read",
		Resource:  Resource{Type: "User", ModuleID: "user"},
	})
	if err != nil {
		t.Fatalf("Evaluate error = %v", err)
	}
	if result.Decision != DecisionAbstain {
		t.Fatalf("Decision = %q; want abstain", result.Decision)
	}
}

func TestPolicyMatchRequiresMatchingRequestModule(t *testing.T) {
	policy, err := (Policy{
		ID:        "read-users",
		ModuleID:  "user_management",
		Effect:    EffectAllow,
		Actions:   []string{"read"},
		Resources: []string{"User"},
	}).Normalize()
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}

	if !policy.Match(Request{Action: " read ", Resource: Resource{Type: " User ", ModuleID: " user_management "}}) {
		t.Fatal("policy should match its own module after request normalization")
	}
	if policy.Match(Request{Action: "read", Resource: Resource{Type: "User", ModuleID: "billing"}}) {
		t.Fatal("policy must not bleed across module-owned resources")
	}
	if policy.Match(Request{Action: "read", Resource: Resource{Type: "User"}}) {
		t.Fatal("policy must not match when request omits the resource module")
	}
}

func TestPolicyMatchNormalizesPolicyAndRejectsInvalidPolicy(t *testing.T) {
	policy := Policy{
		ID:        " read-users ",
		ModuleID:  " user_management ",
		Effect:    " allow ",
		Actions:   []string{" read "},
		Resources: []string{" User "},
		Roles:     []string{" admin "},
	}
	if !policy.Match(Request{
		Principal: Principal{Roles: []string{" admin "}},
		Action:    " read ",
		Resource:  Resource{Type: " User ", ModuleID: " user_management "},
	}) {
		t.Fatal("Match should normalize policy and request before comparison")
	}
	if (Policy{}).Match(Request{}) {
		t.Fatal("invalid policy must not match")
	}
}

type staticEvaluator struct {
	result Result
	err    error
}

func (e staticEvaluator) Evaluate(context.Context, Request) (Result, error) {
	return e.result, e.err
}

func TestAggregateEvaluator(t *testing.T) {
	aggregate := NewAggregateEvaluator(
		staticEvaluator{result: Result{Decision: DecisionAllow, PolicyID: "allow"}},
		staticEvaluator{result: Result{Decision: DecisionDeny, PolicyID: "deny"}},
	)
	result, err := aggregate.Evaluate(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Evaluate error = %v", err)
	}
	if result.Decision != DecisionDeny || result.PolicyID != "deny" {
		t.Fatalf("result = %#v; want deny", result)
	}

	wantErr := errors.New("backend unavailable")
	aggregate = NewAggregateEvaluator(staticEvaluator{err: wantErr})
	if _, err := aggregate.Evaluate(context.Background(), Request{}); !errors.Is(err, wantErr) {
		t.Fatalf("Evaluate error = %v; want %v", err, wantErr)
	}

	aggregate = NewAggregateEvaluator(staticEvaluator{result: Result{Decision: "maybe"}})
	if _, err := aggregate.Evaluate(context.Background(), Request{}); err == nil || !strings.Contains(err.Error(), "invalid decision") {
		t.Fatalf("Evaluate error = %v; want invalid decision", err)
	}
}

func TestEvaluatorsRespectContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	policyEvaluator, err := NewPolicyEvaluator(Policy{
		ID:        "allow",
		ModuleID:  "user",
		Effect:    EffectAllow,
		Actions:   []string{"read"},
		Resources: []string{"User"},
	})
	if err != nil {
		t.Fatalf("NewPolicyEvaluator error = %v", err)
	}
	if _, err := policyEvaluator.Evaluate(ctx, Request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("PolicyEvaluator error = %v; want context.Canceled", err)
	}

	aggregate := NewAggregateEvaluator(staticEvaluator{result: Result{Decision: DecisionAllow}})
	if _, err := aggregate.Evaluate(ctx, Request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("AggregateEvaluator error = %v; want context.Canceled", err)
	}
}
