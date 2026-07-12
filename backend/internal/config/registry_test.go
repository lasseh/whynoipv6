package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// registryRowRe matches one 09-ops §2 registry table line:
// | `key` | `ENV_VAR` | type | default | from | meaning |.
var registryRowRe = regexp.MustCompile("^\\| `([a-z0-9_.]+)` \\| (?:`([A-Z0-9_]+)`|\\(YAML only\\)) \\|")

// bareEnvRowRe matches the §2.1 UPPERCASE deployment keys, which map to
// themselves (| `API_LISTEN` | type | default | binary | from | meaning |).
var bareEnvRowRe = regexp.MustCompile("^\\| `([A-Z0-9_]+)` \\|")

// specRegistryKeys parses the consolidated config registry out of
// docs/spec/09-ops.md §2 — the single source of truth this gate holds the
// code to (09-ops §15.1).
func specRegistryKeys(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "docs", "spec", "09-ops.md")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("spec registry unavailable: %v", err)
	}
	defer func() { _ = f.Close() }()

	keys := map[string]string{} // key → env var ("" = YAML only)
	inRegistry := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## ") {
			inRegistry = strings.Contains(line, "Consolidated config registry") ||
				strings.HasPrefix(line, "## 2")
		}
		if !inRegistry {
			continue
		}
		if m := registryRowRe.FindStringSubmatch(line); m != nil {
			keys[m[1]] = m[2]
		} else if m := bareEnvRowRe.FindStringSubmatch(line); m != nil {
			keys[m[1]] = m[1] // UPPERCASE deployment keys map to themselves
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(keys) < 30 {
		t.Fatalf("parsed only %d registry rows — the table format changed?", len(keys))
	}
	return keys
}

// TestRegistryCompleteness (P7.4 / 09-ops §15.1): every documented key is
// registered with a compiled-in default; every compiled-in key is
// documented; the env-var naming transform holds; no migrate.* keys.
func TestRegistryCompleteness(t *testing.T) {
	spec := specRegistryKeys(t)
	code := registryDefaults("test")

	// Top-level operational env keys registered outside the dotted
	// registry (documented in 09-ops §2.1 as bare env vars).
	for key := range spec {
		if key == "DATABASE_URL" {
			continue // deliberately default-less: Load fails without it
		}
		if _, ok := code[key]; !ok {
			t.Errorf("spec key %q has no compiled-in default (viper.SetDefault)", key)
		}
	}
	for key := range code {
		if strings.HasPrefix(key, "migrate.") {
			t.Errorf("migrate.* keys were dropped from the registry (OPEN-9): %q", key)
		}
		if _, ok := spec[key]; !ok {
			t.Errorf("compiled-in key %q is undocumented in 09-ops §2", key)
		}
	}

	// The env transform: dots → underscores, uppercased (bare deployment
	// keys map to themselves, which the same transform satisfies).
	for key, env := range spec {
		if env == "" {
			continue // YAML only
		}
		want := strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		if env != want {
			t.Errorf("key %q documents env %q; the replacer produces %q", key, env, want)
		}
	}
}

// TestEnvOverrides (09-ops §15.2): the four named env overrides apply with
// no YAML present.
func TestEnvOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/whynoipv6")
	t.Setenv("WORKER_SLOTS", "7")
	t.Setenv("CONSENSUS_PER_PROVIDER_QPS", "3")
	t.Setenv("CRAWLER_RESOURCES_ENABLED", "false")
	t.Setenv("RESOLVER_BULK_UPSTREAMS", "127.0.0.1:5300 127.0.0.1:5301")

	// Run from a directory with no config.yaml.
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	cfg, err := Load("crawler")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Int("worker_slots"); got != 7 {
		t.Errorf("WORKER_SLOTS override: %d", got)
	}
	if got := cfg.Int("consensus.per_provider_qps"); got != 3 {
		t.Errorf("CONSENSUS_PER_PROVIDER_QPS override: %d", got)
	}
	if cfg.Bool("crawler.resources.enabled") {
		t.Error("CRAWLER_RESOURCES_ENABLED override did not apply")
	}
	if got := cfg.StringSlice("resolver.bulk_upstreams"); len(got) != 2 || got[0] != "127.0.0.1:5300" {
		t.Errorf("RESOLVER_BULK_UPSTREAMS override: %v", got)
	}
	_ = fmt.Sprintf("%v", cfg) // the summary path is exercised by LogSummary in Load callers
}
