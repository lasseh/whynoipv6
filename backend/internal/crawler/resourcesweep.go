package crawler

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
)

// Sweep constants — deliberately NOT config (06 §5.2): the volume (2–4 qps)
// is too small to warrant registry keys.
const (
	sweepBatchSize = 100
	sweepEmptyPoll = 60 * time.Second
)

// sqlSweepClaim is the §5.2 claim: the schedule bump IS the crash lease —
// resource_host has no claimed_at column by decision.
const sqlSweepClaim = `
UPDATE resource_host
SET next_check_at = now() + interval '2 hours'
WHERE id IN (
  SELECT id FROM resource_host
  WHERE next_check_at <= now()
    AND dependent_count > 0
  ORDER BY next_check_at ASC
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
RETURNING id, host, aaaa_status, aaaa_pending, aaaa_pending_count`

// sweepOutcome is one host's mapped lookup result; empty = non-definitive.
type sweepOutcome string

// rcodeNoError complements commit.go's rcodeNXDomain (both mirror
// internal/consensus).
const rcodeNoError = "NOERROR"

// ResourceSweeper is the per-process resource-host sweep goroutine
// (06 §5.2–§5.4). Runs in every crawler process; SKIP LOCKED makes the
// concurrency safe without singleton gating. Not started while
// crawler.resources.enabled=false.
type ResourceSweeper struct {
	Pool *pgxpool.Pool
	Bulk *checker.Resolver // the two local Unbound instances; no quorum
}

type sweptHost struct {
	ID           int64
	Host         string
	Status       *string
	Pending      *string
	PendingCount int16
}

// Run blocks until ctx is done, claiming batches and processing hosts
// sequentially (one goroutine is enough at this volume).
func (s *ResourceSweeper) Run(ctx context.Context) {
	for ctx.Err() == nil {
		batch, err := s.claim(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("resource sweep claim failed", "err", err.Error())
			}
			sleepCtx(ctx, sweepEmptyPoll)
			continue
		}
		if len(batch) == 0 {
			sleepCtx(ctx, sweepEmptyPoll)
			continue
		}
		for i := range batch {
			s.sweepHost(ctx, &batch[i])
		}
	}
}

func (s *ResourceSweeper) claim(ctx context.Context) ([]sweptHost, error) {
	rows, err := s.Pool.Query(ctx, sqlSweepClaim, sweepBatchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sweptHost
	for rows.Next() {
		var h sweptHost
		if err := rows.Scan(&h.ID, &h.Host, &h.Status, &h.Pending, &h.PendingCount); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// lookup maps one bulk-resolver AAAA answer to a sweep outcome (06 §5.3).
// The sweep never produces not_applicable.
func (s *ResourceSweeper) lookup(ctx context.Context, host string) sweepOutcome {
	ips, _, _, rcode, err := s.Bulk.LookupAAAA(ctx, host)
	if err != nil {
		return "" // timeout / network error → non-definitive
	}
	switch rcode {
	case rcodeNXDomain:
		return sweepOutcome(domain.StatusNoRecord)
	case rcodeNoError:
		for _, ip := range ips {
			if checker.IsGloballyRoutableIPv6(ip) {
				return sweepOutcome(domain.StatusSupported)
			}
		}
		return sweepOutcome(domain.StatusUnsupported)
	default: // SERVFAIL etc.
		return ""
	}
}

// sweepHost applies the §5.4 host confirmation machine (N=2): one implicit
// single-row transaction per host; non-definitive touches nothing (the
// claim already bumped next_check_at 2h out).
func (s *ResourceSweeper) sweepHost(ctx context.Context, h *sweptHost) {
	outcome := s.lookup(ctx, h.Host)
	if outcome == "" {
		return
	}
	o := string(outcome)

	status, pending, pendingCount := h.Status, h.Pending, h.PendingCount
	switch {
	case status == nil: // first-ever definitive value commits immediately
		status, pending, pendingCount = &o, nil, 0
	case o == *status: // agreement: clear any candidate
		pending, pendingCount = nil, 0
	case pending != nil && o == *pending: // consecutive candidate sighting
		pendingCount++
		if pendingCount >= 2 { // N=2
			status, pending, pendingCount = &o, nil, 0
		}
	default: // new candidate
		pending, pendingCount = &o, 1
	}

	_, err := s.Pool.Exec(ctx, `
		UPDATE resource_host
		SET aaaa_status = $2::ipv6_status, aaaa_pending = $3::ipv6_status,
		    aaaa_pending_count = $4,
		    last_checked_at = now(), next_check_at = now() + interval '24 hours'
		WHERE id = $1`, h.ID, status, pending, pendingCount)
	if err != nil && ctx.Err() == nil {
		slog.Error("resource sweep commit failed", "host", h.Host, "err", err.Error())
	}
}
