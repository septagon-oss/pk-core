package registry

// catalog.go owns immutable declarative registries with deterministic
// ordering, conflict policy, and structured diagnostics.
//
// ADR: ADR-0005 (no silent failures), ADR-0029 (file purpose declaration).
// Convention: C-10 (shared builders return errors), C-14 (file purpose declaration).

import (
	"fmt"
	"sort"
	"strings"
)

// ConflictPolicy controls duplicate keys during catalog construction.
type ConflictPolicy int

const (
	// ConflictReject fails Build when two contributions resolve to the same key.
	ConflictReject ConflictPolicy = iota
	// ConflictFirstWins keeps the first contribution for a duplicate key.
	ConflictFirstWins
	// ConflictLastWins keeps the last contribution for a duplicate key.
	ConflictLastWins
)

// Contribution is a declarative registry contribution with source metadata.
type Contribution[K comparable, V any] struct {
	Key    K
	Value  V
	Source string
}

// Catalog is an immutable registry view built from declarative contributions.
type Catalog[K comparable, V any] struct {
	entries    map[K]Contribution[K, V]
	keys       []K
	normalizer func(K) K
	spec       Spec
	hasSpec    bool
}

// CatalogBuilder gathers contributions and validates them into a Catalog.
type CatalogBuilder[K comparable, V any] struct {
	contributions []Contribution[K, V]
	normalizer    func(K) K
	conflict      ConflictPolicy
	less          func(K, K) bool
	validateKey   func(K) error
	validateValue func(V) error
	spec          Spec
	hasSpec       bool
}

// CatalogOption configures a CatalogBuilder.
type CatalogOption[K comparable, V any] func(*CatalogBuilder[K, V])

// NewCatalogBuilder creates a builder for immutable registry catalogs.
func NewCatalogBuilder[K comparable, V any](opts ...CatalogOption[K, V]) *CatalogBuilder[K, V] {
	b := &CatalogBuilder[K, V]{conflict: ConflictReject}
	for _, opt := range opts {
		if opt != nil {
			opt(b)
		}
	}
	return b
}

// WithCatalogNormalizer normalizes contribution keys before validation.
func WithCatalogNormalizer[K comparable, V any](fn func(K) K) CatalogOption[K, V] {
	return func(b *CatalogBuilder[K, V]) {
		b.normalizer = fn
	}
}

// WithCatalogConflictPolicy sets duplicate-key behavior.
func WithCatalogConflictPolicy[K comparable, V any](policy ConflictPolicy) CatalogOption[K, V] {
	return func(b *CatalogBuilder[K, V]) {
		b.conflict = policy
	}
}

// WithCatalogKeyLess sets deterministic key ordering for Keys and Entries.
func WithCatalogKeyLess[K comparable, V any](less func(K, K) bool) CatalogOption[K, V] {
	return func(b *CatalogBuilder[K, V]) {
		b.less = less
	}
}

// WithCatalogKeyValidator sets a validation hook for normalized keys.
func WithCatalogKeyValidator[K comparable, V any](fn func(K) error) CatalogOption[K, V] {
	return func(b *CatalogBuilder[K, V]) {
		b.validateKey = fn
	}
}

// WithCatalogValueValidator sets a validation hook for contribution values.
func WithCatalogValueValidator[K comparable, V any](fn func(V) error) CatalogOption[K, V] {
	return func(b *CatalogBuilder[K, V]) {
		b.validateValue = fn
	}
}

// WithCatalogSpec attaches stable registry metadata to the built catalog.
func WithCatalogSpec[K comparable, V any](spec Spec) CatalogOption[K, V] {
	return func(b *CatalogBuilder[K, V]) {
		b.spec = spec
		b.hasSpec = true
	}
}

// Add appends one contribution.
func (b *CatalogBuilder[K, V]) Add(key K, value V, source string) *CatalogBuilder[K, V] {
	if b == nil {
		return b
	}
	b.contributions = append(b.contributions, Contribution[K, V]{
		Key:    key,
		Value:  value,
		Source: strings.TrimSpace(source),
	})
	return b
}

// AddContribution appends a contribution.
func (b *CatalogBuilder[K, V]) AddContribution(c Contribution[K, V]) *CatalogBuilder[K, V] {
	if b == nil {
		return b
	}
	return b.Add(c.Key, c.Value, c.Source)
}

