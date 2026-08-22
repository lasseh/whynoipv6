package main

import (
	"testing"

	"github.com/lasseh/whynoipv6/internal/campaign"
	"github.com/lasseh/whynoipv6/internal/config"
	"github.com/lasseh/whynoipv6/internal/ingest"
)

// TestConfigBinding drives every registry key v6ctl reads through the real
// registry — see cmd/crawler's twin for the rationale.
func TestConfigBinding(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u@localhost/whynoipv6")
	cfg, err := config.Load("v6ctl")
	if err != nil {
		t.Fatal(err)
	}
	_ = cfg.String("unbound_stats.control")
	_ = cfg.Duration("tranco.stale_warn_after")
	_ = campaign.ConfigFrom(cfg)
	_ = ingest.TrancoConfigFrom(cfg)
}
