package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// LeaseReclaim is the stale-lease window, embedded in the claim SQL; never
// configurable (04-lifecycle-scheduling.md §2).
const LeaseReclaim = 30 * time.Minute

// ClaimedDomain is the snapshot contract with 04's claim query
// (03-state-machine.md §16). The commit never re-SELECTs the row.
type ClaimedDomain struct {
	ID             int64
	Host           string
	Kind           domain.Kind
	Rank           *int32
	ClaimedAt      time.Time // L — the lease token
	Disabled       bool
	DisabledReason *domain.DisabledReason
	DisabledAt     *time.Time
	DeadStreak     int16
	ErrorStreak    int16
	LastCountedAt  *time.Time
	AsnID          int32
	CountryID      int32
	Dims           map[domain.Dimension]DimState // all six groups
}

// DimState is one dimension's confirm/pending column group.
type DimState struct {
	Status       *domain.IPv6Status
	Pending      *domain.IPv6Status
	PendingCount int16
	Since        *time.Time
}

// FrontierConfig carries the claim-loop knobs (registry: 09-ops.md §2.2).
type FrontierConfig struct {
	BatchSize     int           // claim.batch_size, default 200
	Order         string        // claim.order: rank | age
	EmptyPoll     time.Duration // claim.empty_poll_interval, default 10s
	WorkerSlots   int           // worker_slots, default 64
	RetryInterval time.Duration // preflight.retry_interval, default 60s
}

// Frontier owns the claim loop and the WORKER_SLOTS-bounded slot pool.
// The per-domain slot body and the preflight gate are injected (their
// implementations arrive with the commit machine and preflight wiring).
type Frontier struct {
	pool *pgxpool.Pool
	q    *db.Queries
	cfg  FrontierConfig

	// Preflight runs before every claim cycle; false = claim nothing and
	// retry after cfg.RetryInterval (04 §11/§12). Nil = always proceed.
	Preflight func(ctx context.Context) bool

	// Process handles one claimed domain inside a worker slot (engine run →
	// map → schedule → commit → metrics).
	Process func(ctx context.Context, d ClaimedDomain)
}

// NewFrontier builds the claim loop. claim.order is read once at startup —
// no runtime flipping (04 §3).
func NewFrontier(pool *pgxpool.Pool, cfg FrontierConfig) *Frontier {
	return &Frontier{pool: pool, q: db.New(pool), cfg: cfg}
}

// ClaimBatch runs one claim statement and returns the claimed snapshots.
func (f *Frontier) ClaimBatch(ctx context.Context) ([]ClaimedDomain, error) {
	limit := int32(f.cfg.BatchSize) //nolint:gosec // claim.batch_size is a small registry int
	if f.cfg.Order == "age" {
		rows, err := f.q.ClaimBatchByAge(ctx, limit)
		if err != nil {
			return nil, fmt.Errorf("claim (age): %w", err)
		}
		out := make([]ClaimedDomain, len(rows))
		for i := range rows {
			r := claimRow(rows[i])
			out[i] = claimedFromRow(&r)
		}
		return out, nil
	}
	rows, err := f.q.ClaimBatchByRank(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("claim (rank): %w", err)
	}
	out := make([]ClaimedDomain, len(rows))
	for i := range rows {
		r := claimRow(rows[i])
		out[i] = claimedFromRow(&r)
	}
	return out, nil
}

