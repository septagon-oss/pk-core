// Validates: REQ-002.
// Per: ADR-0009.
// Discipline: C-14.

package registry

// rule_registry_test.go validates deterministic rule priority ordering and
// fallback behavior for extension classification rules.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "testing"

func TestRuleRegistryPriorityAndOrder(t *testing.T) {
	r := NewRuleRegistry[string]("default")
	r.RegisterRule(Rule[string]{
		Name:     "first",
		Priority: 10,
		Match:    ContainsMatch("value"),
		Value:    "first",
	})
	r.RegisterRule(Rule[string]{
		Name:     "second",
		Priority: 10,
		Match:    ContainsMatch("value"),
		Value:    "second",
	})
	r.RegisterRule(Rule[string]{
		Name:     "higher",
		Priority: 20,
		Match:    ContainsMatch("high"),
		Value:    "higher",
	})

	if got := r.Evaluate("value"); got != "first" {
		t.Fatalf("equal priority should preserve registration order: got %q", got)
	}
	if got := r.Evaluate("high value"); got != "higher" {
		t.Fatalf("higher priority should win: got %q", got)
	}
	if got := r.Evaluate("other"); got != "default" {
		t.Fatalf("fallback = %q; want default", got)
	}
}

func TestRuleRegistrySkipsNilMatchers(t *testing.T) {
	r := NewRuleRegistry[string]("fallback")
	r.RegisterRule(Rule[string]{Name: "nil", Priority: 100, Value: "nil"})
	r.RegisterRule(Rule[string]{Name: "suffix", Priority: 1, Match: SuffixMatch(".css"), Value: "css"})

	if got := r.Evaluate("theme.css"); got != "css" {
		t.Fatalf("Evaluate(theme.css) = %q; want css", got)
	}
}

func TestRuleMatchers(t *testing.T) {
	if !ContainsMatch("email")("user_email") {
		t.Fatal("ContainsMatch should match")
	}
	if !PrefixMatch("/assets/")("/assets/app.css") {
		t.Fatal("PrefixMatch should match")
	}
	if !SuffixMatch(".css")("/assets/app.css") {
		t.Fatal("SuffixMatch should match")
	}
}
