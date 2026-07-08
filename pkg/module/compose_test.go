package module_test

// compose_test.go validates dependency ordering, missing-provider failures,
// and Pro embedding of OSS module primitives.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/module"
)

type AuditService interface {
	Record(message string) error
}

type UserService interface {
	Create(email string) error
}

type ContentService interface {
	Render(slug string) (string, error)
}

type PaidBillingModule struct {
	module.Module
	Provider string
}

func TestComposeOrdersByTypedPorts(t *testing.T) {
	t.Parallel()

	audit := module.NewBundle("audit", []module.Entry{
		{ID: "audit", New: func() module.Composable {
			return module.Must(
				module.Metadata{ID: "audit"},
				module.WithProvides(module.Provide[AuditService]("1.0.0")),
			)
		}},
	}, nil)
	user := module.NewBundle("user", []module.Entry{
		{ID: "user", New: func() module.Composable {
			return module.Must(
				module.Metadata{ID: "user"},
				module.WithProvides(module.Provide[UserService]("1.0.0")),
				module.WithDependencies(module.Require[AuditService](module.DependencySpec{
					Version:           "1.0.0",
					Purpose:           "audit user lifecycle changes",
					PreferredProvider: "audit",
				})),
			)
		}},
	}, []string{"user", "audit"})

	catalog := module.NewCatalog().Add(user).Add(audit).MustBuild()
	plan, err := module.Compose(catalog)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}

	got := moduleIDs(plan.Modules)
	if !slices.Equal(got, []string{"audit", "user"}) {
		t.Fatalf("composed module order = %v", got)
	}
}

func TestComposeFailsMissingRequiredPort(t *testing.T) {
	t.Parallel()

	user := module.NewBundle("user", []module.Entry{
		{ID: "user", New: func() module.Composable {
			return module.Must(
				module.Metadata{ID: "user"},
				module.WithDependencies(module.Require[AuditService](module.DependencySpec{
					Purpose: "audit user lifecycle changes",
				})),
			)
		}},
	}, []string{"user"})

	catalog := module.NewCatalog().Add(user).MustBuild()
	_, err := module.Compose(catalog)
	if err == nil {
		t.Fatal("Compose() error = nil, want missing dependency error")
	}
}

func TestComposeFailsWhenProviderVersionDoesNotMatch(t *testing.T) {
	t.Parallel()

	catalog := module.NewCatalog().Add(module.NewBundle("test", []module.Entry{
		{ID: "audit", New: func() module.Composable {
			return module.Must(module.Metadata{ID: "audit"}, module.WithProvides(module.Provide[AuditService]("1.0.0")))
		}},
		{ID: "content", New: func() module.Composable {
			return module.Must(
				module.Metadata{ID: "content"},
				module.WithDependencies(module.Require[AuditService](module.DependencySpec{
					Version: "^2.0.0",
					Purpose: "audit content",
				})),
			)
		}},
	}, []string{"audit", "content"})).MustBuild()

	_, err := module.Compose(catalog)
	if err == nil {
		t.Fatal("expected incompatible provider version to fail")
	}
	if !strings.Contains(err.Error(), "content requires") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestComposeAllowsFallbackProvider(t *testing.T) {
	t.Parallel()

	catalog := module.NewCatalog().Add(module.NewBundle("test", []module.Entry{
		{ID: "audit_fallback", New: func() module.Composable {
			return module.Must(module.Metadata{ID: "audit_fallback"}, module.WithProvides(module.Provide[AuditService]("1.0.0")))
		}},
		{ID: "content", New: func() module.Composable {
			return module.Must(
				module.Metadata{ID: "content"},
				module.WithDependencies(module.Require[AuditService](module.DependencySpec{
					Version:           "^1.0.0",
					Purpose:           "audit content",
					PreferredProvider: "audit",
					FallbackProviders: []string{"audit_fallback"},
				})),
			)
		}},
	}, []string{"content", "audit_fallback"})).MustBuild()

	plan, err := module.Compose(catalog)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}

	got := moduleIDs(plan.Modules)
	if !slices.Equal(got, []string{"audit_fallback", "content"}) {
		t.Fatalf("composed module order = %v", got)
	}
}

func TestSortDoesNotTurnOptionalIntegrationsIntoHardCycles(t *testing.T) {
	t.Parallel()

	audit := module.Must(
		module.Metadata{ID: "audit"},
		module.WithProvides(module.Provide[AuditService]("1.0.0")),
		module.WithDependencies(module.Optional[UserService](module.DependencySpec{
			Version: "^1.0.0",
			Purpose: "enrich audit events with user context",
		})),
	)
	user := module.Must(
		module.Metadata{ID: "user"},
		module.WithProvides(module.Provide[UserService]("1.0.0")),
		module.WithDependencies(module.Optional[AuditService](module.DependencySpec{
			Version: "^1.0.0",
			Purpose: "record optional user lifecycle audit events",
		})),
	)

	sorted, err := module.Sort([]module.Composable{user, audit})
	if err != nil {
		t.Fatalf("Sort() error = %v", err)
	}
	if got := moduleIDs(sorted); !slices.Equal(got, []string{"audit", "user"}) {
		t.Fatalf("module order = %v", got)
	}
}

