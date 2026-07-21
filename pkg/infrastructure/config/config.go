// Implements: REQ-010.
// Per: ADR-0029.
// Discipline: C-14.

// Package config — config.go owns the public surface of the config
// contract: the Config struct + nested HTTP/Database/Cache sections,
// the DefaultConfig constructor, the LoadFromJSON loader, the
// LoadFromEnv environment overlay, and the Validate normalizer.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the canonical OSS app config. Fields cover the four
// infrastructure primitives (HTTP, database, cache) and basic lifecycle
// hints (AppName, AppVersion, Environment). Callers can extend by
// embedding Config in their own app config struct.
type Config struct {
	AppName     string `json:"appName"`
	AppVersion  string `json:"appVersion"`
	Environment string `json:"environment"` // "development" | "staging" | "production"

	HTTP     HTTPConfig     `json:"http"`
	Database DatabaseConfig `json:"database"`
	Cache    CacheConfig    `json:"cache"`
}

// HTTPConfig holds the canonical HTTP server settings.
type HTTPConfig struct {
	Addr            string        `json:"addr"`
	ReadTimeout     time.Duration `json:"readTimeout"`
	WriteTimeout    time.Duration `json:"writeTimeout"`
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
}

// DatabaseConfig holds the canonical database settings.
type DatabaseConfig struct {
	Driver string `json:"driver"` // "sqlite", "postgres", "mysql"
	DSN    string `json:"dsn"`
}

// CacheConfig holds the canonical cache settings.
type CacheConfig struct {
	Provider string `json:"provider"` // "memory", "redis"
	Addr     string `json:"addr"`     // for remote providers
}

// Recognized environment values. Validate restricts Environment to
// this set so accidental typos ("prod", "dev", "Production") fail
// fast at startup.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Default values. Constants make defaults inspectable without
// constructing a Config.
const (
	defaultHTTPAddr            = ":8080"
	defaultHTTPReadTimeout     = 30 * time.Second
	defaultHTTPWriteTimeout    = 30 * time.Second
	defaultHTTPShutdownTimeout = 30 * time.Second

	defaultDatabaseDriver = "sqlite"
	defaultDatabaseDSN    = "file::memory:?cache=shared"

	defaultCacheProvider = "memory"

	defaultAppVersion  = "0.0.0"
	defaultEnvironment = EnvDevelopment
)

// DefaultConfig returns a Config with sane defaults: sqlite in-memory
// DSN, in-process memory cache, :8080 with 30s timeouts, and
// environment "development". AppName is left empty so callers MUST
// set it (Validate refuses empty AppName).
func DefaultConfig() Config {
	return Config{
		AppName:     "",
		AppVersion:  defaultAppVersion,
		Environment: defaultEnvironment,
		HTTP: HTTPConfig{
			Addr:            defaultHTTPAddr,
			ReadTimeout:     defaultHTTPReadTimeout,
			WriteTimeout:    defaultHTTPWriteTimeout,
			ShutdownTimeout: defaultHTTPShutdownTimeout,
		},
		Database: DatabaseConfig{
			Driver: defaultDatabaseDriver,
			DSN:    defaultDatabaseDSN,
		},
		Cache: CacheConfig{
			Provider: defaultCacheProvider,
		},
	}
}

// LoadFromJSON reads path and unmarshals its contents on top of
// DefaultConfig. Missing fields in the JSON keep their default values;
// present fields overlay the defaults verbatim.
//
// Duration fields accept JSON numbers (nanoseconds, per encoding/json's
// default) AND duration strings ("30s", "5m"). The custom unmarshal is
// implemented via a transient typed alias to keep the public API tidy.
func LoadFromJSON(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config.LoadFromJSON: read %s: %w", path, err)
	}
	cfg := DefaultConfig()
	if err := unmarshalJSON(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("config.LoadFromJSON: parse %s: %w", path, err)
	}
	return cfg, nil
}

// unmarshalJSON deserializes raw into cfg, accepting durations as
// either JSON numbers (ns) or strings ("30s"). The two-pass shape
// lets us keep Config's public field types as time.Duration while
// supporting human-friendly JSON.
func unmarshalJSON(raw []byte, cfg *Config) error {
	// First pass: durations as RawMessage so we can decide per-field.
	type httpRaw struct {
		Addr            string          `json:"addr"`
		ReadTimeout     json.RawMessage `json:"readTimeout"`
		WriteTimeout    json.RawMessage `json:"writeTimeout"`
		ShutdownTimeout json.RawMessage `json:"shutdownTimeout"`
	}
	type configRaw struct {
		AppName     *string         `json:"appName"`
		AppVersion  *string         `json:"appVersion"`
		Environment *string         `json:"environment"`
		HTTP        *httpRaw        `json:"http"`
		Database    *DatabaseConfig `json:"database"`
		Cache       *CacheConfig    `json:"cache"`
	}
	var cr configRaw
	if err := json.Unmarshal(raw, &cr); err != nil {
		return err
	}
	if cr.AppName != nil {
		cfg.AppName = *cr.AppName
	}
	if cr.AppVersion != nil {
		cfg.AppVersion = *cr.AppVersion
	}
	if cr.Environment != nil {
		cfg.Environment = *cr.Environment
	}
	if cr.HTTP != nil {
		if cr.HTTP.Addr != "" {
			cfg.HTTP.Addr = cr.HTTP.Addr
		}
		if d, ok, err := parseDuration(cr.HTTP.ReadTimeout); err != nil {
			return fmt.Errorf("http.readTimeout: %w", err)
		} else if ok {
			cfg.HTTP.ReadTimeout = d
		}
		if d, ok, err := parseDuration(cr.HTTP.WriteTimeout); err != nil {
			return fmt.Errorf("http.writeTimeout: %w", err)
		} else if ok {
			cfg.HTTP.WriteTimeout = d
		}
		if d, ok, err := parseDuration(cr.HTTP.ShutdownTimeout); err != nil {
			return fmt.Errorf("http.shutdownTimeout: %w", err)
		} else if ok {
			cfg.HTTP.ShutdownTimeout = d
		}
	}
	if cr.Database != nil {
		if cr.Database.Driver != "" {
			cfg.Database.Driver = cr.Database.Driver
		}
		if cr.Database.DSN != "" {
			cfg.Database.DSN = cr.Database.DSN
		}
	}
	if cr.Cache != nil {
		if cr.Cache.Provider != "" {
			cfg.Cache.Provider = cr.Cache.Provider
		}
		if cr.Cache.Addr != "" {
			cfg.Cache.Addr = cr.Cache.Addr
		}
	}
	return nil
}

