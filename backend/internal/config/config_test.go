package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:pw@dbhost:5432/whynoipv6")

	cfg, err := Load("crawler")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Int("worker_slots"); got != 64 {
		t.Errorf("worker_slots default = %d, want 64", got)
	}
	if got := cfg.String("claim.order"); got != "rank" {
		t.Errorf("claim.order default = %q, want rank", got)
	}
	if got := cfg.StringSlice("resolver.bulk_upstreams"); len(got) != 2 || got[0] != "127.0.0.1:53" {
		t.Errorf("resolver.bulk_upstreams default = %v", got)
	}
	if cfg.Bool("crawler.resources.enabled") {
		t.Error("crawler.resources.enabled default = true, want false")
	}
	if got := cfg.APIListen; got != "[::1]:8080" {
		t.Errorf("API_LISTEN default = %q, want [::1]:8080", got)
	}

	// Env overrides with no YAML present (09-ops.md §15.2).
	t.Setenv("WORKER_SLOTS", "32")
	t.Setenv("CONSENSUS_PER_PROVIDER_QPS", "7")
	t.Setenv("CRAWLER_RESOURCES_ENABLED", "true")
	t.Setenv("RESOLVER_BULK_UPSTREAMS", "10.0.0.1:53,10.0.0.2:5353")

	cfg, err = Load("crawler")
	if err != nil {
		t.Fatalf("Load with env: %v", err)
	}
	if got := cfg.Int("worker_slots"); got != 32 {
		t.Errorf("WORKER_SLOTS override = %d, want 32", got)
	}
	if got := cfg.Int("consensus.per_provider_qps"); got != 7 {
		t.Errorf("CONSENSUS_PER_PROVIDER_QPS override = %d, want 7", got)
	}
	if !cfg.Bool("crawler.resources.enabled") {
		t.Error("CRAWLER_RESOURCES_ENABLED override not applied")
	}
	if got := cfg.StringSlice("resolver.bulk_upstreams"); len(got) != 2 || got[1] != "10.0.0.2:5353" {
		t.Errorf("RESOLVER_BULK_UPSTREAMS override = %v", got)
	}
}

func TestConfigRequiredDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load("api"); err == nil {
		t.Fatal("Load with empty DATABASE_URL: want error, got nil")
	}
}

func TestConfigRedaction(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://whynoipv6:s3cretpw@dbhost:5432/whynoipv6?sslmode=disable")
	t.Setenv("OPS_WEBHOOK_URL", "https://hooks.example.com/T000/B000/supersecrettoken")

	cfg, err := Load("crawler")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	cfg.LogSummary(log)
	out := buf.String()

	for _, leak := range []string{"s3cretpw", "supersecrettoken", "hooks.example.com"} {
		if strings.Contains(out, leak) {
			t.Errorf("startup summary leaks secret %q", leak)
		}
	}
	if !strings.Contains(out, "postgres://whynoipv6@dbhost:5432/whynoipv6") {
		t.Errorf("summary missing redacted host+db DSN: %s", out)
	}
	if !strings.Contains(out, `"ops.webhook_url":"set"`) {
		t.Errorf("summary should log webhook as set: %s", out)
	}
	if !strings.Contains(out, `"ops.healthcheck_url":"unset"`) {
		t.Errorf("summary should log unset ping URL as unset: %s", out)
	}
}
