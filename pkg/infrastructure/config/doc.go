// Implements: REQ-010.
// Per: ADR-0029.
// Discipline: C-14.

// Package config defines the canonical OSS app config for PlatformKit
// and ships a stdlib-only loader (JSON file + environment overrides).
//
// # Why this package exists
//
// Every PlatformKit app needs a small, consistent way to express the
// four infrastructure primitives (HTTP, database, cache) and basic
// lifecycle hints (app name, version, environment). Hand-rolling a
// config struct per app fragments the schema and prevents Pro adapters
// from extending it consistently. This package owns the canonical
// Config struct + DefaultConfig, LoadFromJSON, LoadFromEnv, and
// Validate so any OSS app boots with the same shape.
//
// Callers can extend by embedding Config in their own app config
// struct; Pro adapters can layer YAML / TOML / Vault loaders on top of
// the same Config type without changing this package.
//
// # Block scorecard
//
//   - Identity:        github.com/septagon-oss/pk-core/pkg/infrastructure/config
//   - Boundary:        Config is a value type with public fields so
//     callers construct, mutate, and copy it without
//     reflection or builders.
//   - Contract:        Config struct + DefaultConfig + LoadFromJSON +
//     (*Config).LoadFromEnv + (*Config).Validate.
//   - Composition:     Loaders compose — DefaultConfig sets defaults,
//     LoadFromJSON overlays file values, LoadFromEnv
//     overlays env values, Validate normalizes and checks.
//   - Replacement:     Callers may swap any loader (YAML, TOML, Vault)
//     by reading into a Config; the struct is the contract.
//   - Extension:       Pro adapters embed Config in their own type and
//     add fields without breaking OSS consumers.
//   - Runtime binding: Application composition picks a loader chain at
//     startup; Validate gates the result before wiring.
//   - Evidence:        config_test.go covers defaults, JSON round-trip,
//     missing-field defaulting, env overrides, env empty-
//     value handling, and the Validate environment whitelist.
//
// # Chainable laws
//
//   - Identity:        DefaultConfig() returns a valid Config that passes
//     Validate() once AppName is set.
//   - Type compat:     every loader produces a Config value; chains are
//     just field overlays.
//   - Context preserve: loaders are blocking file/env reads and do not
//     accept a ctx; callers gate startup with their own
//     deadlines if needed.
//   - Error algebra:   LoadFromJSON returns a wrapped fs/json error;
//     Validate returns a sentinel-shaped error describing
//     the first invalid field.
//   - Cancellation:    not applicable — config load is a one-shot
//     synchronous operation.
//
// # External dependencies
//
// None. The contract and loaders are pure stdlib so the kernel stays
// audit-friendly. YAML / TOML / cloud-secret loaders live in Pro
// adapters.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package config
