// Package mutation defines the core governed-mutation contract.
package mutation

// mutation.go owns the small mutation-governance contract that lets modules
// declare gated changes without coupling core to a workflow implementation.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/septagon-oss/pk-core/pkg/authz"
	"github.com/septagon-oss/pk-core/pkg/registry"
)

// Operation is the coarse mutation verb.
type Operation string

// Recognized mutation operations.
const (
	// OperationCreate creates a new entity.
	OperationCreate Operation = "create"
	// OperationUpdate updates an existing entity.
	OperationUpdate Operation = "update"
	// OperationDelete removes an existing entity.
	OperationDelete Operation = "delete"
	// OperationCustom is a module-defined operation.
	OperationCustom Operation = "custom"
)

// GateDecision describes how a mutation should proceed.
type GateDecision string

// Possible gate decisions for a mutation.
const (
	// GateAllow permits the mutation to proceed immediately.
	GateAllow GateDecision = "allow"
	// GateRequiresReview routes the mutation through a review workflow.
	GateRequiresReview GateDecision = "requires_review"
	// GateDeny refuses the mutation.
	GateDeny GateDecision = "deny"
)

// Intent is the stable request shape for a governed mutation.
type Intent struct {
	ID         string
	ModuleID   string
	Entity     string
	EntityID   string
	Operation  Operation
	ChangeType string
	TenantID   string
	Actor      authz.Principal
	Reason     string
	Metadata   map[string]string
	CreatedAt  time.Time
}

// Normalize validates and returns a copy of the intent.
func (i Intent) Normalize() (Intent, error) {
	i.ID = strings.TrimSpace(i.ID)
	i.ModuleID = strings.TrimSpace(i.ModuleID)
	i.Entity = strings.TrimSpace(i.Entity)
	i.EntityID = strings.TrimSpace(i.EntityID)
	i.Operation = Operation(strings.TrimSpace(string(i.Operation)))
	i.ChangeType = strings.TrimSpace(i.ChangeType)
	i.TenantID = strings.TrimSpace(i.TenantID)
	i.Actor.ID = strings.TrimSpace(i.Actor.ID)
	i.Actor.Kind = strings.TrimSpace(i.Actor.Kind)
	i.Actor.TenantID = strings.TrimSpace(i.Actor.TenantID)
	i.Actor.Roles = normalizeStrings(i.Actor.Roles)
	i.Actor.Claims = normalizeMap(i.Actor.Claims)
	i.Reason = strings.TrimSpace(i.Reason)
	i.Metadata = normalizeMap(i.Metadata)
	if i.CreatedAt.IsZero() {
		i.CreatedAt = time.Now().UTC()
	}

	switch {
	case i.ModuleID == "":
		return Intent{}, fmt.Errorf("mutation intent module ID is required")
	case hasWhitespace(i.ModuleID):
		return Intent{}, fmt.Errorf("mutation intent module ID %q must not contain whitespace", i.ModuleID)
	case i.Entity == "":
		return Intent{}, fmt.Errorf("mutation intent entity is required")
	case hasWhitespace(i.Entity):
		return Intent{}, fmt.Errorf("mutation intent entity %q must not contain whitespace", i.Entity)
	case i.Operation == "":
		return Intent{}, fmt.Errorf("mutation intent operation is required")
	case !validOperation(i.Operation):
		return Intent{}, fmt.Errorf("mutation intent operation %q is not supported", i.Operation)
	case i.ChangeType == "":
		i.ChangeType = string(i.Operation)
	}
	if i.TenantID != "" && i.Actor.TenantID != "" && i.TenantID != i.Actor.TenantID {
		return Intent{}, fmt.Errorf("mutation intent tenant ID %q does not match actor tenant ID %q", i.TenantID, i.Actor.TenantID)
	}
	return i, nil
}

// Rule declares when a mutation is auto-allowed, denied, or sent for review.
type Rule struct {
	ID          string
	ModuleID    string
	Entity      string
	Operations  []Operation
	ChangeTypes []string
	Decision    GateDecision
	Description string
}

