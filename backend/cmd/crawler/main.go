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
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/postgres"

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

// atLeastOne are the registry keys whose zero is not a setting but a
// process that runs and does nothing right: no slots or an empty claim
// batch idles forever, a zero lookup cap turns every NS/MX check into an
// error, a zero QPS limiter blocks every consensus lookup, and zero
// breaker samples or probes trip and restore on nothing.
var atLeastOne = []string{
	"worker_slots",
	"claim.batch_size",
	"checks.max_ns_lookups",
	"checks.max_mx_lookups",
	"consensus.per_provider_qps",
	"consensus.fastlane_breaker.min_samples",
	"consensus.provider_breaker.min_samples",
	"consensus.provider_breaker.recovery_probes",
}

// validateBounds is the fail-fast gate for the values above (04 §13): the
// registry types them but does not bound them, and each consumer would
// otherwise start and misbehave quietly. An empty resolver.bulk_upstreams
// is rejected too: the resolver's fallback is the public resolvers, which
// must never carry the bulk load.
func validateBounds(cfg *config.Config) error {
	for _, key := range atLeastOne {
		if v := cfg.Int(key); v < 1 {
			return fmt.Errorf("config: %s must be at least 1, got %d", key, v)
		}
	}
	if len(cfg.StringSlice("resolver.bulk_upstreams")) == 0 {
		return fmt.Errorf("config: resolver.bulk_upstreams must name at least one upstream")
	}
	// The campaign checkout and git: tick step 5 needs both, and without
	// this the crawler reports it once a night instead of at startup.
	return campaign.ConfigFrom(cfg).Validate()
}

// minCrawlerConns is the pool floor this binary needs to make progress.
// Pool sizing is deliberately DSN-only (Annex C #8), so nothing else states
// or checks it (09-ops §2.1 erratum, review issue 41).
//
// The floor is set by the deepest nesting, not the widest fan-out: the
// 03:30 tick holds a lock connection, its nested campaign-sync lock holds a
// second, the sync transaction a third and the dedupe transaction a fourth
// — four at once, with the lifecycle sweep running underneath. Add the
// standing holders (4 live-check consumers, the reaper, the sweeper, the
// metrics checkpoint, the provider refresh) and 16 is the point below which
// the tick can starve itself while 64 worker slots queue on Acquire. The
// documented ?pool_max_conns=32 clears it with room; pgxpool's own default
// of 4 does not, and an operator who omits the parameter gets exactly that.
const minCrawlerConns = 16

