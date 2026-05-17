package module

// compose.go owns dependency validation and ordering for selected module sets.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-10 (shared builders return errors), C-14 (file purpose declaration).

import (
	"cmp"
	"fmt"
	"maps"
	"reflect"
	"slices"
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
		slices.Sort(missing)
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
	choices, err := dependencyChoices(refs, providers, byID)
	if err != nil {
		return nil, err
	}
	moduleIDs := sortedModuleIDs(byID)
	inDegree, dependents, ok := acyclicProviderSelection(moduleIDs, choices)
	if !ok {
		return nil, fmt.Errorf("module dependency cycle detected: %s", strings.Join(moduleIDs, ", "))
	}

	queue := []string{}
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	slices.Sort(queue)

	sorted := []Composable{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, byID[current])

		next := dependents[current]
		slices.Sort(next)
		for _, dependent := range next {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
				slices.Sort(queue)
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
		slices.Sort(cycle)
		return nil, fmt.Errorf("module dependency cycle detected: %s", strings.Join(cycle, ", "))
	}

	return sorted, nil
}

type dependencyChoice struct {
	consumerID  string
	providerIDs []string
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

func dependencyChoices(refs []moduleRef, providers map[string][]providedPort, byID map[string]Composable) ([]dependencyChoice, error) {
	choices := []dependencyChoice{}
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
			satisfying := satisfyingProviders(compatible, dep)
			selected := make([]string, 0, len(satisfying))
			for _, providerID := range satisfying {
				if providerID == consumerID {
					continue
				}
				if _, ok := byID[providerID]; ok {
					selected = append(selected, providerID)
				}
			}
			if len(selected) > 0 {
				choices = append(choices, dependencyChoice{consumerID: consumerID, providerIDs: selected})
			}
		}
	}
	return choices, nil
}

// acyclicProviderSelection treats compatible providers as OR edges: each
// required dependency needs one selected provider, not every compatible provider.
// Backtracking preserves preference order while pruning assignments that already
// form a cycle.
func acyclicProviderSelection(moduleIDs []string, choices []dependencyChoice) (map[string]int, map[string][]string, bool) {
	inDegree := zeroInDegree(moduleIDs)
	dependents := map[string][]string{}

	var choose func(int) bool
	choose = func(index int) bool {
		if !isAcyclic(moduleIDs, inDegree, dependents) {
			return false
		}
		if index == len(choices) {
			return true
		}
		choice := choices[index]
		for _, providerID := range choice.providerIDs {
			inDegree[choice.consumerID]++
			dependents[providerID] = append(dependents[providerID], choice.consumerID)
			if choose(index + 1) {
				return true
			}
			dependents[providerID] = dependents[providerID][:len(dependents[providerID])-1]
			if len(dependents[providerID]) == 0 {
				delete(dependents, providerID)
			}
			inDegree[choice.consumerID]--
		}
		return false
	}

	if !choose(0) {
		return nil, nil, false
	}
	return maps.Clone(inDegree), cloneDependents(dependents), true
}

func isAcyclic(moduleIDs []string, inDegree map[string]int, dependents map[string][]string) bool {
	degrees := maps.Clone(inDegree)
	queue := []string{}
	for _, id := range moduleIDs {
		if degrees[id] == 0 {
			queue = append(queue, id)
		}
	}

	visited := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		visited++
		for _, dependent := range dependents[current] {
			degrees[dependent]--
			if degrees[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}
	return visited == len(moduleIDs)
}

func zeroInDegree(moduleIDs []string) map[string]int {
	inDegree := make(map[string]int, len(moduleIDs))
	for _, id := range moduleIDs {
		inDegree[id] = 0
	}
	return inDegree
}

func sortedModuleIDs(byID map[string]Composable) []string {
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func cloneDependents(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for key, value := range in {
		out[key] = slices.Clone(value)
	}
	return out
}

func isNilComposable(module Composable) bool {
	if module == nil {
		return true
	}
	value := reflect.ValueOf(module)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
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
		slices.SortFunc(providers[port], func(a, b providedPort) int {
			return cmp.Compare(a.moduleID, b.moduleID)
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
	slices.Sort(out)
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
	return slices.Contains(values, want)
}