func TestSortUsesOneProviderPerRequiredPortToAvoidFalseCycles(t *testing.T) {
	t.Parallel()

	auditDecorated := module.Must(
		module.Metadata{ID: "audit_a"},
		module.WithProvides(module.Provide[AuditService]("1.0.0")),
		module.WithDependencies(module.Require[ContentService](module.DependencySpec{
			Version: "^1.0.0",
			Purpose: "decorate audit events with rendered content",
		})),
	)
	auditPrimary := module.Must(
		module.Metadata{ID: "audit_b"},
		module.WithProvides(module.Provide[AuditService]("1.0.0")),
	)
	content := module.Must(
		module.Metadata{ID: "content"},
		module.WithProvides(module.Provide[ContentService]("1.0.0")),
		module.WithDependencies(module.Require[AuditService](module.DependencySpec{
			Version: "^1.0.0",
			Purpose: "audit content changes",
		})),
	)

	sorted, err := module.Sort([]module.Composable{content, auditDecorated, auditPrimary})
	if err != nil {
		t.Fatalf("Sort() error = %v", err)
	}
	if got := moduleIDs(sorted); !slices.Equal(got, []string{"audit_b", "content", "audit_a"}) {
		t.Fatalf("module order = %v", got)
	}
}

func TestComposeRejectsUnlistedProviderWhenPreferredProviderIsDeclared(t *testing.T) {
	t.Parallel()

	catalog := module.NewCatalog().Add(module.NewBundle("test", []module.Entry{
		{ID: "audit_alternative", New: func() module.Composable {
			return module.Must(module.Metadata{ID: "audit_alternative"}, module.WithProvides(module.Provide[AuditService]("1.0.0")))
		}},
		{ID: "content", New: func() module.Composable {
			return module.Must(
				module.Metadata{ID: "content"},
				module.WithDependencies(module.Require[AuditService](module.DependencySpec{
					Version:           "^1.0.0",
					Purpose:           "audit content",
					PreferredProvider: "audit",
					FallbackProviders: []string{"audit_fallback"},
				})),
			)
		}},
	}, []string{"content", "audit_alternative"})).MustBuild()

	_, err := module.Compose(catalog)
	if err == nil {
		t.Fatal("expected unlisted provider to fail")
	}
	if !strings.Contains(err.Error(), "preferred provider audit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSortFailsOnInvalidProviderVersion(t *testing.T) {
	t.Parallel()

	audit := module.Must(module.Metadata{ID: "audit"}, module.WithProvides(module.Port{
		Name:    module.PortOf[AuditService]("").Name,
		Version: "not-semver",
	}))
	content := module.Must(
		module.Metadata{ID: "content"},
		module.WithDependencies(module.Require[AuditService](module.DependencySpec{Version: ">=1.0.0"})),
	)

	_, err := module.Sort([]module.Composable{content, audit})
	if err == nil {
		t.Fatal("expected invalid provider version to fail")
	}
	if !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsInvalidDependencyDeclaration(t *testing.T) {
	t.Parallel()

	content := module.Must(
		module.Metadata{ID: "content"},
		module.WithDependencies(module.Dependency{
			Port:     module.Port{Name: module.PortOf[AuditService]("").Name, Version: ">"},
			Required: true,
		}),
	)

	err := module.Validate([]module.Composable{content})
	if err == nil {
		t.Fatal("expected invalid dependency declaration to fail")
	}
	if !strings.Contains(err.Error(), "invalid dependency") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSortRejectsInvalidSingleModuleDeclaration(t *testing.T) {
	t.Parallel()

	content := module.Must(
		module.Metadata{ID: "content"},
		module.WithDependencies(module.Dependency{
			Port:     module.Port{Name: module.PortOf[AuditService]("").Name, Version: ">"},
			Required: true,
		}),
	)

	_, err := module.Sort([]module.Composable{content})
	if err == nil {
		t.Fatal("expected invalid single-module dependency declaration to fail")
	}
	if !strings.Contains(err.Error(), "invalid dependency") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsDuplicateModuleIDs(t *testing.T) {
	t.Parallel()

	one := module.Must(module.Metadata{ID: "audit"})
	two := module.Must(module.Metadata{ID: "audit"})

	err := module.Validate([]module.Composable{one, two})
	if err == nil {
		t.Fatal("expected duplicate module IDs to fail")
	}
	if !strings.Contains(err.Error(), "duplicate module ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsTypedNilModule(t *testing.T) {
	t.Parallel()

	var nilModule *module.Module
	err := module.Validate([]module.Composable{nilModule})
	if err == nil {
		t.Fatal("expected typed nil module to fail")
	}
	if !strings.Contains(err.Error(), "module at index 0 is nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProModuleCanEmbedOSSModule(t *testing.T) {
	t.Parallel()

	base := module.Must(
		module.Metadata{ID: "billing"},
		module.WithDependencies(module.Require[AuditService](module.DependencySpec{
			Purpose:           "audit invoice events",
			PreferredProvider: "audit",
		})),
	)
	pro := &PaidBillingModule{Module: *base, Provider: "stripe"}

	var composable module.Composable = pro
	if composable.Metadata().ID != "billing" {
		t.Fatalf("embedded module metadata ID = %q", composable.Metadata().ID)
	}
}

func moduleIDs(modules []module.Composable) []string {
	ids := make([]string, 0, len(modules))
	for _, module := range modules {
		ids = append(ids, module.Metadata().ID)
	}
	return ids
}
