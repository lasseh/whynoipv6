package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/campaign"
	"github.com/lasseh/whynoipv6/internal/lock"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// tickAt is the daily-tick fire time, UTC — a compile-time constant, not
// config (04 §9). Tests invoke Tick.Run directly.
const tickAt = "03:30"

// v6ctlLockWait is the hardcoded blocking-lock wait for explicitly
// requested singleton runs (04 §10).
const v6ctlLockWait = 5 * time.Minute

// TickConfig carries the tick's knobs (registry: 09-ops.md).
type TickConfig struct {
	Sweep              SweepConfig
	IndegreeThreshold  int32         // service_detect.indegree_threshold, 100
	LiveCheckRetention time.Duration // live_check.retention, 720h
}

// Tick is the daily-tick coordinator (04 §9): the canonical step sequence
// with per-step failure containment, run under the JobDailyTick lock.
type Tick struct {
	Pool     *pgxpool.Pool
	Cfg      TickConfig
	Campaign campaign.Config

	// Notify posts the step-7 ops summary (nil = disabled).
	Notify func(ctx context.Context, msg string)
	// PingTick pings the daily-tick healthchecks.io check (nil = disabled);
	// called only when steps 1–3 all succeeded.
	PingTick func(ctx context.Context)
}

// Run executes tick steps 1–7 in canonical order. A failing step logs at
// error level and CONTINUES — the tick never aborts mid-sequence. The
// caller holds JobDailyTick (or is v6ctl's break-glass path).
func (t *Tick) Run(ctx context.Context) error {
	var failed []string
	fail := func(step string, err error) {
		failed = append(failed, step)
		slog.Error("tick step failed", "step", step, "err", err.Error())
	}

	// Step 1 — lifecycle sweep.
	if _, err := Sweep(ctx, t.Pool, t.Cfg.Sweep); err != nil {
		fail("lifecycle_sweep", err)
	}

	// Steps 2+3 — stats snapshot + counter recompute.
	if err := RunStatsRollup(ctx, t.Pool); err != nil {
		fail("stats_rollup", err)
	}

	// Step 4 — service-candidate detection (candidates only).
	q := db.New(t.Pool)
	if _, err := q.DetectServiceCandidatesApex(ctx); err != nil {
		fail("service_candidates_apex", err)
	}
	if _, err := q.DetectServiceCandidatesIndegree(ctx, t.Cfg.IndegreeThreshold); err != nil {
		fail("service_candidates_indegree", err)
	}

	// Step 5 — campaign sync (nested BLOCKING lock: the tick waits out a
	// concurrent webhook-triggered sync rather than dropping the daily
	// guarantee).
	if err := lock.Run(ctx, t.Pool, lock.JobCampaignSync, v6ctlLockWait, func(ctx context.Context) error {
		_, err := campaign.Sync(ctx, t.Campaign, t.Pool)
		return err
	}); err != nil {
		fail("campaign_sync", err)
	}

	// Step 6 — check_job purge.
	if _, err := q.PurgeCheckJobs(ctx, pgInterval(t.Cfg.LiveCheckRetention)); err != nil {
		fail("check_job_purge", err)
	}

	// Step 7 — ops summary + heartbeat (ping only when steps 1–3 succeeded).
	t.summary(ctx, failed)
	coreOK := !contains(failed, "lifecycle_sweep") && !contains(failed, "stats_rollup")
	if coreOK && t.PingTick != nil {
		t.PingTick(ctx)
	}
	if len(failed) > 0 {
		return fmt.Errorf("tick completed with failed steps: %s", strings.Join(failed, ", "))
	}
	return nil
}

// RunStatsRollup runs the four §10.2–§10.5 snapshot upserts and the three
// §10.6 counter recomputes in one transaction — shared verbatim by tick
// steps 2–3 and `v6ctl stats recalc` (06-ingest.md §10.7).
func RunStatsRollup(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("stats rollup: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"global", q.SnapshotGlobalDaily},
		{"country", q.SnapshotCountryDaily},
		{"campaign", q.SnapshotCampaignDaily},
		{"asn", q.SnapshotASNDaily},
		{"reset_country", q.ResetCountryCounters},
		{"recompute_country", q.RecomputeCountryCounters},
		{"reset_asn", q.ResetASNCounters},
		{"recompute_asn", q.RecomputeASNCounters},
		{"reset_provider", q.ResetProviderCounters},
		{"recompute_provider", q.RecomputeProviderCounters},
	}
	for _, s := range steps {
		if err := s.fn(ctx); err != nil {
			return fmt.Errorf("stats rollup %s: %w", s.name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("stats rollup: commit: %w", err)
	}
	return nil
}

// summary sends the step-7 ops-webhook digest.
func (t *Tick) summary(ctx context.Context, failed []string) {
	if t.Notify == nil {
		return
	}
	var scanned, transitions, due int64
	_ = t.Pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM scan WHERE ts >= now() - interval '24 hours'),
		(SELECT count(*) FROM changelog WHERE ts >= now() - interval '24 hours'),
		(SELECT count(*) FROM domain WHERE (NOT disabled OR disabled_reason IN ('dead','delisted'))
		   AND next_check_at <= now())`).Scan(&scanned, &transitions, &due)
	msg := fmt.Sprintf("daily tick: scanned=%d transitions=%d queue_depth=%d", scanned, transitions, due)
	if len(failed) > 0 {
		msg += " FAILED_STEPS=" + strings.Join(failed, ",")
	}
	t.Notify(ctx, msg)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
