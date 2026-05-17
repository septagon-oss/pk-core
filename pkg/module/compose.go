package module

// compose.go owns dependency validation and ordering for selected module sets.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-10 (shared builders return errors), C-14 (file purpose declaration).

import (
	"fmt"
	"reflect"
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
	refs, err := normalizeModuleRefs(modules)
	if err != nil {
		return err
	}
	providers, err := providerIndex(refs)
	if err != nil {
		return err
	}
	missing := []string{}
	for _, ref := range refs {
		moduleID := ref.metadata.ID
		for _, dep := range ref.module.Dependencies() {
			if err := validateDependency(dep); err != nil {
				missing = append(missing, fmt.Sprintf("%s declares invalid dependency: %v", moduleID, err))
				continue
			}
			if !dep.Required {
				continue
			}
			candidates, err := compatibleProviders(providers[dep.Port.Name], dep.Port.Version)
			if err != nil {
				missing = append(missing, fmt.Sprintf("%s requires %s with incompatible version declaration: %v", moduleID, dep.Port.Name, err))
				continue
			}
			if len(candidates) == 0 {
				missing = append(missing, fmt.Sprintf("%s requires %s (%s)", moduleID, dep.Port.Name, dep.Purpose))
				continue
			}
			if len(satisfyingProviders(candidates, dep)) == 0 {
				missing = append(missing, fmt.Sprintf("%s requires %s from preferred provider %s", moduleID, dep.Port.Name, strings.TrimSpace(dep.PreferredProvider)))
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
	refs, err := normalizeModuleRefs(modules)
	if err != nil {
		return nil, err
	}
	byID := map[string]Composable{}
	for _, ref := range refs {
		byID[ref.metadata.ID] = ref.module
	}

	providers, err := providerIndex(refs)
	if err != nil {
		return nil, err
	}
	inDegree := map[string]int{}
	dependents := map[string][]string{}
	for id := range byID {
		inDegree[id] = 0
	}

	for _, ref := range refs {
		consumerID := ref.metadata.ID
		for _, dep := range ref.module.Dependencies() {
			if err := validateDependency(dep); err != nil {
				return nil, fmt.Errorf("module %q declares invalid dependency: %w", consumerID, err)
			}
			if !dep.Required {
				continue
			}
			compatible, err := compatibleProviders(providers[dep.Port.Name], dep.Port.Version)
			if err != nil {
				return nil, err
			}
			for _, providerID := range satisfyingProviders(compatible, dep) {
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

	if len(sorted) != len(refs) {
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

type moduleRef struct {
	module   Composable
	metadata Metadata
}

func normalizeModuleRefs(modules []Composable) ([]moduleRef, error) {
	refs := make([]moduleRef, 0, len(modules))
	seen := map[string]struct{}{}
	for i, module := range modules {
		if isNilComposable(module) {
			return nil, fmt.Errorf("module at index %d is nil", i)
		}
		metadata, err := module.Metadata().Normalize()
		if err != nil {
			return nil, err
		}
		if _, exists := seen[metadata.ID]; exists {
			return nil, fmt.Errorf("duplicate module ID %q", metadata.ID)
		}
		seen[metadata.ID] = struct{}{}
		refs = append(refs, moduleRef{module: module, metadata: metadata})
	}
	return refs, nil
}

func isNilComposable(module Composable) bool {
	if module == nil {
		return true
	}
	value := reflect.ValueOf(module)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type providedPort struct {
	moduleID string
	version  string
}

func providerIndex(refs []moduleRef) (map[string][]providedPort, error) {
	providers := map[string][]providedPort{}
	for _, ref := range refs {
		id := ref.metadata.ID
		for _, port := range ref.module.Provides() {
			if strings.TrimSpace(port.Name) == "" {
				return nil, fmt.Errorf("module %q provides invalid port: port name is required", id)
			}
			if err := ValidatePortVersion(PortVersion(port.Version)); err != nil {
				return nil, fmt.Errorf("module %q provides invalid version %q for port %s: %w", id, port.Version, port.Name, err)
			}
			providers[port.Name] = append(providers[port.Name], providedPort{moduleID: id, version: port.Version})
		}
	}
	for port := range providers {
		sort.Slice(providers[port], func(i, j int) bool {
			return providers[port][i].moduleID < providers[port][j].moduleID
		})
	}
	return providers, nil
}

func compatibleProviders(candidates []providedPort, constraint string) ([]string, error) {
	if err := ValidateVersionConstraint(constraint); err != nil {
		return nil, fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ok, err := MatchesVersion(PortVersion(candidate.version), constraint)
		if err != nil {
			return nil, fmt.Errorf("module %q provides invalid version %q for constraint %q: %w", candidate.moduleID, candidate.version, constraint, err)
		}
		if !ok {
			continue
		}
		out = append(out, candidate.moduleID)
	}
	sort.Strings(out)
	return out, nil
}

func validateDependency(dep Dependency) error {
	if strings.TrimSpace(dep.Port.Name) == "" {
		return fmt.Errorf("port name is required")
	}
	if err := ValidateVersionConstraint(dep.Port.Version); err != nil {
		return fmt.Errorf("invalid version constraint %q: %w", dep.Port.Version, err)
	}
	return nil
}

func satisfyingProviders(candidates []string, dep Dependency) []string {
	preferred := strings.TrimSpace(dep.PreferredProvider)
	if preferred != "" {
		if contains(candidates, preferred) {
			return []string{preferred}
		}
		return matchingProviders(candidates, dep.FallbackProviders)
	}

	fallbacks := matchingProviders(candidates, dep.FallbackProviders)
	if len(fallbacks) > 0 {
		return fallbacks
	}
	return candidates
}

func matchingProviders(candidates, allowed []string) []string {
	out := []string{}
	for _, provider := range trimStrings(allowed) {
		if contains(candidates, provider) {
			out = append(out, provider)
		}
	}
	return out
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
