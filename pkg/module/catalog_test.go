package module_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/module"
)

func TestCatalogComposesBundles(t *testing.T) {
	t.Parallel()

	auth := module.NewBundle("core.auth", []module.Entry{
		{ID: "auth", New: func() module.Composable {
			return module.Must(module.Metadata{ID: "auth"})
		}},
	}, []string{"auth"})
	user := module.NewBundle("core.user", []module.Entry{
		{ID: "user", New: func() module.Composable {
			return module.Must(module.Metadata{ID: "user"})
		}},
	}, []string{"auth", "user"})

	catalog, err := module.NewCatalog().Add(auth).Add(user).Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if got := catalog.ModuleIDs(); !slices.Equal(got, []string{"auth", "user"}) {
		t.Fatalf("ModuleIDs() = %v", got)
	}
	if got := catalog.Defaults(); !slices.Equal(got, []string{"auth", "user"}) {
		t.Fatalf("Defaults() = %v", got)
	}
	if got := catalog.Source("user"); got != "core.user" {
		t.Fatalf("Source(user) = %q", got)
	}
}

func TestCatalogRejectsDuplicateModules(t *testing.T) {
	t.Parallel()

	first := module.NewBundle("first", []module.Entry{{ID: "auth", New: func() module.Composable {
		return module.Must(module.Metadata{ID: "auth"})
	}}}, nil)
	second := module.NewBundle("second", []module.Entry{{ID: "auth", New: func() module.Composable {
		return module.Must(module.Metadata{ID: "auth"})
	}}}, nil)

	_, err := module.NewCatalog().Add(first).Add(second).Build()
	if err == nil {
		t.Fatal("Build() error = nil, want duplicate error")
	}
}

func TestCatalogUnknownModule(t *testing.T) {
	t.Parallel()

	catalog := module.NewCatalog().MustBuild()
	_, err := catalog.BuildModule("missing")
	if !errors.Is(err, module.ErrUnknownModule) {
		t.Fatalf("BuildModule() error = %v, want ErrUnknownModule", err)
	}
}
