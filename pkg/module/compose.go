package module

import (
	"fmt"
	"sort"
	"strings"
)

// Plan is the dependency-ordered result of composing modules from a catalog.
type Plan struct {
	Modules     []Composable
	Providers   []any
	Invocations []any
}

// Compose builds and validates the requested modules. When ids is empty, the
// catalog defaults are used.
func Compose(catalog *Catalog, ids ...string) (*Plan, error) {
	if catalog == nil {
		return nil, fmt.Errorf("compose: nil catalog")
	}
	if len(ids) == 0 {
		ids = catalog.Defaults()
	}
	ids = normalizeIDs(ids)
	if len(ids) == 0 {
		return &Plan{}, nil
	}

	modules := make([]Composable, 0, len(ids))
	for _, id := range ids {
		module, err := catalog.BuildModule(id)
		if err != nil {
			return nil, err
		}
		modules = append(modules, module)
	}

	sorted, err := Sort(modules)
	if err != nil {
		return nil, err
	}
	if err := Validate(sorted); err != nil {
		return nil, err
	}

	plan := &Plan{Modules: sorted}
	for _, module := range sorted {
		plan.Providers = append(plan.Providers, module.Providers()...)
		plan.Invocations = append(plan.Invocations, module.Invocations()...)
	}
	return plan, nil
}

func normalizeIDs(ids []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// Validate checks that required dependencies are provided by the selected
// module set.
func Validate(modules []Composable) error {
	providers := providerIndex(modules)
	missing := []string{}
	for _, module := range modules {
		moduleID := module.Metadata().ID
		for _, dep := range module.Dependencies() {
			if !dep.Required {
				continue
			}
			candidates := providers[dep.Port.Name]
			if len(candidates) == 0 {
				missing = append(missing, fmt.Sprintf("%s requires %s (%s)", moduleID, dep.Port.Name, dep.Purpose))
				continue
			}
			if dep.PreferredProvider != "" && !contains(candidates, dep.PreferredProvider) {
				missing = append(missing, fmt.Sprintf("%s requires %s from preferred provider %s", moduleID, dep.Port.Name, dep.PreferredProvider))
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("module dependency validation failed:\n  - %s", strings.Join(missing, "\n  - "))
	}
	return nil
}

// Sort returns modules in dependency order.
func Sort(modules []Composable) ([]Composable, error) {
	if len(modules) < 2 {
		return append([]Composable(nil), modules...), nil
	}

	byID := map[string]Composable{}
	for _, module := range modules {
		metadata, err := module.Metadata().Normalize()
		if err != nil {
			return nil, err
		}
		if _, exists := byID[metadata.ID]; exists {
			return nil, fmt.Errorf("duplicate module ID %q", metadata.ID)
		}
		byID[metadata.ID] = module
	}

	providers := providerIndex(modules)
	inDegree := map[string]int{}
	dependents := map[string][]string{}
	for id := range byID {
		inDegree[id] = 0
	}

	for _, module := range modules {
		consumerID := module.Metadata().ID
		for _, dep := range module.Dependencies() {
			for _, providerID := range providers[dep.Port.Name] {
				if providerID == consumerID {
					continue
				}
				if _, selected := byID[providerID]; !selected {
					continue
				}
				inDegree[consumerID]++
				dependents[providerID] = append(dependents[providerID], consumerID)
			}
		}
	}

	queue := []string{}
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	sorted := []Composable{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, byID[current])

		next := dependents[current]
		sort.Strings(next)
		for _, dependent := range next {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
				sort.Strings(queue)
			}
		}
	}

	if len(sorted) != len(modules) {
		cycle := []string{}
		for id, degree := range inDegree {
			if degree > 0 {
				cycle = append(cycle, id)
			}
		}
		sort.Strings(cycle)
		return nil, fmt.Errorf("module dependency cycle detected: %s", strings.Join(cycle, ", "))
	}

	return sorted, nil
}

func providerIndex(modules []Composable) map[string][]string {
	providers := map[string][]string{}
	for _, module := range modules {
		id := module.Metadata().ID
		for _, port := range module.Provides() {
			if port.Name == "" {
				continue
			}
			providers[port.Name] = append(providers[port.Name], id)
		}
	}
	for port := range providers {
		sort.Strings(providers[port])
	}
	return providers
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
