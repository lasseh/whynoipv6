// Command crawler is the autonomous scanning daemon: frontier claim loop +
// worker slots, singleton coordinator (daily tick, Tranco cycle), metrics
// checkpointer, and GeoIP reloader (04-lifecycle-scheduling.md §13).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/campaign"
	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/config"
	"github.com/lasseh/whynoipv6/internal/consensus"
	"github.com/lasseh/whynoipv6/internal/crawler"
	"github.com/lasseh/whynoipv6/internal/geoip"
	"github.com/lasseh/whynoipv6/internal/ingest"
	"github.com/lasseh/whynoipv6/internal/notify"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// drainBudget is the graceful-shutdown drain deadline (04 §14 Decision).
const drainBudget = 80 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "crawler: "+err.Error())
		os.Exit(1)
	}
}

//nolint:gocritic // startup wiring is one linear sequence by design (04 §13)
func run() error {
	// Startup order (fail fast — 04 §13): config → pool → sentinels →
	// GeoIP → engine/consensus/preflight → run_id → goroutines.
	cfg, err := config.Load("crawler")
	if err != nil {
		return err
	}
	log, flushLogs, err := cfg.InstallLogger()
	if err != nil {
		return err
	}
	defer flushLogs()
	cfg.LogSummary(log)

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	pool, err := pgxpool.New(rootCtx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open pool: %w", err)
	}
	defer pool.Close()
	q := db.New(pool)

	countries, err := geoip.LoadCountryMap(rootCtx, q)
	if err != nil {
		return err
	}
	geoReader, err := geoip.Open(cfg.GeoIPPath)
	if err != nil {
		return err // missing/unreadable mmdb = fatal (06 §6.1)
	}
	defer geoReader.Close()

	bulk := checker.NewResolver(cfg.StringSlice("resolver.bulk_upstreams"))
	dialer := checker.NewSafeDialer(bulk)
	notifier := notify.New(cfg.String("ops.webhook_url"), cfg.String("ops.healthcheck_url"),
		cfg.String("ops.healthcheck_tick_url"), cfg.Duration("ops.healthcheck_min_interval"))
	cons := consensus.New(consensus.ConfigFrom(cfg), bulk, notifier.Webhook, log)
	defer cons.Close()

	resourcesEnabled := cfg.Bool("crawler.resources.enabled")
	runner := checker.NewRunner(checker.ConfigFrom(cfg), cons, dialer, log)
	preflight := checker.NewPreflight(bulk, cfg.String("preflight.probe_host"), log)

	providers, err := ingest.LoadProviderMapping(rootCtx, q)
	if err != nil {
		return err
	}

	commitCfg := crawler.CommitConfigFrom(cfg)
	commitCfg.Schedule.Bands, err = crawler.BandsFrom(cfg)
	if err != nil {
		return err
	}
	committer := crawler.NewCommitter(pool, commitCfg)

	runID := uuid.New()
	hostname, _ := os.Hostname()
	worker := fmt.Sprintf("%s:%d", hostname, os.Getpid())
	log = log.With("run_id", runID.String(), "worker", worker)
	slog.SetDefault(log)

	metrics := crawler.NewMetrics(pool, runID, worker)
	metrics.GeoIPBuildEpoch = geoReader.BuildEpoch
	metrics.Heartbeat = func() { notifier.HeartbeatOK(rootCtx) }
	metrics.Disagreement = cons.DrainDisagreement

	w := &crawler.Worker{
		Pool: pool, Scanner: runner, Preflight: preflight, Committer: committer,
		Metrics: metrics, BreakerOpen: cons.FastLaneSuppressed,
		Enrich: &crawler.GeoEnricher{
			Pool:      pool,
			Attr:      &geoip.Attributor{Meta: geoReader, Countries: countries},
			Countries: countries,
			Providers: providers,
		},
		ResourcesEnabled: resourcesEnabled,
	}

	frontier := crawler.NewFrontier(pool, crawler.FrontierConfigFrom(cfg))
	metrics.ActiveSlots = frontier.ActiveSlots
	// Workers commit under rootCtx (drain); the claim loop stops first.
	frontier.Process = func(_ context.Context, d crawler.ClaimedDomain) { w.Process(rootCtx, d) }
	frontier.Preflight = func(ctx context.Context) bool {
		if preflight.Run(ctx) {
			return true
		}
		slog.Warn("ipv6 preflight failed; claiming nothing")
		notifier.Webhook(ctx, "crawler preflight failed: no IPv6 egress; claiming paused")
		notifier.HeartbeatFail(ctx)
		return false
	}

	tick := &crawler.Tick{
		Pool:     pool,
		Cfg:      crawler.TickConfigFrom(cfg),
		Campaign: campaign.ConfigFrom(cfg),
		Notify:   notifier.Webhook,
		PingTick: notifier.PingTick,
	}
	coordinator := &crawler.Coordinator{
		Pool: pool, Tick: tick, Metrics: metrics, Notify: notifier.Webhook,
		Tranco:        ingest.NewTrancoImporter(pool, ingest.NewHTTPTrancoSource(), ingest.TrancoConfigFrom(cfg)),
		ImportAt:      cfg.String("tranco.import_at"),
		RetryInterval: cfg.Duration("tranco.retry_interval"),
		StaleWarn:     cfg.Duration("tranco.stale_warn_after"),
	}

	// Claiming contexts cancel immediately on SIGTERM; workers drain under
	// drainBudget on rootCtx (04 §14).
	claimCtx, stopClaim := context.WithCancel(rootCtx)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("shutdown: draining in-flight scans", "drain_budget", drainBudget.String())
		stopClaim()
		time.AfterFunc(drainBudget, rootCancel)
	}()

	go coordinator.Run(claimCtx)
	go metrics.RunIdleLoop(claimCtx)
	go geoipReloadLoop(claimCtx, geoReader)

	// The §5.1.5 check-job consumer pool + reaper (04 — placement).
	liveChecker := &crawler.LiveChecker{
		Pool: pool, Q: q, Runner: runner, Preflight: preflight,
		Cfg: crawler.LiveCheckConfigFrom(cfg),
	}
	go liveChecker.Run(claimCtx)

	// The resource-host sweep (06 §5.2): only when the dimension is on —
	// the registry is empty otherwise; flag changes apply on restart.
	if resourcesEnabled {
		sweeper := &crawler.ResourceSweeper{Pool: pool, Bulk: bulk}
		go sweeper.Run(claimCtx)
	}

	slog.Info("crawler started", "worker_slots", cfg.Int("worker_slots"))
	frontier.Run(claimCtx) // returns once claiming stopped and slots drained

	// Final checkpoint (is_final) with a fresh short context: rootCtx may
	// already be cancelled by the drain deadline.
	finalCtx, cancel := context.WithTimeout(context.WithoutCancel(rootCtx), 10*time.Second)
	defer cancel()
	metrics.Checkpoint(finalCtx, true)
	slog.Info("shutdown complete")
	return nil
}

// geoipReloadLoop stats the mmdb files hourly and swaps readers on change
// (06 §6.8).
func geoipReloadLoop(ctx context.Context, r *geoip.Reader) {
	ticker := time.NewTicker(geoip.ReloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.MaybeReload(); err != nil {
				slog.Warn("geoip reload failed", "err", err.Error())
			}
		}
	}
}
