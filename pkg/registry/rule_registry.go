package registry

// rule_registry.go owns deterministic priority-ordered string rule matching
// used by extensible platform classification rules.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"sort"
	"strings"
	"sync"
)

// Rule defines a priority-ordered string matching rule.
//
// Higher priority wins. Rules with equal priority are evaluated in registration
// order so the result is deterministic.
type Rule[V any] struct {
	Name     string
	Priority int
	Match    func(string) bool
	Value    V
}

type registeredRule[V any] struct {
	rule  Rule[V]
	order int
}

// RuleRegistry evaluates input against ordered rules and returns the first
// matching value or the configured fallback.
type RuleRegistry[V any] struct {
	mu       sync.RWMutex
	rules    []registeredRule[V]
	sorted   bool
	next     int
	fallback V
}

// NewRuleRegistry creates a RuleRegistry.
func NewRuleRegistry[V any](fallback V) *RuleRegistry[V] {
	return &RuleRegistry[V]{fallback: fallback}
}

// RegisterRule adds a rule.
func (r *RuleRegistry[V]) RegisterRule(rule Rule[V]) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.rules = append(r.rules, registeredRule[V]{rule: rule, order: r.next})
	r.next++
	r.sorted = false
	r.mu.Unlock()
}

// Evaluate returns the first matching rule value or the fallback.
func (r *RuleRegistry[V]) Evaluate(input string) V {
	if r == nil {
		var zero V
		return zero
	}
	r.mu.RLock()
	if !r.sorted {
		r.mu.RUnlock()
		r.mu.Lock()
		if !r.sorted {
			sort.SliceStable(r.rules, func(i, j int) bool {
				if r.rules[i].rule.Priority != r.rules[j].rule.Priority {
					return r.rules[i].rule.Priority > r.rules[j].rule.Priority
				}
				return r.rules[i].order < r.rules[j].order
			})
			r.sorted = true
		}
		r.mu.Unlock()
		r.mu.RLock()
	}
	rules := append([]registeredRule[V](nil), r.rules...)
	fallback := r.fallback
	r.mu.RUnlock()

	for _, item := range rules {
		if item.rule.Match != nil && item.rule.Match(input) {
			return item.rule.Value
		}
	}
	return fallback
}

// Len returns the number of registered rules.
func (r *RuleRegistry[V]) Len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.rules)
}

// ContainsMatch returns a matcher that checks whether input contains substr.
func ContainsMatch(substr string) func(string) bool {
	return func(s string) bool { return strings.Contains(s, substr) }
}

// PrefixMatch returns a matcher that checks whether input starts with prefix.
func PrefixMatch(prefix string) func(string) bool {
	return func(s string) bool { return strings.HasPrefix(s, prefix) }
}

// SuffixMatch returns a matcher that checks whether input ends with suffix.
func SuffixMatch(suffix string) func(string) bool {
	return func(s string) bool { return strings.HasSuffix(s, suffix) }
}
