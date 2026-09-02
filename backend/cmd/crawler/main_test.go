package main

import (
	"testing"

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
	// The startup bounds now include the campaign checkout, which lives at
	// /srv/whynoipv6-campaign in production and nowhere on a test machine.
	t.Setenv("CAMPAIGN_REPO_PATH", t.TempDir())
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
	if err := validateBounds(cfg); err != nil {
		t.Errorf("the defaults fail the startup bounds: %v", err)
	}
}

// TestValidateBounds: a zero for one of the counted keys is a startup
// error, not a crawler that runs and does nothing.
func TestValidateBounds(t *testing.T) {
	for _, env := range []string{"WORKER_SLOTS", "CLAIM_BATCH_SIZE", "CHECKS_MAX_NS_LOOKUPS", "CONSENSUS_PER_PROVIDER_QPS"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://u@localhost/whynoipv6")
			t.Setenv("CAMPAIGN_REPO_PATH", t.TempDir())
			t.Setenv(env, "0")
			cfg, err := config.Load("crawler")
			if err != nil {
				t.Fatal(err)
			}
			if err := validateBounds(cfg); err == nil {
				t.Errorf("%s=0 passed the startup bounds", env)
			}
		})
	}
}
