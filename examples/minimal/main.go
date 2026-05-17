package main

// main.go demonstrates ports-only module composition with the OSS core package.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

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
				module.WithDependencies(module.Require[AuditService](module.DependencySpec{
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