// parseDuration accepts a JSON value that is either a number (ns) or a
// string parseable by time.ParseDuration. Empty/missing returns ok=false.
func parseDuration(raw json.RawMessage) (time.Duration, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false, nil
	}
	// Try string first.
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, false, err
		}
		if s == "" {
			return 0, false, nil
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, false, err
		}
		return d, true, nil
	}
	// Otherwise treat as number of nanoseconds.
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false, err
	}
	return time.Duration(n), true, nil
}

// Environment variable names. Centralized so docs + tests share the
// same authoritative list.
const (
	EnvVarAppName             = "PK_APP_NAME"
	EnvVarAppVersion          = "PK_APP_VERSION"
	EnvVarEnvironment         = "PK_ENVIRONMENT"
	EnvVarHTTPAddr            = "PK_HTTP_ADDR"
	EnvVarHTTPReadTimeout     = "PK_HTTP_READ_TIMEOUT"
	EnvVarHTTPWriteTimeout    = "PK_HTTP_WRITE_TIMEOUT"
	EnvVarHTTPShutdownTimeout = "PK_HTTP_SHUTDOWN_TIMEOUT"
	EnvVarDatabaseDriver      = "PK_DATABASE_DRIVER"
	EnvVarDatabaseDSN         = "PK_DATABASE_DSN"
	EnvVarCacheProvider       = "PK_CACHE_PROVIDER"
	EnvVarCacheAddr           = "PK_CACHE_ADDR"
)

// LoadFromEnv overlays environment-variable overrides onto cfg in
// place. Empty env values are ignored (so unset variables and
// explicit empty strings are treated identically). Duration variables
// accept either an integer (nanoseconds) or a string parseable by
// time.ParseDuration; unparseable values are silently ignored to
// avoid panicking on stray test fixtures.
func (cfg *Config) LoadFromEnv() {
	if v := os.Getenv(EnvVarAppName); v != "" {
		cfg.AppName = v
	}
	if v := os.Getenv(EnvVarAppVersion); v != "" {
		cfg.AppVersion = v
	}
	if v := os.Getenv(EnvVarEnvironment); v != "" {
		cfg.Environment = v
	}
	if v := os.Getenv(EnvVarHTTPAddr); v != "" {
		cfg.HTTP.Addr = v
	}
	if d, ok := parseEnvDuration(EnvVarHTTPReadTimeout); ok {
		cfg.HTTP.ReadTimeout = d
	}
	if d, ok := parseEnvDuration(EnvVarHTTPWriteTimeout); ok {
		cfg.HTTP.WriteTimeout = d
	}
	if d, ok := parseEnvDuration(EnvVarHTTPShutdownTimeout); ok {
		cfg.HTTP.ShutdownTimeout = d
	}
	if v := os.Getenv(EnvVarDatabaseDriver); v != "" {
		cfg.Database.Driver = v
	}
	if v := os.Getenv(EnvVarDatabaseDSN); v != "" {
		cfg.Database.DSN = v
	}
	if v := os.Getenv(EnvVarCacheProvider); v != "" {
		cfg.Cache.Provider = v
	}
	if v := os.Getenv(EnvVarCacheAddr); v != "" {
		cfg.Cache.Addr = v
	}
}

func parseEnvDuration(name string) (time.Duration, bool) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return 0, false
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d, true
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Duration(n), true
	}
	return 0, false
}

// Validation errors.

// ErrInvalidConfig is the sentinel returned by Validate for any
// invalid Config. Callers wrap on errors.Is to branch on it.
var ErrInvalidConfig = errors.New("config: invalid configuration")

// Validate normalizes cfg in place and returns an error if any
// required field is missing or invalid. The current rules:
//
//   - AppName must be non-empty
//   - HTTP.Addr must be non-empty
//   - Database.Driver must be non-empty
//   - Environment must be one of EnvDevelopment, EnvStaging, EnvProduction
//
// Validate is idempotent: calling it twice produces the same result.
func (cfg *Config) Validate() error {
	if cfg == nil {
		return fmt.Errorf("%w: nil receiver", ErrInvalidConfig)
	}
	if strings.TrimSpace(cfg.AppName) == "" {
		return fmt.Errorf("%w: AppName must be non-empty", ErrInvalidConfig)
	}
	if strings.TrimSpace(cfg.HTTP.Addr) == "" {
		return fmt.Errorf("%w: HTTP.Addr must be non-empty", ErrInvalidConfig)
	}
	if strings.TrimSpace(cfg.Database.Driver) == "" {
		return fmt.Errorf("%w: Database.Driver must be non-empty", ErrInvalidConfig)
	}
	switch cfg.Environment {
	case EnvDevelopment, EnvStaging, EnvProduction:
		// OK.
	default:
		return fmt.Errorf("%w: Environment %q must be one of development/staging/production",
			ErrInvalidConfig, cfg.Environment)
	}
	return nil
}
