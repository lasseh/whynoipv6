package crawler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/campaign"
	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/observe"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// LiveCheckConfig carries the live_check.* registry keys (09-ops).
type LiveCheckConfig struct {
	Workers          int           // live_check.workers (default 4)
	JobBudget        time.Duration // live_check.job_budget (default 60s)
	ReclaimAfter     time.Duration // live_check.reclaim_after (default 5m)
	FailAfter        time.Duration // live_check.fail_after (default 15m)
	ResourcesEnabled bool
}

// livePollIdle is the consumer poll interval when the queue is empty
// (07 §5.1.5); the reaper fires every reaperEvery.
const (
	livePollIdle = 2 * time.Second
	reaperEvery  = 60 * time.Second
)

// LiveChecker is the check-job consumer pool + reaper, run inside
// cmd/crawler next to the frontier workers (04 — placement).
type LiveChecker struct {
	Pool      *pgxpool.Pool
	Q         *db.Queries
	Runner    *checker.Runner
	Preflight *checker.Preflight
	Cfg       LiveCheckConfig
}

// Run blocks until ctx is done, operating Cfg.Workers claim loops and the
// 60 s reaper tick.
func (lc *LiveChecker) Run(ctx context.Context) {
	for i := 0; i < lc.Cfg.Workers; i++ {
		go lc.workerLoop(ctx)
	}
	t := time.NewTicker(reaperEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := lc.Q.CheckJobReap(ctx, pgInterval(lc.Cfg.FailAfter))
			if err != nil && ctx.Err() == nil {
				slog.Error("check-job reaper failed", "err", err.Error())
			} else if n > 0 {
				slog.Warn("check-job reaper failed stale jobs", "count", n)
			}
		}
	}
}

func (lc *LiveChecker) workerLoop(ctx context.Context) {
	for ctx.Err() == nil {
		job, err := lc.Q.CheckJobClaim(ctx, pgInterval(lc.Cfg.ReclaimAfter))
		if errors.Is(err, pgx.ErrNoRows) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(livePollIdle):
			}
			continue
		}
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("check-job claim failed", "err", err.Error())
			}
			continue
		}
		lc.process(ctx, job.ID, job.Host)
	}
}

// process runs one job under the engine budget, panic-recovered; only
// check_job.result is written (Rule 0).
func (lc *LiveChecker) process(ctx context.Context, id int64, host string) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("live check panicked", "domain", host, "panic", rec)
			if err := lc.Q.CheckJobFail(ctx, db.CheckJobFailParams{ID: id, Error: ptr("internal error")}); err != nil {
				slog.Error("check-job fail write failed", "err", err.Error())
			}
		}
	}()

	kind, err := lc.ensureDomain(ctx, host)
	if err != nil {
		// A PSL miss (unknown TLD / bare public suffix) is an expected
		// client error, not an internal fault — surface the real reason.
		var pslErr *pslEvalError
		if errors.As(err, &pslErr) {
			_ = lc.Q.CheckJobFail(ctx, db.CheckJobFailParams{
				ID: id, Error: ptr("the host is not under a known public-suffix TLD"),
			})
			return
		}
		slog.Error("live check ensure-domain failed", "domain", host, "err", err.Error())
		_ = lc.Q.CheckJobFail(ctx, db.CheckJobFailParams{ID: id, Error: ptr("internal error")})
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, lc.Cfg.JobBudget)
	sr := lc.Runner.Run(runCtx, host, checker.Kind(kind))
	budgetBlown := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	cancel()
	if budgetBlown {
		// §5.1.5 step 4: a timed-out run fails — never a partial `done`.
		if err := lc.Q.CheckJobFail(ctx, db.CheckJobFailParams{ID: id, Error: ptr("timed out")}); err != nil && ctx.Err() == nil {
			slog.Error("check-job fail write failed", "err", err.Error())
		}
		return
	}

	links := observe.LiveLinks(ctx, lc.Pool, sr, lc.Cfg.ResourcesEnabled)
	res := observe.MapLiveResult(kind, sr, lc.Preflight.LastPass(), time.Now().UTC(), links, lc.Cfg.ResourcesEnabled)
	raw, err := json.Marshal(res)
	if err != nil {
		_ = lc.Q.CheckJobFail(ctx, db.CheckJobFailParams{ID: id, Error: ptr("result encode failed")})
		return
	}
	if err := lc.Q.CheckJobComplete(ctx, db.CheckJobCompleteParams{ID: id, Result: raw}); err != nil && ctx.Err() == nil {
		slog.Error("check-job complete write failed", "err", err.Error())
	}
}

// ensureDomain inserts the initial row for an unknown host (§5.1.5 step 2):
// created_by='live_check', rank NULL, parent linked only when the
// registrable parent row ALREADY exists — never auto-ensured.
// pslEvalError marks a PSL evaluation failure — an invalid host, not an
// internal fault.
type pslEvalError struct{ err error }

func (e *pslEvalError) Error() string { return e.err.Error() }
func (e *pslEvalError) Unwrap() error { return e.err }

func (lc *LiveChecker) ensureDomain(ctx context.Context, host string) (domain.Kind, error) {
	registrable, tld, err := campaign.PSLParse(host)
	if err != nil {
		return "", &pslEvalError{err: err}
	}
	kind := domain.KindApex
	var parentID *int64
	if host != registrable {
		kind = domain.KindSubdomain
		if parent, err := lc.Q.DomainByHost(ctx, registrable); err == nil {
			parentID = &parent.ID
		}
	}

	if existing, err := lc.Q.DomainByHost(ctx, host); err == nil {
		return domain.Kind(existing.Kind), nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	asnID, err := lc.Q.ASNSentinelID(ctx)
	if err != nil {
		return "", fmt.Errorf("sentinel asn: %w", err)
	}
	countryID, err := lc.Q.CountrySentinelID(ctx)
	if err != nil {
		return "", fmt.Errorf("sentinel country: %w", err)
	}
	// Insert-time country attribution: final-label ccTLD probe (06 §6.5).
	label := host[strings.LastIndexByte(host, '.')+1:]
	probe := "." + strings.ToUpper(label)
	if id, err := lc.Q.CountryIDByTLD(ctx, &probe); err == nil {
		countryID = id
	}

	_, err = lc.Q.DomainInsertLiveCheck(ctx, db.DomainInsertLiveCheckParams{
		Host: host, Kind: db.DomainKind(kind), ParentID: parentID,
		AsnID: asnID, CountryID: countryID, Tld: &tld,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) { // ErrNoRows = conflict race, fine
		return "", err
	}
	return kind, nil
}

func ptr[T any](v T) *T { return &v }
