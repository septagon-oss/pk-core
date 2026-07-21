// Implements: REQ-014.
// Per: ADR-0029.
// Discipline: C-14.

// Package cache — memory.go implements the in-process default Cache.
//
// Storage is a map[string]*entry guarded by a sync.Mutex. A doubly-linked
// list tracks LRU order so eviction is O(1). Each entry carries an
// expiresAt timestamp; Get reaps lazily, and an optional background
// goroutine sweeps the table on a ticker.
//
// The implementation deliberately avoids sync.Map: tracking LRU order
// requires consistent ordering under both reads (touch-on-Get moves an
// entry to MRU) and writes, which sync.Map's atomic semantics do not
// model cheaply. A single mutex keeps the code obvious and the contract
// tight; the LRU list and the map are mutated together under the same
// lock.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package cache

import (
	"context"
	"sync"
	"time"
)

// NewMemory returns an in-process Cache backed by a mutex-guarded map +
// LRU list. Without options the cache caps at defaultMaxEntries entries
// and sweeps expired entries every defaultSweepInterval. Pass options to
// override either bound.
func NewMemory(opts ...MemoryOption) Cache {
	cfg := memoryConfig{
		maxEntries:    defaultMaxEntries,
		sweepInterval: defaultSweepInterval,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	m := &memoryCache{
		cfg:     cfg,
		entries: make(map[string]*entry),
	}
	m.lru.init()

	if cfg.sweepInterval > 0 {
		m.stop = make(chan struct{})
		m.sweepWG.Add(1)
		go m.sweepLoop()
	}
	return m
}

// entry is one cache record. expiresAt zero means "never expires".
//
// The entry doubles as a node in the LRU list: prev/next link it to its
// neighbors. Embedding the list node avoids a second allocation per Set.
type entry struct {
	key       string
	value     []byte
	expiresAt time.Time

	prev *entry
	next *entry
}

// list is a sentinel-headed doubly-linked list. head.next is MRU,
// head.prev is LRU. Using a sentinel removes branch cases for "list is
// empty" — the list is "empty" when head.next == &head.
type list struct {
	head entry
}

func (l *list) init() {
	l.head.next = &l.head
	l.head.prev = &l.head
}

// pushFront inserts e at the MRU position.
func (l *list) pushFront(e *entry) {
	e.prev = &l.head
	e.next = l.head.next
	l.head.next.prev = e
	l.head.next = e
}

// remove unlinks e from the list. Safe to call on a node already removed
// (no-op).
func (l *list) remove(e *entry) {
	if e.prev == nil || e.next == nil {
		return
	}
	e.prev.next = e.next
	e.next.prev = e.prev
	e.prev = nil
	e.next = nil
}

// moveToFront promotes e to MRU.
func (l *list) moveToFront(e *entry) {
	l.remove(e)
	l.pushFront(e)
}

// back returns the LRU entry, or nil if the list is empty.
func (l *list) back() *entry {
	if l.head.prev == &l.head {
		return nil
	}
	return l.head.prev
}

// memoryCache is the in-process Cache implementation.
type memoryCache struct {
	cfg memoryConfig

	mu      sync.Mutex
	entries map[string]*entry
	lru     list

	// Sweep-loop lifecycle. stop is nil when no background sweep runs.
	stop    chan struct{}
	sweepWG sync.WaitGroup
}

// Get returns the value stored at key, honoring TTL.
func (m *memoryCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.entries[key]
	if !ok {
		return nil, false, nil
	}
	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		// Lazy reap: drop the expired entry and report miss.
		m.removeLocked(e)
		return nil, false, nil
	}
	// Touch: move to MRU.
	m.lru.moveToFront(e)
	// Defensive copy so callers cannot mutate stored bytes.
	out := make([]byte, len(e.value))
	copy(out, e.value)
	return out, true, nil
}

// Set stores value at key for ttl.
func (m *memoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Defensive copy: callers retain ownership of the input slice.
	stored := make([]byte, len(value))
	copy(stored, value)

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	if e, ok := m.entries[key]; ok {
		// Update in place; refresh LRU position.
		e.value = stored
		e.expiresAt = expiresAt
		m.lru.moveToFront(e)
		return nil
	}

	e := &entry{
		key:       key,
		value:     stored,
		expiresAt: expiresAt,
	}
	m.entries[key] = e
	m.lru.pushFront(e)

	// Enforce max-entries: evict LRU until under the cap.
	if m.cfg.maxEntries > 0 {
		for len(m.entries) > m.cfg.maxEntries {
			victim := m.lru.back()
			if victim == nil {
				break
			}
			m.removeLocked(victim)
		}
	}
	return nil
}

// Delete removes the entry at key.
func (m *memoryCache) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.entries[key]; ok {
		m.removeLocked(e)
	}
	return nil
}

// removeLocked drops e from both the map and the LRU list. Caller MUST
// hold m.mu.
func (m *memoryCache) removeLocked(e *entry) {
	delete(m.entries, e.key)
	m.lru.remove(e)
}

// sweepLoop periodically reaps expired entries. It exits when m.stop is
// closed.
func (m *memoryCache) sweepLoop() {
	defer m.sweepWG.Done()
	ticker := time.NewTicker(m.cfg.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.sweep()
		}
	}
}

// sweep removes every expired entry in one pass. It walks the LRU list
// from tail to head; entries without TTL are skipped.
func (m *memoryCache) sweep() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	// Iterate via the map: order does not matter for sweeping.
	for _, e := range m.entries {
		if !e.expiresAt.IsZero() && now.After(e.expiresAt) {
			m.removeLocked(e)
		}
	}
}
