package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lasseh/whynoipv6/internal/campaign"
	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/config"
	"github.com/lasseh/whynoipv6/internal/consensus"
	"github.com/lasseh/whynoipv6/internal/crawler"
	"github.com/lasseh/whynoipv6/internal/ingest"
)

// TestConfigBinding drives every ConfigFrom constructor through the real
// registry: the config getters panic on unregistered keys, so a typo'd key
// in any binding fails this test instead of resolving to a silent zero
// value at runtime.
func TestConfigBinding(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u@localhost/whynoipv6")
	cfg, err := config.Load("crawler")
	if err != nil {
		t.Fatal(err)
	}
	_ = consensus.ConfigFrom(cfg)
	_ = checker.ConfigFrom(cfg)
	_ = crawler.CommitConfigFrom(cfg)
	_ = crawler.FrontierConfigFrom(cfg)
	_ = crawler.TickConfigFrom(cfg)
	_ = crawler.LiveCheckConfigFrom(cfg)
	_ = campaign.ConfigFrom(cfg)
	_ = ingest.TrancoConfigFrom(cfg)
	if _, err := crawler.BandsFrom(cfg); err != nil {
		t.Fatal(err)
	}
}

// TestBandsFromYAML proves the YAML-only cadence.bands shape (09-ops §2.7)
// decodes through the real registry — mapstructure tags plus viper's
// duration hook — and that ValidateBands gates it.
func TestBandsFromYAML(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u@localhost/whynoipv6")
	dir := t.TempDir()
	yaml := "cadence:\n  bands:\n    - {min_rank: 1, max_rank: 1000, every: 6h}\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	cfg, err := config.Load("crawler")
	if err != nil {
		t.Fatal(err)
	}
	bands, err := crawler.BandsFrom(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := crawler.Band{MinRank: 1, MaxRank: 1000, Every: 6 * time.Hour}
	if len(bands) != 1 || bands[0] != want {
		t.Errorf("bands = %+v, want [%+v]", bands, want)
	}
}

// TestBandsFromRejectsMalformed: a zero-every band aborts instead of being
// silently ignored (04 §4 Decision).
func TestBandsFromRejectsMalformed(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u@localhost/whynoipv6")
	dir := t.TempDir()
	yaml := "cadence:\n  bands:\n    - {min_rank: 1000, max_rank: 1, every: 6h}\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	cfg, err := config.Load("crawler")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := crawler.BandsFrom(cfg); err == nil {
		t.Fatal("BandsFrom accepted min_rank > max_rank")
	}
}