// Normalize validates and returns a copy of the rule.
func (r Rule) Normalize() (Rule, error) {
	r.ID = strings.TrimSpace(r.ID)
	r.ModuleID = strings.TrimSpace(r.ModuleID)
	r.Entity = strings.TrimSpace(r.Entity)
	r.Operations = normalizeOperations(r.Operations)
	r.ChangeTypes = normalizeStrings(r.ChangeTypes)
	r.Decision = GateDecision(strings.TrimSpace(string(r.Decision)))
	r.Description = strings.TrimSpace(r.Description)
	switch {
	case r.ID == "":
		return Rule{}, fmt.Errorf("mutation rule ID is required")
	case hasWhitespace(r.ID):
		return Rule{}, fmt.Errorf("mutation rule ID %q must not contain whitespace", r.ID)
	case r.ModuleID == "":
		return Rule{}, fmt.Errorf("mutation rule %q module ID is required", r.ID)
	case hasWhitespace(r.ModuleID):
		return Rule{}, fmt.Errorf("mutation rule %q module ID %q must not contain whitespace", r.ID, r.ModuleID)
	case r.Entity == "":
		return Rule{}, fmt.Errorf("mutation rule %q entity is required", r.ID)
	case hasWhitespace(r.Entity):
		return Rule{}, fmt.Errorf("mutation rule %q entity %q must not contain whitespace", r.ID, r.Entity)
	case len(r.Operations) == 0:
		return Rule{}, fmt.Errorf("mutation rule %q must declare at least one operation", r.ID)
	case !validDecision(r.Decision):
		return Rule{}, fmt.Errorf("mutation rule %q decision must be allow, requires_review, or deny", r.ID)
	}
	for _, operation := range r.Operations {
		if !validOperation(operation) {
			return Rule{}, fmt.Errorf("mutation rule %q operation %q is not supported", r.ID, operation)
		}
	}
	return r, nil
}

// Match reports whether the rule applies to an intent.
func (r Rule) Match(intent Intent) bool {
	normalizedRule, err := r.Normalize()
	if err != nil {
		return false
	}
	normalizedIntent, err := intent.Normalize()
	if err != nil {
		return false
	}
	r = normalizedRule
	intent = normalizedIntent
	if r.ModuleID != intent.ModuleID || r.Entity != intent.Entity {
		return false
	}
	if !containsOperation(r.Operations, intent.Operation) {
		return false
	}
	if len(r.ChangeTypes) > 0 && !containsString(r.ChangeTypes, intent.ChangeType) {
		return false
	}
	return true
}

// Result is the answer returned by a gate.
type Result struct {
	Decision GateDecision
	RuleID   string
	ReviewID string
	Reason   string
}

// Gate evaluates a mutation intent.
type Gate interface {
	EvaluateMutation(ctx context.Context, intent Intent) (Result, error)
}

// RuleGate is a deterministic static gate for core and tests. Deny wins over
// review; review wins over allow; no match allows by default so lean
// compositions can run without a change-management module.
type RuleGate struct {
	rules           []Rule
	defaultDecision GateDecision
}

// RuleGateOption configures a RuleGate.
type RuleGateOption func(*RuleGate)

// WithDefaultDecision sets the decision returned when no rule matches. The
// default is GateAllow for lightweight OSS compositions; hosted or regulated
// distributions can set GateRequiresReview or GateDeny.
func WithDefaultDecision(decision GateDecision) RuleGateOption {
	return func(g *RuleGate) {
		g.defaultDecision = GateDecision(strings.TrimSpace(string(decision)))
	}
}

// NewRuleGate validates and stores rules in deterministic order.
func NewRuleGate(rules ...Rule) (*RuleGate, error) {
	return NewRuleGateWithOptions(rules)
}

// NewRuleGateWithOptions validates and stores rules in deterministic order.
func NewRuleGateWithOptions(rules []Rule, opts ...RuleGateOption) (*RuleGate, error) {
	gate := &RuleGate{defaultDecision: GateAllow}
	for _, opt := range opts {
		if opt != nil {
			opt(gate)
		}
	}
	if !validDecision(gate.defaultDecision) {
		return nil, fmt.Errorf("mutation rule gate default decision must be allow, requires_review, or deny")
	}
	normalized := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		r, err := rule.Normalize()
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, r)
	}
	slices.SortStableFunc(normalized, func(a, b Rule) int { return cmp.Compare(a.ID, b.ID) })
	gate.rules = normalized
	return gate, nil
}

