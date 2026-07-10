package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// SweepConfig carries the lifecycle intervals (registry: 09-ops.md §2.4).
type SweepConfig struct {
	LiveCheckLinkage time.Duration // lifecycle.live_check_linkage, 168h
	DelistGrace      time.Duration // lifecycle.delist_grace, 720h
	SlowLaneEvery    time.Duration // lifecycle.slow_lane_every, 720h
}

// SweepResult reports per-statement row counts (S1–S5).
type SweepResult struct {
	OrphansCleared    int64 // S1
	Reenabled         int64 // S2
	LiveCheckDelisted int64 // S3
	OrphansStamped    int64 // S4
	Delisted          int64 // S5
}

// Sweep runs the daily lifecycle sweep S1–S5 in one transaction
// (04-lifecycle-scheduling.md §8, tick step 1). Idempotent: a same-day
// second run changes zero rows.
func Sweep(ctx context.Context, pool *pgxpool.Pool, cfg SweepConfig) (SweepResult, error) {
	var res SweepResult
	tx, err := pool.Begin(ctx)
	if err != nil {
		return res, fmt.Errorf("sweep: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	linkage := pgInterval(cfg.LiveCheckLinkage)
	grace := pgInterval(cfg.DelistGrace)
	slow := pgInterval(cfg.SlowLaneEvery)

	if res.OrphansCleared, err = q.SweepClearOrphans(ctx, linkage); err != nil {
		return res, fmt.Errorf("sweep S1: %w", err)
	}
	if res.Reenabled, err = q.SweepReenableDelisted(ctx, linkage); err != nil {
		return res, fmt.Errorf("sweep S2: %w", err)
	}
	if res.LiveCheckDelisted, err = q.SweepDelistLiveCheck(ctx, db.SweepDelistLiveCheckParams{
		SlowLaneEvery: slow, LiveCheckLinkage: linkage,
	}); err != nil {
		return res, fmt.Errorf("sweep S3: %w", err)
	}
	if res.OrphansStamped, err = q.SweepStampOrphans(ctx, linkage); err != nil {
		return res, fmt.Errorf("sweep S4: %w", err)
	}
	if res.Delisted, err = q.SweepDelistExpired(ctx, db.SweepDelistExpiredParams{
		SlowLaneEvery: slow, LiveCheckLinkage: linkage, DelistGrace: grace,
	}); err != nil {
		return res, fmt.Errorf("sweep S5: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return res, fmt.Errorf("sweep: commit: %w", err)
	}
	slog.Info("lifecycle sweep done",
		"orphans_cleared", res.OrphansCleared, "reenabled", res.Reenabled,
		"live_check_delisted", res.LiveCheckDelisted, "orphans_stamped", res.OrphansStamped,
		"delisted", res.Delisted)
	return res, nil
}

func pgInterval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}
