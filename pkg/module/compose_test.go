package module_test

import (
	"slices"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/module"
)

type AuditService interface {
	Record(message string) error
}

type UserService interface {
	Create(email string) error
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