func validatePoolSize(pool *pgxpool.Pool) error {
	if n := pool.Config().MaxConns; n < minCrawlerConns {
		return fmt.Errorf("DATABASE_URL pool_max_conns=%d is below the crawler floor of %d: "+
			"the daily tick nests four connections (lock, nested campaign lock, sync tx, "+
			"dedupe tx) over the standing live-check, sweep and metrics holders, and would "+
			"stall while worker slots queue on Acquire (09-ops §2.1 recommends 32)",
			n, minCrawlerConns)
	}
	return nil
}

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
	cfg.LogSummary(log, slog.LevelInfo)
	if err := validateBounds(cfg); err != nil {
		return err
	}

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	pool, err := postgres.NewPool(rootCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := validatePoolSize(pool); err != nil {
		return err
	}
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
	committer := crawler.NewCommitter(pool, crawler.CommitConfigFrom(cfg))

	runID := uuid.New()
	hostname, _ := os.Hostname()
	worker := fmt.Sprintf("%s:%d", hostname, os.Getpid())
	log = log.With("run_id", runID.String(), "worker", worker)
	slog.SetDefault(log)

	metrics := crawler.NewMetrics(pool, runID, worker)
	metrics.GeoIPBuildEpoch = geoReader.BuildEpoch
	// Off the worker slot: the ping is throttled to one per interval by its
	// own CAS, so at most one is in flight, and a slow hc-ping must not hold
	// a scan slot for the notify client's timeout.
	metrics.Heartbeat = func() { go notifier.HeartbeatOK(rootCtx) }

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
	frontier.Process = w.Process
	alerts := &preflightAlerter{now: time.Now}
	frontier.Preflight = func(ctx context.Context) bool {
		if preflight.Run(ctx) {
			if alerts.recovered() {
				notifier.Webhook(ctx, "crawler preflight recovered: IPv6 egress is back; claiming resumed")
			}
			return true
		}
		if ctx.Err() != nil {
			return false // a cancelled probe is shutdown, not an outage (04 §14.4)
		}
		slog.Warn("ipv6 preflight failed; claiming nothing")
		if alerts.failed() {
			notifier.Webhook(ctx, "crawler preflight failed: no IPv6 egress; claiming paused")
		}
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
	importAt := cfg.String("tranco.import_at")
	if _, err := time.Parse("15:04", importAt); err != nil {
		return fmt.Errorf("tranco.import_at: %w", err)
	}
	coordinator := &crawler.Coordinator{
		Pool: pool, Tick: tick, Metrics: metrics, Notify: notifier.Webhook,
		Tranco:        ingest.NewTrancoImporter(pool, ingest.NewHTTPTrancoSource(), ingest.TrancoConfigFrom(cfg)),
		ImportAt:      importAt,
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

	// The auxiliary loops stop with claimCtx and are joined before the final
	// checkpoint so the process never exits mid-operation; in-flight
	// live-check jobs drain under rootCtx like frontier scans (04 §14).
	var aux sync.WaitGroup
	aux.Go(func() { coordinator.Run(claimCtx) })
	aux.Go(func() { metrics.Run(claimCtx) })
	aux.Go(func() { geoipReloadLoop(claimCtx, geoReader) })
	// Re-snapshot the ns_host → provider mapping so curation (v6ctl provider
	// add/remove) lands without a crawler restart (06-ingest §6.10).
	if interval := cfg.Duration("dns_provider.refresh_interval"); interval > 0 {
		aux.Go(func() { providerRefreshLoop(claimCtx, providers, q, interval) })
	}

	// The §5.1.5 check-job consumer pool + reaper (04 — placement).
	liveChecker := &crawler.LiveChecker{
		Pool: pool, Q: q, Runner: runner, Preflight: preflight,
		Cfg: crawler.LiveCheckConfigFrom(cfg), Countries: countries,
	}
	aux.Go(func() { liveChecker.Run(claimCtx, rootCtx) })

	// The resource-host sweep (06 §5.2): only when the dimension is on —
	// the registry is empty otherwise; flag changes apply on restart.
	if resourcesEnabled {
		sweeper := &crawler.ResourceSweeper{Pool: pool, Bulk: bulk}
		aux.Go(func() { sweeper.Run(claimCtx) })
	}

	slog.Info("crawler started", "worker_slots", cfg.Int("worker_slots"))
	// claimCtx stops the loop taking new work; rootCtx carries the drain
	// budget, so an in-flight commit finishes rather than being cancelled.
	frontier.Run(claimCtx, rootCtx) // returns once claiming stopped and slots drained
	aux.Wait()

	// Final checkpoint (is_final) with a fresh short context: rootCtx may
	// already be cancelled by the drain deadline.
	finalCtx, cancel := context.WithTimeout(context.WithoutCancel(rootCtx), 10*time.Second)
	defer cancel()
	metrics.Checkpoint(finalCtx, true)
	slog.Info("shutdown complete")
	return nil
}

// providerRefreshLoop re-reads the DNS-provider mapping every interval; it
// stops with the other auxiliary loops and is joined before pool.Close().
func providerRefreshLoop(ctx context.Context, providers *ingest.ProviderMapping, q *db.Queries, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := providers.Refresh(ctx, q); err != nil {
				slog.Warn("provider mapping refresh failed", "err", err.Error())
			}
		}
	}
}

// preflightAlertInterval spaces the repeat ops-webhook alerts while an IPv6
// egress outage persists. Deliberately not ops.healthcheck_min_interval:
// that is 60s, the same as preflight.retry_interval, so reusing it would
// throttle nothing.
const preflightAlertInterval = 15 * time.Minute

// preflightAlerter gates the ops-webhook alert behind a failing preflight
// (04 §11 erratum). One message on the healthy→failed edge, then at most one
// per preflightAlertInterval while it persists, then one on recovery. An
// hour-long outage costs 5 messages instead of 60.
//
// The /fail ping is deliberately left per-cycle: healthchecks.io dedupes it
// and it is what holds the check red for the duration.
//
// Not concurrency-safe, and does not need to be — the claim loop calls
// Preflight from its single goroutine (crawler.Frontier.Run).
type preflightAlerter struct {
	failing  bool
	lastSent time.Time
	now      func() time.Time
}

// failed reports whether this failed cycle should post a webhook.
func (a *preflightAlerter) failed() bool {
	now := a.now()
	if !a.failing {
		a.failing, a.lastSent = true, now
		return true
	}
	if now.Sub(a.lastSent) < preflightAlertInterval {
		return false
	}
	a.lastSent = now
	return true
}

// recovered reports whether this healthy cycle ends an outage, and so should
// post the recovery message. False on every cycle that follows a healthy one,
// including the first — a process that starts healthy announces nothing.
func (a *preflightAlerter) recovered() bool {
	if !a.failing {
		return false
	}
	a.failing = false
	return true
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
