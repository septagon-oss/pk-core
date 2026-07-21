// Validates: REQ-002.
// Per: ADR-0009.
// Discipline: C-14.

package module_test

import (
	"fmt"

	"github.com/septagon-oss/pk-core/pkg/module"
)

// Notifier is an example port: an interface one module provides and another
// consumes, without either module importing the other.
type Notifier interface {
	Notify(message string) error
}

// Example composes two modules through a shared port. The content module
// declares a required dependency on Notifier; the notifications module provides
// it. Compose validates the dependency and returns the modules in dependency
// order (provider before consumer).
func Example() {
	notifications := module.NewBundle("example.notifications", []module.Entry{
		{ID: "notifications", New: func() module.Composable {
			return module.Must(
				module.Metadata{ID: "notifications", Name: "Notifications"},
				module.WithProvides(module.Provide[Notifier]("1.0.0")),
			)
		}},
	}, []string{"notifications"})

	content := module.NewBundle("example.content", []module.Entry{
		{ID: "content", New: func() module.Composable {
			return module.Must(
				module.Metadata{ID: "content", Name: "Content"},
				module.WithDependencies(module.RequiresPort[Notifier](module.PortSpec{
					Version:           "1.0.0",
					Purpose:           "notify subscribers on publish",
					PreferredProvider: "notifications",
				})),
			)
		}},
	}, []string{"content"})

	catalog := module.NewCatalog().Add(content).Add(notifications).MustBuild()
	plan, err := module.Compose(catalog)
	if err != nil {
		fmt.Println("compose error:", err)
		return
	}

	for _, m := range plan.Modules {
		fmt.Println(m.Metadata().ID)
	}
	// Output:
	// notifications
	// content
}
