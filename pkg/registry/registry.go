// Package registry provides the small, reusable registry substrate used by
// PlatformKit core and extension layers.
package registry

// registry.go owns the generic in-memory registry primitive used by higher
// level declarative catalogs and extension points.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// Registry is a thread-safe key-value registry.
//
// Register intentionally overwrites an existing key, matching ordinary map
// assignment semantics. Use RegisterIfAbsent, CatalogBuilder, or a
// domain-specific registry when duplicate ownership must fail closed.
type Registry[K comparable, V any] struct {
	mu          sync.RWMutex
	entries     map[K]V
	normalizer  func(K) K
	fallback    V
	hasFallback bool
}

// Option configures a Registry.
type Option[K comparable, V any] func(*Registry[K, V])

// WithDefault sets the fallback value returned by MustGet for missing keys.
func WithDefault[K comparable, V any](v V) Option[K, V] {
	return func(r *Registry[K, V]) {
		r.fallback = v
		r.hasFallback = true
	}
}

// WithNormalizer applies fn before every lookup and registration.
func WithNormalizer[K comparable, V any](fn func(K) K) Option[K, V] {
	return func(r *Registry[K, V]) {
		r.normalizer = fn
	}
}

// New creates a Registry.
func New[K comparable, V any](opts ...Option[K, V]) *Registry[K, V] {
	r := &Registry[K, V]{
		entries: make(map[K]V),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

// Register adds or overwrites a value.
func (r *Registry[K, V]) Register(key K, value V) {
	if r == nil {
		return
	}
	key = r.normalize(key)
	r.mu.Lock()
	r.ensureEntriesLocked()
	r.entries[key] = value
	r.mu.Unlock()
}

// ErrAlreadyRegistered is returned when a strict registration sees an existing key.
type ErrAlreadyRegistered[K comparable] struct {
	Key K
}

func (e ErrAlreadyRegistered[K]) Error() string {
	return fmt.Sprintf("registry: key %v already registered", e.Key)
}

// RegisterIfAbsent adds a value and fails if the key already exists.
func (r *Registry[K, V]) RegisterIfAbsent(key K, value V) error {
	if r == nil {
		return fmt.Errorf("registry: nil registry")
	}
	key = r.normalize(key)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureEntriesLocked()
	if _, ok := r.entries[key]; ok {
		return ErrAlreadyRegistered[K]{Key: key}
	}
	r.entries[key] = value
	return nil
}

// Delete removes key and reports whether it existed.
func (r *Registry[K, V]) Delete(key K) bool {
	if r == nil {
		return false
	}
	key = r.normalize(key)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[key]; !ok {
		return false
	}
	delete(r.entries, key)
	return true
}

// Get returns the value and true, or the zero value and false.
func (r *Registry[K, V]) Get(key K) (V, bool) {
	var zero V
	if r == nil {
		return zero, false
	}
	key = r.normalize(key)
	r.mu.RLock()
	v, ok := r.entries[key]
	r.mu.RUnlock()
	return v, ok
}

// MustGet returns the value for key, the configured fallback, or the zero value.
func (r *Registry[K, V]) MustGet(key K) V {
	v, ok := r.Get(key)
	if ok {
		return v
	}
	if r != nil && r.hasFallback {
		return r.fallback
	}
	return v
}

// Has reports whether a key exists.
func (r *Registry[K, V]) Has(key K) bool {
	_, ok := r.Get(key)
	return ok
}

// All returns a shallow copy of all entries.
func (r *Registry[K, V]) All() map[K]V {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[K]V, len(r.entries))
	for k, v := range r.entries {
		out[k] = v
	}
	return out
}

// Keys returns all registered keys.
func (r *Registry[K, V]) Keys() []K {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]K, 0, len(r.entries))
	for k := range r.entries {
		keys = append(keys, k)
	}
	return keys
}

// Len returns the number of entries.
func (r *Registry[K, V]) Len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// Range iterates over a snapshot of entries. The callback never runs under the
// registry lock, so callbacks can safely call back into the registry.
func (r *Registry[K, V]) Range(fn func(K, V) bool) {
	if r == nil || fn == nil {
		return
	}
	for k, v := range r.All() {
		if !fn(k, v) {
			return
		}
	}
}

// SortedKeys returns keys sorted by less.
func SortedKeys[K comparable, V any](r *Registry[K, V], less func(a, b K) bool) []K {
	keys := r.Keys()
	if less == nil {
		return keys
	}
	sort.Slice(keys, func(i, j int) bool {
		return less(keys[i], keys[j])
	})
	return keys
}

func (r *Registry[K, V]) normalize(key K) K {
	if r != nil && r.normalizer != nil {
		return r.normalizer(key)
	}
	return key
}

func (r *Registry[K, V]) ensureEntriesLocked() {
	if r.entries == nil {
		r.entries = make(map[K]V)
	}
}

// IsNil reports whether v is nil. It handles typed nil pointers, maps, slices,
// channels, funcs, and interfaces without relying on string formatting.
func IsNil[T any](v T) bool {
	value := reflect.ValueOf(v)
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