// Build validates contributions and returns an immutable Catalog.
func (b *CatalogBuilder[K, V]) Build() (*Catalog[K, V], error) {
	if b == nil {
		return &Catalog[K, V]{entries: map[K]Contribution[K, V]{}}, nil
	}
	if err := validateConflictPolicy(b.conflict); err != nil {
		return nil, err
	}
	spec, hasSpec, err := b.normalizedSpec()
	if err != nil {
		return nil, err
	}
	entries := make(map[K]Contribution[K, V], len(b.contributions))
	order := make([]K, 0, len(b.contributions))

	for _, contribution := range b.contributions {
		key := contribution.Key
		if b.normalizer != nil {
			key = b.normalizer(key)
		}
		if b.validateKey != nil {
			if err := b.validateKey(key); err != nil {
				return nil, diagnosticError(Diagnostic{
					Severity:   SeverityError,
					Code:       CodeInvalidKey,
					RegistryID: spec.ID,
					Key:        fmt.Sprint(key),
					Source:     contribution.Source,
					Message:    fmt.Sprintf("invalid key: %v", err),
				})
			}
		}
		if b.validateValue != nil {
			if err := b.validateValue(contribution.Value); err != nil {
				return nil, diagnosticError(Diagnostic{
					Severity:   SeverityError,
					Code:       CodeInvalidValue,
					RegistryID: spec.ID,
					Key:        fmt.Sprint(key),
					Source:     contribution.Source,
					Message:    fmt.Sprintf("invalid value: %v", err),
				})
			}
		}

		normalized := Contribution[K, V]{
			Key:    key,
			Value:  contribution.Value,
			Source: contribution.Source,
		}
		if existing, ok := entries[key]; ok {
			switch b.conflict {
			case ConflictReject:
				return nil, diagnosticError(Diagnostic{
					Severity:   SeverityError,
					Code:       CodeDuplicateKey,
					RegistryID: spec.ID,
					Key:        fmt.Sprint(key),
					Source:     contribution.Source,
					Message:    fmt.Sprintf("duplicate key already contributed by %q", existing.Source),
				})
			case ConflictFirstWins:
				continue
			case ConflictLastWins:
				entries[key] = normalized
				continue
			default:
				return nil, fmt.Errorf("registry catalog: unknown conflict policy %d", b.conflict)
			}
		}
		entries[key] = normalized
		order = append(order, key)
	}

	if b.less != nil {
		sort.SliceStable(order, func(i, j int) bool {
			return b.less(order[i], order[j])
		})
	}

	return &Catalog[K, V]{
		entries:    entries,
		keys:       append([]K(nil), order...),
		normalizer: b.normalizer,
		spec:       spec,
		hasSpec:    hasSpec,
	}, nil
}

// MustBuild returns Build's catalog or panics.
func (b *CatalogBuilder[K, V]) MustBuild() *Catalog[K, V] {
	catalog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return catalog
}

// Len returns the number of contributions in the catalog.
func (c *Catalog[K, V]) Len() int {
	if c == nil {
		return 0
	}
	return len(c.entries)
}

// Spec returns the registry metadata attached to the catalog, when present.
func (c *Catalog[K, V]) Spec() (Spec, bool) {
	if c == nil || !c.hasSpec {
		return Spec{}, false
	}
	return c.spec, true
}

// Has reports whether key exists.
func (c *Catalog[K, V]) Has(key K) bool {
	if c == nil {
		return false
	}
	key = c.normalize(key)
	_, ok := c.entries[key]
	return ok
}

// Lookup returns the value for key.
func (c *Catalog[K, V]) Lookup(key K) (V, bool) {
	var zero V
	if c == nil {
		return zero, false
	}
	key = c.normalize(key)
	entry, ok := c.entries[key]
	if !ok {
		return zero, false
	}
	return entry.Value, true
}

// Contribution returns the full contribution for key.
func (c *Catalog[K, V]) Contribution(key K) (Contribution[K, V], bool) {
	if c == nil {
		return Contribution[K, V]{}, false
	}
	key = c.normalize(key)
	entry, ok := c.entries[key]
	return entry, ok
}

// Source returns the contribution source for key.
func (c *Catalog[K, V]) Source(key K) string {
	if c == nil {
		return ""
	}
	key = c.normalize(key)
	return c.entries[key].Source
}

// Keys returns contribution keys in builder order or WithCatalogKeyLess order.
func (c *Catalog[K, V]) Keys() []K {
	if c == nil {
		return nil
	}
	return append([]K(nil), c.keys...)
}

// Entries returns contributions in builder order or WithCatalogKeyLess order.
func (c *Catalog[K, V]) Entries() []Contribution[K, V] {
	if c == nil {
		return nil
	}
	out := make([]Contribution[K, V], 0, len(c.keys))
	for _, key := range c.keys {
		out = append(out, c.entries[key])
	}
	return out
}

func (c *Catalog[K, V]) normalize(key K) K {
	if c != nil && c.normalizer != nil {
		return c.normalizer(key)
	}
	return key
}

func validateConflictPolicy(policy ConflictPolicy) error {
	switch policy {
	case ConflictReject, ConflictFirstWins, ConflictLastWins:
		return nil
	default:
		return diagnosticError(Diagnostic{
			Severity: SeverityError,
			Code:     CodeInvalidConflictPolicy,
			Message:  fmt.Sprintf("unknown conflict policy %d", policy),
		})
	}
}

func (b *CatalogBuilder[K, V]) normalizedSpec() (Spec, bool, error) {
	if !b.hasSpec {
		return Spec{}, false, nil
	}
	spec, err := b.spec.Normalize()
	if err != nil {
		return Spec{}, true, err
	}
	return spec, true, nil
}

// All returns a shallow copy of key-to-value entries.
func (c *Catalog[K, V]) All() map[K]V {
	if c == nil {
		return nil
	}
	out := make(map[K]V, len(c.entries))
	for key, entry := range c.entries {
		out[key] = entry.Value
	}
	return out
}
