package api

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/observe"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// checkStore is the consumer-side seam for the live-check surface's data
// needs (07 §5.1): the rate windows, the lifecycle re-entry, the two dedupe
// sources, the advisory-locked enqueue, and the stored-evidence inputs.
// pgCheckStore adapts db.Queries + the pool in production; the check unit
// tests substitute an in-memory fake, so the whole §5.1.1 dedupe ladder
// runs without a database.
type checkStore interface {
	RatePrefix(ctx context.Context, prefix netip.Prefix) (db.CheckJobRatePrefixRow, error)
	RateGlobal(ctx context.Context) (db.CheckJobRateGlobalRow, error)
	Reentry(ctx context.Context, host string) error
	Confirmed(ctx context.Context, host string) (db.DomainConfirmedRow, error)
	JobDedupe(ctx context.Context, host string, window time.Duration) (db.CheckJobDedupeRow, error)
	JobByID(ctx context.Context, id int64) (db.CheckJobByIDRow, error)

	// EnqueueLocked re-checks both §6.3 caps and inserts the job in ONE
	// transaction under the enqueue advisory lock, so concurrent requests
	// cannot race the check-then-insert past the limits. A cap overrun
	// reports the offending window instead of inserting; the returned
	// IPWindow always carries the locked count (the fast-path read may be
	// stale).
	EnqueueLocked(ctx context.Context, host string, requester netip.Addr, prefix netip.Prefix,
		ipCap, globalCap int) (enqueueResult, error)

	LatestScanDetail(ctx context.Context, domainID int64) ([]byte, error)

	// LiveLinks builds the live-check LinkSet through the one observe
	// constructor (CONTEXT.md: LinkSet) — behind the seam only because it
	// reads resource rows from the pool.
	LiveLinks(ctx context.Context, sr checker.ScanResult, enabled bool) []observe.LinkedResource
}

// enqueueResult is EnqueueLocked's outcome: the inserted job, or the window
// that rejected it under the lock.
type enqueueResult struct {
	ID        int64
	CreatedAt time.Time

	OverIP       bool
	OverGlobal   bool
	IPWindow     db.CheckJobRatePrefixRow
	GlobalWindow db.CheckJobRateGlobalRow
}

// pgCheckStore is the production adapter at the checkStore seam.
type pgCheckStore struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func (p pgCheckStore) RatePrefix(ctx context.Context, prefix netip.Prefix) (db.CheckJobRatePrefixRow, error) {
	return p.q.CheckJobRatePrefix(ctx, prefix)
}

func (p pgCheckStore) RateGlobal(ctx context.Context) (db.CheckJobRateGlobalRow, error) {
	return p.q.CheckJobRateGlobal(ctx)
}

func (p pgCheckStore) Reentry(ctx context.Context, host string) error {
	return p.q.DomainLiveCheckReentry(ctx, host)
}

func (p pgCheckStore) Confirmed(ctx context.Context, host string) (db.DomainConfirmedRow, error) {
	return p.q.DomainConfirmed(ctx, host)
}

func (p pgCheckStore) JobDedupe(ctx context.Context, host string, window time.Duration) (db.CheckJobDedupeRow, error) {
	return p.q.CheckJobDedupe(ctx, db.CheckJobDedupeParams{Host: host, DedupeWindow: pgInterval(window)})
}

func (p pgCheckStore) JobByID(ctx context.Context, id int64) (db.CheckJobByIDRow, error) {
	return p.q.CheckJobByID(ctx, id)
}

func (p pgCheckStore) EnqueueLocked(ctx context.Context, host string, requester netip.Addr, prefix netip.Prefix,
	ipCap, globalCap int,
) (enqueueResult, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return enqueueResult{}, fmt.Errorf("enqueue begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", checkJobEnqueueLockID); err != nil {
		return enqueueResult{}, fmt.Errorf("enqueue lock: %w", err)
	}
	qtx := p.q.WithTx(tx)
	ipWin, err := qtx.CheckJobRatePrefix(ctx, prefix)
	if err != nil {
		return enqueueResult{}, fmt.Errorf("prefix window: %w", err)
	}
	if int(ipWin.N) >= ipCap {
		return enqueueResult{OverIP: true, IPWindow: ipWin}, nil
	}
	globalWin, err := qtx.CheckJobRateGlobal(ctx)
	if err != nil {
		return enqueueResult{}, fmt.Errorf("global window: %w", err)
	}
	if int(globalWin.N) >= globalCap {
		return enqueueResult{OverGlobal: true, IPWindow: ipWin, GlobalWindow: globalWin}, nil
	}
	ins, err := qtx.CheckJobInsert(ctx, db.CheckJobInsertParams{Host: host, RequesterIp: requester})
	if err != nil {
		return enqueueResult{}, fmt.Errorf("insert check_job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return enqueueResult{}, fmt.Errorf("enqueue commit: %w", err)
	}
	return enqueueResult{ID: ins.ID, CreatedAt: ins.CreatedAt.Time, IPWindow: ipWin}, nil
}

func (p pgCheckStore) LatestScanDetail(ctx context.Context, domainID int64) ([]byte, error) {
	return p.q.LatestScanDetail(ctx, domainID)
}

func (p pgCheckStore) LiveLinks(ctx context.Context, sr checker.ScanResult, enabled bool) []observe.LinkedResource {
	return observe.LiveLinks(ctx, observe.Resources(p.pool), sr, enabled)
}