// Run is the §12 claim loop: preflight → claim → dispatch to the slot pool,
// claiming again as soon as the batch is dispatched. Returns when claim is
// cancelled and every in-flight slot has finished.
//
// Two contexts, the same shape LiveChecker.Run already takes (04 §14):
// claim stops the loop from taking new work, while work is what slot
// bodies run under — cmd wiring passes the drain-budget root, so a SIGTERM
// finishes an in-flight commit instead of cancelling it. Naming both in the
// signature is what makes the split visible; when Run handed slot bodies
// the claim context, every caller had to silently discard it and reach for
// the drain context from an enclosing scope, and the shutdown test
// reproduced that same substitution — so a regression in the wiring would
// have left it green.
func (f *Frontier) Run(claim, work context.Context) {
	slots := make(chan ClaimedDomain)
	var pool sync.WaitGroup
	for range f.cfg.WorkerSlots {
		pool.Go(func() {
			for d := range slots {
				f.process(work, d)
			}
		})
	}
	defer func() {
		close(slots)
		pool.Wait()
	}()

	for {
		if claim.Err() != nil {
			return
		}
		if f.Preflight != nil && !f.Preflight(claim) {
			if !sleepCtx(claim, f.cfg.RetryInterval) {
				return
			}
			continue
		}
		batch, err := f.ClaimBatch(claim)
		if err != nil {
			if claim.Err() != nil {
				return
			}
			slog.Error("claim batch failed", "err", err.Error())
			if !sleepCtx(claim, f.cfg.EmptyPoll) {
				return
			}
			continue
		}
		if len(batch) == 0 {
			if !sleepCtx(claim, f.cfg.EmptyPoll) {
				return
			}
			continue
		}
		for i := range batch {
			select {
			case slots <- batch[i]: // blocks until a slot is free
			case <-claim.Done():
				return // undispatched claims expire via the 30-min lease
			}
		}
	}
}

// process runs one slot body under a recover: the engine recovers per
// check, but a panic in the mapping, enrichment or commit path would
// otherwise take the slot with it — parked on the join, never logged,
// shrinking the pool one domain at a time. The domain is left to its lease.
func (f *Frontier) process(ctx context.Context, d ClaimedDomain) { //nolint:gocritic // the slot contract passes by value
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("scan panicked; slot kept, domain left to its lease",
				"domain", d.Host, "panic", rec, "stack", string(debug.Stack()))
		}
	}()
	f.Process(ctx, d)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// claimRow is the shared column set of both claim-query variants (the
// generated row structs are identical field-for-field).
type claimRow db.ClaimBatchByRankRow

func claimedFromRow(r *claimRow) ClaimedDomain {
	cd := ClaimedDomain{
		ID:          r.ID,
		Host:        r.Host,
		Kind:        domain.Kind(r.Kind),
		Rank:        r.Rank,
		ClaimedAt:   r.ClaimedAt.Time,
		Disabled:    r.Disabled,
		DisabledAt:  postgres.TimePtr(r.DisabledAt),
		DeadStreak:  r.DeadStreak,
		ErrorStreak: r.ErrorStreak,
		AsnID:       r.AsnID,
		CountryID:   r.CountryID,
	}
	if r.DisabledReason != nil {
		reason := domain.DisabledReason(*r.DisabledReason)
		cd.DisabledReason = &reason
	}
	cd.LastCountedAt = postgres.TimePtr(r.LastCountedAt)
	cd.Dims = map[domain.Dimension]DimState{
		domain.DimBase:      dim(r.BaseStatus, r.BasePending, r.BasePendingCount, r.BaseSince),
		domain.DimWWW:       dim(r.WwwStatus, r.WwwPending, r.WwwPendingCount, r.WwwSince),
		domain.DimNS:        dim(r.NsStatus, r.NsPending, r.NsPendingCount, r.NsSince),
		domain.DimMX:        dim(r.MxStatus, r.MxPending, r.MxPendingCount, r.MxSince),
		domain.DimConn:      dim(r.ConnStatus, r.ConnPending, r.ConnPendingCount, r.ConnSince),
		domain.DimResources: dim(r.ResourcesStatus, r.ResourcesPending, r.ResourcesPendingCount, r.ResourcesSince),
	}
	return cd
}

func dim(status, pending *db.Ipv6Status, count int16, since pgtype.Timestamptz) DimState {
	return DimState{
		Status:       ipv6StatusPtr(status),
		Pending:      ipv6StatusPtr(pending),
		PendingCount: count,
		Since:        postgres.TimePtr(since),
	}
}

func ipv6StatusPtr(s *db.Ipv6Status) *domain.IPv6Status {
	if s == nil {
		return nil
	}
	v := domain.IPv6Status(*s)
	return &v
}
