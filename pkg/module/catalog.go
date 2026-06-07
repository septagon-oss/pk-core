package module

// catalog.go owns bundle-to-catalog composition so OSS and Pro module packs
// can contribute modules through the same deterministic lookup surface.
//
// ADR: ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-10 (shared builders return errors), C-14 (file purpose declaration).

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Constructor builds a module instance.
type Constructor func() Composable

// Entry is one module contribution to a Bundle.
type Entry struct {
	ID  string
	New Constructor
}

// Bundle is the public extension point for registering modules.
type Bundle interface {
	Name() string
	Entries() []Entry
	Defaults() []string
}

// StaticBundle is a simple Bundle implementation.
type StaticBundle struct {
	name     string
	entries  []Entry
	defaults []string
}

// NewBundle creates a StaticBundle.
func NewBundle(name string, entries []Entry, defaults []string) StaticBundle {
	return StaticBundle{
		name:     strings.TrimSpace(name),
		entries:  slices.Clone(entries),
		defaults: slices.Clone(defaults),
	}
}

// Name returns the bundle's name.
func (b StaticBundle) Name() string {
	return b.name
}

// Entries returns a copy of the bundle's module entries.
func (b StaticBundle) Entries() []Entry {
	return slices.Clone(b.entries)
}

// Defaults returns a copy of the bundle's default module IDs.
func (b StaticBundle) Defaults() []string {
	return slices.Clone(b.defaults)
}

// ConflictPolicy controls duplicate module IDs during catalog composition.
type ConflictPolicy int

// Conflict policies for duplicate module IDs during composition.
const (
	// ConflictReject fails the build when two bundles contribute the same ID.
	ConflictReject ConflictPolicy = iota
	// ConflictFirstWins keeps the first contribution and ignores later ones.
	ConflictFirstWins
	// ConflictLastWins lets later contributions overwrite earlier ones.
	ConflictLastWins
)

// CatalogBuilder composes Bundles into a Catalog.
type CatalogBuilder struct {
	bundles []Bundle
	policy  ConflictPolicy
}

// NewCatalog starts a catalog builder.
func NewCatalog() *CatalogBuilder {
	return &CatalogBuilder{}
}

// Add appends a Bundle.
func (b *CatalogBuilder) Add(bundle Bundle) *CatalogBuilder {
	if b == nil {
		return b
	}
	if bundle != nil {
		b.bundles = append(b.bundles, bundle)
	}
	return b
}

// AddAll appends multiple Bundles.
func (b *CatalogBuilder) AddAll(bundles ...Bundle) *CatalogBuilder {
	if b == nil {
		return b
	}
	for _, bundle := range bundles {
		b.Add(bundle)
	}
	return b
}

// WithConflictPolicy sets duplicate handling.
func (b *CatalogBuilder) WithConflictPolicy(policy ConflictPolicy) *CatalogBuilder {
	if b == nil {
		return b
	}
	b.policy = policy
	return b
}

// Catalog is an immutable lookup surface for composed modules.
type Catalog struct {
	entries  map[string]Entry
	defaults []string
	sources  map[string]string
}

// Build resolves the catalog.
func (b *CatalogBuilder) Build() (*Catalog, error) {
	if b == nil {
		return &Catalog{
			entries: map[string]Entry{},
			sources: map[string]string{},
		}, nil
	}
	catalog := &Catalog{
		entries: map[string]Entry{},
		sources: map[string]string{},
	}
	defaults := []string{}
	seenDefault := map[string]struct{}{}

	for _, bundle := range b.bundles {
		name := strings.TrimSpace(bundle.Name())
		if name == "" {
			return nil, fmt.Errorf("module catalog: bundle name is required")
		}

		for _, entry := range bundle.Entries() {
			id := strings.TrimSpace(entry.ID)
			if id == "" {
				return nil, fmt.Errorf("module catalog: bundle %q has entry with empty ID", name)
			}
			if entry.New == nil {
				return nil, fmt.Errorf("module catalog: bundle %q entry %q has nil constructor", name, id)
			}
			if _, exists := catalog.entries[id]; exists {
				switch b.policy {
				case ConflictReject:
					return nil, fmt.Errorf("module catalog: module %q contributed by both %q and %q", id, catalog.sources[id], name)
				case ConflictFirstWins:
					continue
				case ConflictLastWins:
				default:
					return nil, fmt.Errorf("module catalog: unknown conflict policy %d", b.policy)
				}
			}
			catalog.entries[id] = Entry{ID: id, New: entry.New}
			catalog.sources[id] = name
		}

		for _, id := range bundle.Defaults() {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, exists := seenDefault[id]; exists {
				continue
			}
			seenDefault[id] = struct{}{}
			defaults = append(defaults, id)
		}
	}

	catalog.defaults = defaults
	return catalog, nil
}

// MustBuild resolves the catalog or panics.
//
// Panics if Build returns an error (for example, a bundle with an empty name,
// an entry with an empty ID or nil constructor, or a duplicate module ID under
// the ConflictReject policy). Use Build to handle these conditions as errors.
func (b *CatalogBuilder) MustBuild() *Catalog {
	catalog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return catalog
}

// ModuleIDs returns every module ID in sorted order.
func (c *Catalog) ModuleIDs() []string {
	if c == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(c.entries))
}

// Defaults returns the bundle-declared default module IDs.
func (c *Catalog) Defaults() []string {
	if c == nil {
		return nil
	}
	return slices.Clone(c.defaults)
}

// HasModule reports whether id is cataloged.
func (c *Catalog) HasModule(id string) bool {
	if c == nil {
		return false
	}
	_, ok := c.entries[strings.TrimSpace(id)]
	return ok
}

// Source returns the bundle name that contributed id.
func (c *Catalog) Source(id string) string {
	if c == nil {
		return ""
	}
	return c.sources[strings.TrimSpace(id)]
}

// Lookup returns a catalog entry.
func (c *Catalog) Lookup(id string) (Entry, bool) {
	if c == nil {
		return Entry{}, false
	}
	entry, ok := c.entries[strings.TrimSpace(id)]
	return entry, ok
}

// ErrUnknownModule is returned when a module ID is not cataloged.
var ErrUnknownModule = errors.New("unknown module")

// BuildModule constructs a module by ID.
func (c *Catalog) BuildModule(id string) (Composable, error) {
	entry, ok := c.Lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownModule, id)
	}
	module := entry.New()
	if module == nil {
		return nil, fmt.Errorf("module catalog: constructor for %q returned nil", id)
	}
	metadata, err := module.Metadata().Normalize()
	if err != nil {
		return nil, fmt.Errorf("module catalog: module %q returned invalid metadata: %w", id, err)
	}
	if metadata.ID != id {
		return nil, fmt.Errorf("module catalog: entry %q constructed module with metadata ID %q", id, metadata.ID)
	}
	return module, nil
}
