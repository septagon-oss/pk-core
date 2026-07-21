// Validates: REQ-002.
// Per: ADR-0009.
// Discipline: C-14.

package module_test

// dependency_test.go validates typed port dependency declarations and version
// constraint matching used by module composition.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"strings"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/module"
)

type fakePort interface {
	DoSomething()
}

func TestRequiresPortTypedInterface(t *testing.T) {
	dep := module.RequiresPort[fakePort](module.PortSpec{
		Purpose:           "fake",
		Version:           "^1.0.0",
		Category:          module.DependencyCategorySecurity,
		SubCategory:       "audit",
		PreferredProvider: "test_provider",
		FallbackProviders: []string{"fallback_provider"},
	})

	if !dep.Required {
		t.Error("expected Required=true from RequiresPort")
	}
	if !strings.HasSuffix(dep.Port.Name, "fakePort") {
		t.Errorf("Port.Name = %q, expected to end with fakePort", dep.Port.Name)
	}
	if dep.Port.Version != "^1.0.0" {
		t.Errorf("Port.Version = %q, want ^1.0.0", dep.Port.Version)
	}
	if dep.Category != module.DependencyCategorySecurity {
		t.Errorf("Category = %q", dep.Category)
	}
	if dep.SubCategory != "audit" {
		t.Errorf("SubCategory = %q", dep.SubCategory)
	}
	if dep.PreferredProvider != "test_provider" {
		t.Errorf("PreferredProvider = %q", dep.PreferredProvider)
	}
	if got := strings.Join(dep.FallbackProviders, ","); got != "fallback_provider" {
		t.Errorf("FallbackProviders = %q", got)
	}
}

func TestOptionalPort(t *testing.T) {
	dep := module.OptionalPort[fakePort](module.PortSpec{Purpose: "fake-optional"})
	if dep.Required {
		t.Error("expected Required=false from OptionalPort")
	}
}

func TestModuleDependenciesReturnDeepCopy(t *testing.T) {
	t.Parallel()

	deps := []module.Dependency{{
		Port:              module.PortOf[fakePort]("1.0.0"),
		Required:          true,
		FallbackProviders: []string{"audit_fallback"},
	}}
	mod := module.Must(module.Metadata{ID: "consumer"}, module.WithDependencies(deps...))

	deps[0].FallbackProviders[0] = "mutated_before_store"
	got := mod.Dependencies()
	if got[0].FallbackProviders[0] != "audit_fallback" {
		t.Fatalf("WithDependencies aliased caller fallback providers: %#v", got)
	}

	got[0].FallbackProviders[0] = "mutated_after_read"
	again := mod.Dependencies()
	if again[0].FallbackProviders[0] != "audit_fallback" {
		t.Fatalf("Dependencies() returned aliased fallback providers: %#v", again)
	}
}

func TestRequiresPortPanicsOnNonInterfaceType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for non-interface type parameter")
		}
	}()
	type concrete struct{}
	module.RequiresPort[concrete](module.PortSpec{})
}

func TestRequiresPortPanicsOnUnnamedInterfaceType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unnamed interface type parameter")
		}
	}()
	module.RequiresPort[interface{ DoSomething() }](module.PortSpec{})
}

func TestRequiresPortDefersInvalidVersionConstraintToCompositionValidation(t *testing.T) {
	dep := module.RequiresPort[fakePort](module.PortSpec{Version: ">>1.0"})
	if dep.Port.Version != ">>1.0" {
		t.Fatalf("Port.Version = %q; want original invalid declaration", dep.Port.Version)
	}
}

func TestRequiresPortDefaultsCategoryToUnknown(t *testing.T) {
	dep := module.RequiresPort[fakePort](module.PortSpec{Purpose: "fake"})
	if dep.Category != module.DependencyCategoryUnknown {
		t.Errorf("Category = %q, want %q", dep.Category, module.DependencyCategoryUnknown)
	}
}

func TestMatchesVersion(t *testing.T) {
	cases := []struct {
		producer   module.PortVersion
		constraint string
		want       bool
	}{
		{"1.0.0", "", true},
		{"1.0.0", "*", true},
		{"1.2.3", "1.2.3", true},
		{"1.2.4", "1.2.3", false},
		{"2.0.0", ">=1.0.0", true},
		{"0.9.0", ">=1.0.0", false},
		{"1.5.0", "^1.0.0", true},
		{"2.0.0", "^1.0.0", false},
		{"0.2.5", "^0.2.3", true},
		{"0.3.0", "^0.2.3", false},
		{"0.0.3", "^0.0.3", true},
		{"0.0.4", "^0.0.3", false},
		{"1.2.5", "~1.2.0", true},
		{"1.3.0", "~1.2.0", false},
		{"1.5.0", ">=1.0.0,<2.0.0", true},
		{"2.5.0", ">=1.0.0,<2.0.0", false},
		{"1.0.0", "!=1.0.0", false},
		{"1.0.1", "!=1.0.0", true},
		{"v1.2.3", "1.2.3", true},
		{"1.2.3-pre", "1.2.3", true},
	}
	for _, c := range cases {
		got, err := module.MatchesVersion(c.producer, c.constraint)
		if err != nil {
			t.Errorf("MatchesVersion(%q, %q) err: %v", c.producer, c.constraint, err)
			continue
		}
		if got != c.want {
			t.Errorf("MatchesVersion(%q, %q) = %v, want %v", c.producer, c.constraint, got, c.want)
		}
	}
}

func TestMatchesVersionRejectsMalformed(t *testing.T) {
	cases := []struct {
		producer   module.PortVersion
		constraint string
	}{
		{"abc", "1.0.0"},
		{"1.0.0", ">>1.0.0"},
		{"1.0.0", "1.x"},
		{"1.0.0", ">=1.0.0,"},
		{"1.0.0", ">"},
		{"v", ">=0.0.0"},
		{"+build", ">=0.0.0"},
		{"1.2.3-", ">=0.0.0"},
	}
	for _, c := range cases {
		_, err := module.MatchesVersion(c.producer, c.constraint)
		if err == nil {
			t.Errorf("MatchesVersion(%q, %q): expected error", c.producer, c.constraint)
		}
	}
}

func TestValidatePortVersion(t *testing.T) {
	valid := []module.PortVersion{
		"1",
		"1.2",
		"1.2.3",
		"v1.2.3",
		"1.2.3-pre",
		"1.2.3+build",
	}
	for _, version := range valid {
		if err := module.ValidatePortVersion(version); err != nil {
			t.Errorf("ValidatePortVersion(%q) error = %v", version, err)
		}
	}

	invalid := []module.PortVersion{
		"",
		"v",
		"+build",
		"-pre",
		"1.2.3-",
		"1.2.3+",
		"1.2.3.4",
		"1.x.0",
		"01.2.3",
		"1. 2.3",
	}
	for _, version := range invalid {
		if err := module.ValidatePortVersion(version); err == nil {
			t.Errorf("ValidatePortVersion(%q) error = nil, want error", version)
		}
	}
}

func TestProvideDefersInvalidProviderVersionToCompositionValidation(t *testing.T) {
	port := module.Provide[fakePort]("^1.0.0")
	if port.Version != "^1.0.0" {
		t.Fatalf("Port.Version = %q; want original invalid declaration", port.Version)
	}
}

func TestMatchesVersionRejectsEmptyProducer(t *testing.T) {
	if _, err := module.MatchesVersion("", ""); err == nil {
		t.Fatal("MatchesVersion accepted an implicit producer version")
	}
}
