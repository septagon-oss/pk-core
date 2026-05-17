// Package authz defines PlatformKit's provider-neutral authorization contract.
package authz

// authz.go owns provider-neutral policy declarations and evaluator contracts
// so OSS and Pro authorization engines share the same core vocabulary.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Effect is the outcome a policy can declare.
type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

// Decision is the result of evaluating a request.
type Decision string

const (
	DecisionAllow        Decision = "allow"
	DecisionDeny         Decision = "deny"
	DecisionAbstain      Decision = "abstain"
	DecisionNotEvaluated Decision = "not_evaluated"
)

// Principal identifies the actor making a request.
type Principal struct {
	ID       string
	Kind     string
	TenantID string
	Roles    []string
	Claims   map[string]string
}

// Resource identifies the object or surface being accessed.
type Resource struct {
	Type     string
	ID       string
	TenantID string
	ModuleID string
}

// Request is an authorization decision input.
type Request struct {
	Principal Principal
	Action    string
	Resource  Resource
	Context   map[string]string
}

// Normalize trims request fields before structural evaluation. Authorization
// engines may add richer semantics, but shared evaluators should not depend on
// caller whitespace or duplicate role ordering.
func (r Request) Normalize() Request {
	r.Principal.ID = strings.TrimSpace(r.Principal.ID)
	r.Principal.Kind = strings.TrimSpace(r.Principal.Kind)
	r.Principal.TenantID = strings.TrimSpace(r.Principal.TenantID)
	r.Principal.Roles = normalizeList(r.Principal.Roles)
	r.Principal.Claims = normalizeMap(r.Principal.Claims)
	r.Action = strings.TrimSpace(r.Action)
	r.Resource.Type = strings.TrimSpace(r.Resource.Type)
	r.Resource.ID = strings.TrimSpace(r.Resource.ID)
	r.Resource.TenantID = strings.TrimSpace(r.Resource.TenantID)
	r.Resource.ModuleID = strings.TrimSpace(r.Resource.ModuleID)
	r.Context = normalizeMap(r.Context)
	return r
}

// Policy is a provider-neutral policy declaration. Engines may interpret
// Conditions, but the common identity/action/resource vocabulary is stable.
type Policy struct {
	ID          string
	ModuleID    string
	Effect      Effect
	Actions     []string
	Resources   []string
	Roles       []string
	Conditions  map[string]string
	Description string
}

// Normalize trims fields, fills stable defaults, and validates the declaration.
func (p Policy) Normalize() (Policy, error) {
	p.ID = strings.TrimSpace(p.ID)
	p.ModuleID = strings.TrimSpace(p.ModuleID)
	p.Effect = Effect(strings.TrimSpace(string(p.Effect)))
	p.Actions = normalizeList(p.Actions)
	p.Resources = normalizeList(p.Resources)
	p.Roles = normalizeList(p.Roles)
	p.Description = strings.TrimSpace(p.Description)
	p.Conditions = normalizeMap(p.Conditions)

	switch {
	case p.ID == "":
		return Policy{}, fmt.Errorf("authz policy ID is required")
	case p.ModuleID == "":
		return Policy{}, fmt.Errorf("authz policy %q module ID is required", p.ID)
	case p.Effect != EffectAllow && p.Effect != EffectDeny:
		return Policy{}, fmt.Errorf("authz policy %q effect must be %q or %q", p.ID, EffectAllow, EffectDeny)
	case len(p.Actions) == 0:
		return Policy{}, fmt.Errorf("authz policy %q must declare at least one action", p.ID)
	case len(p.Resources) == 0:
		return Policy{}, fmt.Errorf("authz policy %q must declare at least one resource", p.ID)
	}
	return p, nil
}

// Match reports whether the policy structurally matches a request. Conditions
// are exact key/value matches against Request.Context. Empty Roles means any
// role can match.
func (p Policy) Match(req Request) bool {
	normalizedPolicy, err := p.Normalize()
	if err != nil {
		return false
	}
	p = normalizedPolicy
	req = req.Normalize()
	if !matchesModule(p.ModuleID, req.Resource.ModuleID) {
		return false
	}
	if !containsOrWildcard(p.Actions, req.Action) {
		return false
	}
	if !containsOrWildcard(p.Resources, req.Resource.Type) {
		return false
	}
	if len(p.Roles) > 0 && !intersects(p.Roles, req.Principal.Roles) {
		return false
	}
	for key, want := range p.Conditions {
		if req.Context[key] != want {
			return false
		}
	}
	return true
}

