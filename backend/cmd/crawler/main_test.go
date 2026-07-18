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
}
