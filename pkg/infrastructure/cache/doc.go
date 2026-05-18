// Package cache defines the provider-neutral key-value cache contract for
// the PlatformKit OSS kernel and ships an in-process default backed by
// sync.Map + a doubly-linked LRU list.
//
// # Why this package exists
//
// Modules need a small, swappable cache primitive for hot-path lookups,
// rate-limit counters, and ephemeral session metadata. Wiring Redis,
// Memcached, or Ristretto directly into every module fragments behavior
// and chains every module to a specific provider. This package owns the
// three-method contract (Get / Set / Delete) so call sites code against
// the interface; Pro adapters (Redis, Ristretto, distributed caches)
// plug in by implementing the same Cache type.
//
// # Block scorecard
//
//   - Identity:        github.com/septagon-oss/pk-core/pkg/infrastructure/cache
//   - Boundary:        Cache is a 3-method interface; the in-memory default
//     is an unexported struct returned through the interface
//     so callers cannot reach into its internals.
//   - Contract:        Cache { Get, Set, Delete } + Memory constructor +
//     functional options (WithMaxEntries, WithSweepInterval).
//   - Composition:     Caches stack — middleware can wrap a Cache to add
//     metrics, tracing, or write-through layers without
//     changing the contract.
//   - Replacement:     Any type implementing Cache is a drop-in.
//   - Extension:       Pro / downstream adapters publish their own Cache
//     constructors; the interface is the only coupling.
//   - Runtime binding: Application composition picks the constructor —
//     typically NewMemory in OSS, redis.New in hosted.
//   - Evidence:        cache_test.go exercises round-trip, TTL expiry,
//     Delete, LRU eviction, lazy reap, sweep reap, and
//     concurrent access under -race.
//
// # Chainable laws
//
//   - Identity:        a no-op wrapper that delegates to an inner Cache is
//     a valid composition element.
//   - Type compat:     every Cache exposes the same Get / Set / Delete
//     shape so wrappers compose without adapters.
//   - Context preserve: Get / Set / Delete accept a ctx so middleware can
//     propagate deadlines, traces, and cancellation.
//   - Error algebra:   methods return error; the in-memory default never
//     returns an error today but the contract reserves the
//     position so remote adapters can surface failures.
//   - Cancellation:    callers honor ctx.Done() at their own discretion;
//     the OSS in-memory default short-circuits when ctx is
//     already cancelled at entry.
//
// # External dependencies
//
// None. The contract and in-memory default are pure stdlib so the kernel
// stays audit-friendly.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package cache
