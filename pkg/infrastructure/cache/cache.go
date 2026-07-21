// Implements: REQ-014.
// Per: ADR-0029.
// Discipline: C-14.

// Package cache — cache.go owns the public surface of the cache contract:
// the Cache interface that every implementation satisfies, the Memory
// constructor + MemoryOption functional-option type, and the option
// constructors (WithMaxEntries, WithSweepInterval).
//
// Implementations live in this same package (memory.go) so the OSS default
// ships out of the box; Pro adapters live in downstream modules and plug
// in by implementing Cache without importing memory.go.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package cache

import (
	"context"
	"time"
)

// Cache is the provider-neutral key-value cache contract.
//
// Implementations MUST be safe for concurrent use. The bytes returned from
// Get are owned by the caller; implementations MUST NOT retain references
// to the slice passed into Set after the call returns (defensive copy or
// equivalent semantics).
//
// The error return on Get / Set / Delete is reserved for remote adapters
// (Redis, Memcached, distributed caches) that can fail at the transport
// layer; the in-memory default returns nil unless the context is already
// cancelled at entry.
type Cache interface {
	// Get returns the value stored at key. ok is false when the key is
	// absent or has expired; err is non-nil only when the implementation
	// encountered a transport/storage failure.
	Get(ctx context.Context, key string) (value []byte, ok bool, err error)

	// Set stores value at key for the given ttl. A ttl of zero means the
	// entry never expires; negative ttls are treated as zero.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes the entry at key. Deleting a missing key is a no-op
	// and returns nil.
	Delete(ctx context.Context, key string) error
}

// MemoryOption configures an in-memory Cache at construction time.
type MemoryOption func(*memoryConfig)

// memoryConfig holds the resolved Options. Fields are unexported so
// downstream code cannot mutate the cache mid-flight.
type memoryConfig struct {
	maxEntries    int
	sweepInterval time.Duration
}

// defaultMaxEntries is the eviction bound when WithMaxEntries is not
// supplied. 10k is large enough for typical hot-path lookups and small
// enough that an accidental in-memory unbounded cache stays under a few
// megabytes of overhead.
const defaultMaxEntries = 10000

// defaultSweepInterval is the background reaper period. One minute is a
// pragmatic balance between wasted CPU and memory left behind by expired
// entries between accesses; tune via WithSweepInterval if a particular
// workload needs tighter or looser reaping.
const defaultSweepInterval = time.Minute

// WithMaxEntries caps the number of live entries the in-memory cache
// retains. When the cap is reached, the least-recently-used entry is
// evicted to make room for the new write. A value <= 0 disables the
// cap (unlimited entries — use with care).
func WithMaxEntries(n int) MemoryOption {
	return func(c *memoryConfig) {
		c.maxEntries = n
	}
}

// WithSweepInterval overrides how often the background goroutine sweeps
// expired entries. A non-positive value disables the background sweep
// entirely; entries are then reaped only lazily on Get.
func WithSweepInterval(d time.Duration) MemoryOption {
	return func(c *memoryConfig) {
		c.sweepInterval = d
	}
}
