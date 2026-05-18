// Package config_test exercises the config contract from outside the
// package: it imports only the published surface (Config + nested
// structs, DefaultConfig, LoadFromJSON, LoadFromEnv, Validate, sentinel
// errors) and never reaches into unexported state.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/septagon-oss/pk-core/pkg/infrastructure/config"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func TestDefaultConfigHasSaneDefaults(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()

	if cfg.AppName != "" {
		t.Errorf("AppName default = %q, want empty (caller must set)", cfg.AppName)
	}
	if cfg.AppVersion == "" {
		t.Error("AppVersion default is empty")
	}
	if cfg.Environment != config.EnvDevelopment {
		t.Errorf("Environment default = %q, want %q", cfg.Environment, config.EnvDevelopment)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("HTTP.Addr default = %q, want %q", cfg.HTTP.Addr, ":8080")
	}
	if cfg.HTTP.ReadTimeout != 30*time.Second {
		t.Errorf("HTTP.ReadTimeout default = %v, want 30s", cfg.HTTP.ReadTimeout)
	}
	if cfg.HTTP.WriteTimeout != 30*time.Second {
		t.Errorf("HTTP.WriteTimeout default = %v, want 30s", cfg.HTTP.WriteTimeout)
	}
	if cfg.HTTP.ShutdownTimeout != 30*time.Second {
		t.Errorf("HTTP.ShutdownTimeout default = %v, want 30s", cfg.HTTP.ShutdownTimeout)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Errorf("Database.Driver default = %q, want sqlite", cfg.Database.Driver)
	}
	if cfg.Database.DSN == "" {
		t.Error("Database.DSN default is empty")
	}
	if cfg.Cache.Provider != "memory" {
		t.Errorf("Cache.Provider default = %q, want memory", cfg.Cache.Provider)
	}
}

func TestLoadFromJSONFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeFile(t, dir, "cfg.json", `{
        "appName": "demo",
        "appVersion": "1.2.3",
        "environment": "production",
        "http": {
            "addr": ":9090",
            "readTimeout": "15s",
            "writeTimeout": "20s",
            "shutdownTimeout": "5s"
        },
        "database": {
            "driver": "postgres",
            "dsn": "postgres://localhost/db"
        },
        "cache": {
            "provider": "redis",
            "addr": "redis://localhost:6379"
        }
    }`)

	cfg, err := config.LoadFromJSON(path)
	if err != nil {
		t.Fatalf("LoadFromJSON: %v", err)
	}
	if cfg.AppName != "demo" {
		t.Errorf("AppName = %q, want demo", cfg.AppName)
	}
	if cfg.AppVersion != "1.2.3" {
		t.Errorf("AppVersion = %q, want 1.2.3", cfg.AppVersion)
	}
	if cfg.Environment != config.EnvProduction {
		t.Errorf("Environment = %q, want production", cfg.Environment)
	}
	if cfg.HTTP.Addr != ":9090" {
		t.Errorf("HTTP.Addr = %q, want :9090", cfg.HTTP.Addr)
	}
	if cfg.HTTP.ReadTimeout != 15*time.Second {
		t.Errorf("HTTP.ReadTimeout = %v, want 15s", cfg.HTTP.ReadTimeout)
	}
	if cfg.HTTP.WriteTimeout != 20*time.Second {
		t.Errorf("HTTP.WriteTimeout = %v, want 20s", cfg.HTTP.WriteTimeout)
	}
	if cfg.HTTP.ShutdownTimeout != 5*time.Second {
		t.Errorf("HTTP.ShutdownTimeout = %v, want 5s", cfg.HTTP.ShutdownTimeout)
	}
	if cfg.Database.Driver != "postgres" {
		t.Errorf("Database.Driver = %q, want postgres", cfg.Database.Driver)
	}
	if cfg.Database.DSN != "postgres://localhost/db" {
		t.Errorf("Database.DSN = %q", cfg.Database.DSN)
	}
	if cfg.Cache.Provider != "redis" {
		t.Errorf("Cache.Provider = %q, want redis", cfg.Cache.Provider)
	}
	if cfg.Cache.Addr != "redis://localhost:6379" {
		t.Errorf("Cache.Addr = %q", cfg.Cache.Addr)
	}
}

func TestLoadFromJSONMissingFieldsKeepDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Only set AppName; everything else should retain DefaultConfig.
	path := writeFile(t, dir, "cfg.json", `{"appName": "only-name"}`)

	cfg, err := config.LoadFromJSON(path)
	if err != nil {
		t.Fatalf("LoadFromJSON: %v", err)
	}
	if cfg.AppName != "only-name" {
		t.Errorf("AppName = %q, want only-name", cfg.AppName)
	}
	def := config.DefaultConfig()
	if cfg.HTTP.Addr != def.HTTP.Addr {
		t.Errorf("HTTP.Addr = %q, want default %q", cfg.HTTP.Addr, def.HTTP.Addr)
	}
	if cfg.Database.Driver != def.Database.Driver {
		t.Errorf("Database.Driver = %q, want default %q", cfg.Database.Driver, def.Database.Driver)
	}
	if cfg.Environment != def.Environment {
		t.Errorf("Environment = %q, want default %q", cfg.Environment, def.Environment)
	}
}

func TestLoadFromJSONInvalidErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeFile(t, dir, "cfg.json", `{this is not json`)

	_, err := config.LoadFromJSON(path)
	if err == nil {
		t.Fatal("LoadFromJSON: nil error on invalid JSON")
	}
}

func TestLoadFromJSONMissingFileErrors(t *testing.T) {
	t.Parallel()
	_, err := config.LoadFromJSON("/nonexistent/no-such-file.json")
	if err == nil {
		t.Fatal("LoadFromJSON: nil error on missing file")
	}
}

func TestLoadFromEnvOverridesScalars(t *testing.T) {
	// Not parallel: mutates process env.
	cfg := config.DefaultConfig()

	t.Setenv(config.EnvVarAppName, "from-env")
	t.Setenv(config.EnvVarAppVersion, "9.9.9")
	t.Setenv(config.EnvVarEnvironment, config.EnvStaging)
	t.Setenv(config.EnvVarHTTPAddr, ":7070")
	t.Setenv(config.EnvVarHTTPReadTimeout, "11s")
	t.Setenv(config.EnvVarDatabaseDriver, "mysql")
	t.Setenv(config.EnvVarDatabaseDSN, "user@/db")
	t.Setenv(config.EnvVarCacheProvider, "redis")
	t.Setenv(config.EnvVarCacheAddr, "redis://r:6379")

	cfg.LoadFromEnv()

	if cfg.AppName != "from-env" {
		t.Errorf("AppName = %q", cfg.AppName)
	}
	if cfg.AppVersion != "9.9.9" {
		t.Errorf("AppVersion = %q", cfg.AppVersion)
	}
	if cfg.Environment != config.EnvStaging {
		t.Errorf("Environment = %q", cfg.Environment)
	}
	if cfg.HTTP.Addr != ":7070" {
		t.Errorf("HTTP.Addr = %q", cfg.HTTP.Addr)
	}
	if cfg.HTTP.ReadTimeout != 11*time.Second {
		t.Errorf("HTTP.ReadTimeout = %v", cfg.HTTP.ReadTimeout)
	}
	if cfg.Database.Driver != "mysql" {
		t.Errorf("Database.Driver = %q", cfg.Database.Driver)
	}
	if cfg.Database.DSN != "user@/db" {
		t.Errorf("Database.DSN = %q", cfg.Database.DSN)
	}
	if cfg.Cache.Provider != "redis" {
		t.Errorf("Cache.Provider = %q", cfg.Cache.Provider)
	}
	if cfg.Cache.Addr != "redis://r:6379" {
		t.Errorf("Cache.Addr = %q", cfg.Cache.Addr)
	}
}

func TestLoadFromEnvIgnoresEmpty(t *testing.T) {
	// Not parallel: mutates process env.
	cfg := config.DefaultConfig()
	cfg.AppName = "pre-set"
	cfg.HTTP.Addr = ":1234"

	t.Setenv(config.EnvVarAppName, "")
	t.Setenv(config.EnvVarHTTPAddr, "")

	cfg.LoadFromEnv()

	if cfg.AppName != "pre-set" {
		t.Errorf("AppName mutated by empty env: %q", cfg.AppName)
	}
	if cfg.HTTP.Addr != ":1234" {
		t.Errorf("HTTP.Addr mutated by empty env: %q", cfg.HTTP.Addr)
	}
}

func TestValidateAcceptsDefaultsWithAppName(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.AppName = "demo"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsEmptyAppName(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate: nil error on empty AppName")
	}
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Errorf("err is not ErrInvalidConfig: %v", err)
	}
}

func TestValidateRejectsInvalidEnvironment(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.AppName = "demo"
	cfg.Environment = "prod" // not in the whitelist
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate: nil error on bad environment")
	}
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Errorf("err is not ErrInvalidConfig: %v", err)
	}
}

func TestValidateRejectsEmptyHTTPAddr(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.AppName = "demo"
	cfg.HTTP.Addr = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate: nil error on empty HTTP.Addr")
	}
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Errorf("err is not ErrInvalidConfig: %v", err)
	}
}

func TestValidateRejectsEmptyDatabaseDriver(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.AppName = "demo"
	cfg.Database.Driver = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate: nil error on empty Database.Driver")
	}
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Errorf("err is not ErrInvalidConfig: %v", err)
	}
}

func TestValidateNilReceiver(t *testing.T) {
	t.Parallel()
	var cfg *config.Config
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate: nil error on nil receiver")
	}
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Errorf("err is not ErrInvalidConfig: %v", err)
	}
}
