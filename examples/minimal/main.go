// Implements: REQ-002.
// Per: ADR-0009.
// Discipline: C-14.

package main

import (
	"fmt"
	"log"

	"github.com/septagon-oss/pk-core/pkg/module"
)

type AuditService interface {
	Record(message string) error
}

func main() {
	audit := module.NewBundle("example.audit", []module.Entry{
		{ID: "audit", New: func() module.Composable {
			return module.Must(
				module.Metadata{ID: "audit", Name: "Audit"},
				module.WithProvides(module.Provide[AuditService]("1.0.0")),
			)
		}},
	}, []string{"audit"})

	content := module.NewBundle("example.content", []module.Entry{
		{ID: "content", New: func() module.Composable {
			return module.Must(
				module.Metadata{ID: "content", Name: "Content"},
				module.WithDependencies(module.RequiresPort[AuditService](module.PortSpec{
					Version:           "1.0.0",
					Purpose:           "audit content publication",
					PreferredProvider: "audit",
				})),
			)
		}},
	}, []string{"content"})

	catalog := module.NewCatalog().Add(content).Add(audit).MustBuild()
	plan, err := module.Compose(catalog)
	if err != nil {
		log.Fatal(err)
	}

	for _, module := range plan.Modules {
		fmt.Println(module.Metadata().ID)
	}
}
