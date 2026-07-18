// Package config is the two-tier viper loader shared by all three binaries
// (09-ops.md §1): compiled-in default < optional /etc/whynoipv6/config.yaml
// < environment variable. It also installs the slog handler per the logging
// conventions (09-ops.md §13).
package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/lasseh/taillight/pkg/logshipper"
	"github.com/spf13/viper"
)

// secretKeys are logged as set/unset in the startup summary, never by value.
var secretKeys = map[string]bool{
	"ops.webhook_url":          true,
	"ops.healthcheck_url":      true,
	"ops.healthcheck_tick_url": true,
	"taillight.api_key":        true,
}

// Config is the resolved configuration of one binary. Global deployment keys
// are typed fields; the nested tuning sections are read through the accessor
// methods by their registry key (09-ops.md §2).
type Config struct {
	Binary        string
	DatabaseURL   string
	APIListen     string
	GeoIPPath     string
	DatasetsDir   string
	PublicBaseURL string
	LogLevel      slog.Level

	v        *viper.Viper
	registry map[string]any
}

// Load builds the configuration for the named binary (api, crawler, v6ctl).
// A missing YAML file is normal; a missing DATABASE_URL is a fatal error.
func Load(binary string) (*Config, error) {
	v := viper.New()
	for key, def := range registryDefaults(binary) {
		v.SetDefault(key, def)
	}
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("/etc/whynoipv6")
	v.AddConfigPath(".") // dev
	if err := v.ReadInConfig(); err != nil {
		var nf viper.ConfigFileNotFoundError
		if !errors.As(err, &nf) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// Absent config file is normal in production — defaults + env only.
	}
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	dsn := v.GetString("DATABASE_URL")
	if dsn == "" {
		return nil, errors.New("DATABASE_URL is required (postgres://USER:PASS@HOST:5432/whynoipv6)")
	}
	lvl, err := parseLevel(v.GetString("LOG_LEVEL"))
	if err != nil {
		return nil, err
	}
	return &Config{
		Binary:        binary,
		DatabaseURL:   dsn,
		APIListen:     v.GetString("API_LISTEN"),
		GeoIPPath:     v.GetString("GEOIP_PATH"),
		DatasetsDir:   v.GetString("DATASETS_DIR"),
		PublicBaseURL: v.GetString("PUBLIC_BASE_URL"),
		LogLevel:      lvl,
		v:             v,
		registry:      registryDefaults(binary),
	}, nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("LOG_LEVEL: unknown level %q (debug|info|warn|error)", s)
	}
}

// know panics on a key outside the registry: every read happens once
// during startup wiring, so a typo'd key must fail fast there instead of
// resolving to a silent zero value.
func (c *Config) know(key string) {
	if _, ok := c.registry[key]; !ok {
		panic(fmt.Sprintf("config: unregistered key %q", key))
	}
}

// String returns the value of a registry key.
func (c *Config) String(key string) string { c.know(key); return c.v.GetString(key) }

// Int returns the value of a registry key.
func (c *Config) Int(key string) int { c.know(key); return c.v.GetInt(key) }

// Bool returns the value of a registry key.
func (c *Config) Bool(key string) bool { c.know(key); return c.v.GetBool(key) }

// Float returns the value of a registry key.
func (c *Config) Float(key string) float64 { c.know(key); return c.v.GetFloat64(key) }

// Duration returns the value of a registry key.
func (c *Config) Duration(key string) time.Duration { c.know(key); return c.v.GetDuration(key) }

// StringSlice returns the value of a list registry key; env overrides are
// comma-separated (09-ops.md §1).
func (c *Config) StringSlice(key string) []string {
	c.know(key)
	if s := c.v.GetString(key); strings.Contains(s, ",") {
		parts := strings.Split(s, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	return c.v.GetStringSlice(key)
}

// Keys returns every registered registry key, sorted (registry test surface).
func (c *Config) Keys() []string {
	keys := make([]string, 0, len(c.registry))
	for k := range c.registry {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// InstallLogger installs the process-wide slog default per 09-ops.md §13:
// JSON handler, stdout for api/crawler, stderr for v6ctl, component attr
// stamped on the local handler. When taillight.url is set, records also fan
// out to a Taillight log shipper; the returned flush drains its buffer and
// must be called on shutdown (no-op when shipping is off). A malformed
// taillight.url is fatal, like every other misconfiguration.
func (c *Config) InstallLogger() (*slog.Logger, func(), error) {
	w := os.Stdout
	if c.Binary == "v6ctl" {
		w = os.Stderr
	}
	// component goes on the handler, not the logger, so the shipper carries
	// it as its first-class field instead of a duplicated attr.
	local := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: c.LogLevel}).
		WithAttrs([]slog.Attr{slog.String("component", c.Binary)})

	handler := local
	flush := func() {}
	if endpoint := c.String("taillight.url"); endpoint != "" {
		shipper, err := logshipper.New(logshipper.Config{
			Endpoint:     endpoint,
			APIKey:       logshipper.Secret(c.String("taillight.api_key")),
			Service:      "whynoipv6",
			Component:    c.Binary,
			MinLevel:     c.LogLevel,
			MaxAttrBytes: 16384,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("taillight.url: %w", err)
		}
		handler = logshipper.MultiHandler(local, shipper)
		flush = func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shipper.Shutdown(ctx); err != nil {
				slog.New(local).Warn("taillight flush failed", "err", err.Error())
			}
			if n := shipper.Dropped(); n > 0 {
				slog.New(local).Warn("taillight entries dropped (buffer full)", "count", n)
			}
		}
	}
	log := slog.New(handler)
	slog.SetDefault(log)
	return log, flush, nil
}

// LogSummary emits the info-level startup config summary: every registry key
// with its resolved value, secrets redacted (09-ops.md §1, §15.3).
func (c *Config) LogSummary(log *slog.Logger) {
	attrs := make([]any, 0, 2*len(c.registry)+2)
	attrs = append(attrs, "DATABASE_URL", redactDSN(c.DatabaseURL))
	for _, key := range c.Keys() {
		val := c.v.Get(key)
		if secretKeys[key] {
			val = "unset"
			if c.v.GetString(key) != "" {
				val = "set"
			}
		}
		attrs = append(attrs, key, val)
	}
	log.Info("configuration", attrs...)
}

// redactDSN reduces a pgx DSN to user@host/db — no password, no params.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "invalid"
	}
	redacted := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
	if u.User != nil {
		redacted.User = url.User(u.User.Username())
	}
	return redacted.String()
}