// EvaluateMutation implements Gate.
func (g *RuleGate) EvaluateMutation(ctx context.Context, intent Intent) (Result, error) {
	if err := contextErr(ctx); err != nil {
		return Result{}, err
	}
	normalized, err := intent.Normalize()
	if err != nil {
		return Result{}, err
	}
	if g == nil {
		return Result{Decision: GateAllow, Reason: "no mutation gate configured"}, nil
	}
	var review *Rule
	var allow *Rule
	for _, rule := range g.rules {
		if !rule.Match(normalized) {
			continue
		}
		switch rule.Decision {
		case GateDeny:
			return Result{Decision: GateDeny, RuleID: rule.ID, Reason: "deny rule matched"}, nil
		case GateRequiresReview:
			if review == nil {
				r := rule
				review = &r
			}
		case GateAllow:
			if allow == nil {
				r := rule
				allow = &r
			}
		}
	}
	if review != nil {
		return Result{Decision: GateRequiresReview, RuleID: review.ID, Reason: "review rule matched"}, nil
	}
	if allow != nil {
		return Result{Decision: GateAllow, RuleID: allow.ID, Reason: "allow rule matched"}, nil
	}
	return Result{Decision: g.defaultDecision, Reason: "no mutation rule matched"}, nil
}

// CatalogSpec is the standard registry spec for mutation rules.
func CatalogSpec() registry.Spec {
	return registry.Spec{
		ID:           "core.mutation_rules",
		Owner:        registry.OwnerCore,
		Contribution: "mutation.Rule",
		Key:          "moduleID:entity:ruleID",
		Description:  "Governed mutation rule catalog",
	}
}

// Key identifies a mutation rule contribution.
type Key struct {
	ModuleID string
	Entity   string
	RuleID   string
}

// String returns the "moduleID:entity:ruleID" representation of the key.
func (k Key) String() string {
	return strings.TrimSpace(k.ModuleID) + ":" + strings.TrimSpace(k.Entity) + ":" + strings.TrimSpace(k.RuleID)
}

// NewCatalog builds a deterministic mutation rule catalog.
func NewCatalog(rules ...Rule) (*registry.Catalog[Key, Rule], error) {
	builder := registry.NewCatalogBuilder[Key, Rule](
		registry.WithCatalogSpec[Key, Rule](CatalogSpec()),
		registry.WithCatalogKeyLess[Key, Rule](func(a, b Key) bool { return a.String() < b.String() }),
		registry.WithCatalogKeyValidator[Key, Rule](func(key Key) error {
			if strings.TrimSpace(key.ModuleID) == "" || strings.TrimSpace(key.Entity) == "" || strings.TrimSpace(key.RuleID) == "" {
				return fmt.Errorf("moduleID, entity, and ruleID are required")
			}
			return nil
		}),
		registry.WithCatalogValueValidator[Key, Rule](func(rule Rule) error {
			_, err := rule.Normalize()
			return err
		}),
	)
	for _, rule := range rules {
		normalized, err := rule.Normalize()
		if err != nil {
			return nil, err
		}
		key := Key{ModuleID: normalized.ModuleID, Entity: normalized.Entity, RuleID: normalized.ID}
		builder.Add(key, normalized, normalized.ModuleID)
	}
	return builder.Build()
}

func validOperation(operation Operation) bool {
	switch operation {
	case OperationCreate, OperationUpdate, OperationDelete, OperationCustom:
		return true
	default:
		return false
	}
}

func validDecision(decision GateDecision) bool {
	switch decision {
	case GateAllow, GateRequiresReview, GateDeny:
		return true
	default:
		return false
	}
}

func normalizeOperations(values []Operation) []Operation {
	out := make([]Operation, 0, len(values))
	seen := map[Operation]struct{}{}
	for _, value := range values {
		value = Operation(strings.TrimSpace(string(value)))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func containsOperation(values []Operation, want Operation) bool {
	return slices.Contains(values, want)
}

func normalizeStrings(values []string) []string {
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
	slices.Sort(out)
	return out
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
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

func hasWhitespace(value string) bool {
	return strings.ContainsAny(value, " \t\n\r")
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
