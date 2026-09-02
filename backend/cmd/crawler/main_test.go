package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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

// TestStopBudgetFitsSystemd (review issue 38, 04 §14 erratum): everything
// after the first signal has to finish inside systemd's TimeoutStopSec, and
// the unit sets none of its own — so the default 90s is the wall. The old
// numbers were 80s drain + 10s final checkpoint + a 5s shipper drain, which
// is ~95s: SIGKILL landed before the is_final row.
func TestStopBudgetFitsSystemd(t *testing.T) {
	const systemdDefault = 90 * time.Second
	if stopBudget >= systemdDefault {
		t.Errorf("stopBudget %s leaves no margin under systemd's %s", stopBudget, systemdDefault)
	}
	if spent := drainBudget + finalCheckpointBudget; spent >= stopBudget {
		t.Errorf("drain %s + final checkpoint %s = %s, which does not fit stopBudget %s "+
			"with room for the log flush", drainBudget, finalCheckpointBudget, spent, stopBudget)
	}
}

// TestValidatePoolSize (review issue 41): pool sizing is DSN-only, so the
// floor is only enforceable at startup. pgxpool's own default is 4 — what
// an operator who omits pool_max_conns gets — and at 4 the daily tick's
// four nested connections starve everything else.
func TestValidatePoolSize(t *testing.T) {
	for name, tc := range map[string]struct {
		dsn     string
		wantErr bool
	}{
		"pgxpool default is too small": {"postgres://u@localhost/db", true},
		"below the floor":              {"postgres://u@localhost/db?pool_max_conns=15", true},
		"exactly the floor":            {"postgres://u@localhost/db?pool_max_conns=16", false},
		"the documented 32":            {"postgres://u@localhost/db?pool_max_conns=32", false},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := pgxpool.ParseConfig(tc.dsn)
			if err != nil {
				t.Fatal(err)
			}
			// A pool that never dials: ParseConfig fixed MaxConns already.
			pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			if err := validatePoolSize(pool); (err != nil) != tc.wantErr {
				t.Errorf("err = %v, want error: %t", err, tc.wantErr)
			}
		})
	}
}

// TestPreflightAlerter walks the claim loop's alert gate over an outage
// (review issue 12, 04 §11 erratum): one message on the edge, then one per
// preflightAlertInterval while it persists, then one on recovery. Before
// the gate an hour of 60s retries posted 60 identical webhook messages.
func TestPreflightAlerter(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	now := base
	a := &preflightAlerter{now: func() time.Time { return now }}

	if a.recovered() {
		t.Error("a process that starts healthy must announce nothing")
	}
	if !a.failed() {
		t.Error("the healthy→failed edge must alert")
	}
	// An hour of 60s retry cycles: only the 15-minute boundaries speak.
	sent := 0
	for range 59 {
		now = now.Add(time.Minute)
		if a.failed() {
			sent++
		}
	}
	if sent != 3 {
		t.Errorf("repeats over an hour = %d, want 3 (15, 30, 45 min; the 60th lands at 60m)", sent)
	}

	now = now.Add(time.Minute)
	if !a.recovered() {
		t.Error("failed→healthy must post the recovery message")
	}
	if a.recovered() {
		t.Error("recovery must be announced once, not on every healthy cycle")
	}
	// A second outage starts a fresh edge rather than inheriting lastSent.
	if !a.failed() {
		t.Error("a new outage must alert on its own edge")
	}
}