// Result is an evaluator result with optional policy evidence.
type Result struct {
	Decision Decision
	PolicyID string
	Reason   string
}

// Evaluator evaluates one authorization request.
type Evaluator interface {
	Evaluate(ctx context.Context, req Request) (Result, error)
}

// PolicyEvaluator evaluates static policy declarations.
type PolicyEvaluator struct {
	policies []Policy
}

// NewPolicyEvaluator validates and stores policies in deterministic order.
func NewPolicyEvaluator(policies ...Policy) (*PolicyEvaluator, error) {
	normalized := make([]Policy, 0, len(policies))
	for _, policy := range policies {
		p, err := policy.Normalize()
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, p)
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return normalized[i].ID < normalized[j].ID
	})
	return &PolicyEvaluator{policies: normalized}, nil
}

// Evaluate implements Evaluator. Deny wins over allow; no match abstains.
func (e *PolicyEvaluator) Evaluate(ctx context.Context, req Request) (Result, error) {
	if err := contextErr(ctx); err != nil {
		return Result{}, err
	}
	if e == nil {
		return Result{Decision: DecisionNotEvaluated, Reason: "nil evaluator"}, nil
	}
	var allow *Policy
	for _, policy := range e.policies {
		if !policy.Match(req) {
			continue
		}
		if policy.Effect == EffectDeny {
			return Result{Decision: DecisionDeny, PolicyID: policy.ID, Reason: "deny policy matched"}, nil
		}
		if allow == nil {
			p := policy
			allow = &p
		}
	}
	if allow != nil {
		return Result{Decision: DecisionAllow, PolicyID: allow.ID, Reason: "allow policy matched"}, nil
	}
	return Result{Decision: DecisionAbstain, Reason: "no policy matched"}, nil
}

// AggregateEvaluator evaluates multiple engines. Deny wins; any allow wins over
// abstain; errors stop evaluation.
type AggregateEvaluator struct {
	evaluators []Evaluator
}

// NewAggregateEvaluator creates an aggregate evaluator.
func NewAggregateEvaluator(evaluators ...Evaluator) *AggregateEvaluator {
	cleaned := make([]Evaluator, 0, len(evaluators))
	for _, evaluator := range evaluators {
		if evaluator != nil {
			cleaned = append(cleaned, evaluator)
		}
	}
	return &AggregateEvaluator{evaluators: cleaned}
}

// Evaluate implements Evaluator.
func (e *AggregateEvaluator) Evaluate(ctx context.Context, req Request) (Result, error) {
	if err := contextErr(ctx); err != nil {
		return Result{}, err
	}
	if e == nil || len(e.evaluators) == 0 {
		return Result{Decision: DecisionNotEvaluated, Reason: "no evaluators configured"}, nil
	}
	var allow Result
	for _, evaluator := range e.evaluators {
		result, err := evaluator.Evaluate(ctx, req)
		if err != nil {
			return Result{}, err
		}
		switch result.Decision {
		case DecisionDeny:
			return result, nil
		case DecisionAllow:
			if allow.Decision == "" {
				allow = result
			}
		case DecisionAbstain, DecisionNotEvaluated:
		default:
			return Result{}, fmt.Errorf("authz evaluator returned invalid decision %q", result.Decision)
		}
	}
	if allow.Decision == DecisionAllow {
		return allow, nil
	}
	return Result{Decision: DecisionAbstain, Reason: "all evaluators abstained"}, nil
}

func normalizeList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func containsOrWildcard(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, value := range values {
		if value == "*" || value == needle {
			return true
		}
	}
	return false
}

func matchesModule(policyModule, resourceModule string) bool {
	policyModule = strings.TrimSpace(policyModule)
	resourceModule = strings.TrimSpace(resourceModule)
	if policyModule == "*" {
		return true
	}
	if resourceModule == "" {
		return false
	}
	return policyModule == resourceModule
}

func intersects(left, right []string) bool {
	set := map[string]struct{}{}
	for _, value := range right {
		set[strings.TrimSpace(value)] = struct{}{}
	}
	for _, value := range left {
		if _, ok := set[value]; ok {
			return true
		}
	}
	return false
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
