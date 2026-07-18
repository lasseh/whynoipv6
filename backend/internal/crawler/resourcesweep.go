package crawler

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// Sweep constants — deliberately NOT config (06 §5.2): the volume (2–4 qps)
// is too small to warrant registry keys.
const (
	sweepBatchSize = 100
	sweepEmptyPoll = 60 * time.Second
)

// sweepOutcome is one host's mapped lookup result; empty = non-definitive.
type sweepOutcome string

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
	rows, err := db.New(s.Pool).ResourceSweepClaim(ctx, sweepBatchSize)
	if err != nil {
		return nil, err
	}
	out := make([]sweptHost, len(rows))
	for i, r := range rows {
		out[i] = sweptHost{
			ID: r.ID, Host: r.Host,
			Status:       statusStr(r.AaaaStatus),
			Pending:      statusStr(r.AaaaPending),
			PendingCount: r.AaaaPendingCount,
		}
	}
	return out, nil
}

func statusStr(v *db.Ipv6Status) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}

// lookup maps one bulk-resolver AAAA answer to a sweep outcome (06 §5.3).
// The sweep never produces not_applicable.
func (s *ResourceSweeper) lookup(ctx context.Context, host string) sweepOutcome {
	ips, _, _, rcode, err := s.Bulk.LookupAAAA(ctx, host)
	if err != nil {
		return "" // timeout / network error → non-definitive
	}
	switch rcode {
	case checker.RcodeNXDomain:
		return sweepOutcome(domain.StatusNoRecord)
	case checker.RcodeNoError:
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

	err := db.New(s.Pool).ResourceSweepCommit(ctx, db.ResourceSweepCommitParams{
		ID:               h.ID,
		AaaaStatus:       statusEnum(status),
		AaaaPending:      statusEnum(pending),
		AaaaPendingCount: pendingCount,
	})
	if err != nil && ctx.Err() == nil {
		slog.Error("resource sweep commit failed", "domain", h.Host, "err", err.Error())
	}
}

func statusEnum(v *string) *db.Ipv6Status {
	if v == nil {
		return nil
	}
	s := db.Ipv6Status(*v)
	return &s
}
