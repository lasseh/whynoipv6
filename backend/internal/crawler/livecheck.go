package crawler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/campaign"
	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/geoip"
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
	Runner    Scanner
	Preflight PreflightState
	Cfg       LiveCheckConfig

	// Countries is the run's in-memory country map — the insert-time
	// attribution helper 06 §6.5 names, loaded once by cmd wiring. Required.
	Countries *geoip.CountryMap
}

// Run blocks until ctx is done and every in-flight job has finished,
// operating Cfg.Workers claim loops and the 60 s reaper tick. work is the
// context jobs run under — cmd wiring passes the drain-budget root context
// so a SIGTERM drains an in-flight job instead of cancelling it (04 §14).
func (lc *LiveChecker) Run(ctx, work context.Context) {
	var wg sync.WaitGroup
	for range lc.Cfg.Workers {
		wg.Go(func() { lc.workerLoop(ctx, work) })
	}
	defer wg.Wait()
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

func (lc *LiveChecker) workerLoop(ctx, work context.Context) {
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
			// Back off like the empty-queue branch (and the frontier and
			// sweep loops on the same condition): a refused connection
			// fails in a millisecond and would otherwise spin.
			if !sleepCtx(ctx, livePollIdle) {
				return
			}
			continue
		}
		lc.process(work, job.ID, job.Host)
	}
}

// liveWriteTimeout bounds the job's terminal write (done/failed). The
// write runs on a context detached from the drain so a scan that finished
// at the deadline still records its result instead of waiting for the
// reaper (04 §14: an in-flight job completes rather than being cancelled).
const liveWriteTimeout = 10 * time.Second

func (lc *LiveChecker) fail(ctx context.Context, id int64, reason string) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), liveWriteTimeout)
	defer cancel()
	if err := lc.Q.CheckJobFail(wctx, db.CheckJobFailParams{ID: id, Error: ptr(reason)}); err != nil {
		slog.Error("check-job fail write failed", "id", id, "err", err.Error())
	}
}

// process runs one job under the engine budget, panic-recovered; only
// check_job.result is written (Rule 0).
func (lc *LiveChecker) process(ctx context.Context, id int64, host string) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("live check panicked", "domain", host, "panic", rec)
			lc.fail(ctx, id, "internal error")
		}
	}()

	kind, err := lc.ensureDomain(ctx, host)
	if err != nil {
		// A PSL miss (unknown TLD / bare public suffix) is an expected
		// client error, not an internal fault — surface the real reason.
		var pslErr *pslEvalError
		if errors.As(err, &pslErr) {
			lc.fail(ctx, id, "the host is not under a known public-suffix TLD")
			return
		}
		slog.Error("live check ensure-domain failed", "domain", host, "err", err.Error())
		lc.fail(ctx, id, "internal error")
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, lc.Cfg.JobBudget)
	sr := lc.Runner.Run(runCtx, host, kind)
	budgetBlown := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	cancel()
	if budgetBlown {
		// §5.1.5 step 4: a timed-out run fails — never a partial `done`.
		lc.fail(ctx, id, "timed out")
		return
	}

	links := observe.LiveLinks(ctx, observe.Resources(lc.Pool), sr, lc.Cfg.ResourcesEnabled)
	res := observe.MapLiveResult(kind, sr, lc.Preflight.LastPass(), time.Now().UTC(), links, lc.Cfg.ResourcesEnabled)
	raw, err := json.Marshal(res)
	if err != nil {
		lc.fail(ctx, id, "result encode failed")
		return
	}
	wctx, cancelWrite := context.WithTimeout(context.WithoutCancel(ctx), liveWriteTimeout)
	defer cancelWrite()
	if err := lc.Q.CheckJobComplete(wctx, db.CheckJobCompleteParams{ID: id, Result: raw}); err != nil {
		slog.Error("check-job complete write failed", "id", id, "err", err.Error())
	}
}

// pslEvalError marks a PSL evaluation failure — an invalid host, not an
// internal fault.
type pslEvalError struct{ err error }

func (e *pslEvalError) Error() string { return e.err.Error() }
func (e *pslEvalError) Unwrap() error { return e.err }

// ensureDomain inserts the initial row for an unknown host (§5.1.5 step 2):
// created_by='live_check', rank NULL, parent linked only when the
// registrable parent row ALREADY exists — never auto-ensured.
func (lc *LiveChecker) ensureDomain(ctx context.Context, host string) (domain.Kind, error) {
	registrable, tld, err := campaign.PSLParse(host)
	if err != nil {
		return "", &pslEvalError{err: err}
	}
	// The host-existence check first: for a host we already know, its own
	// row carries the kind and nothing else here applies.
	if existing, err := lc.Q.DomainByHost(ctx, host); err == nil {
		return domain.Kind(existing.Kind), nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	kind := domain.KindApex
	var parentID *int64
	if host != registrable {
		kind = domain.KindSubdomain
		if parent, err := lc.Q.DomainByHost(ctx, registrable); err == nil {
			parentID = &parent.ID
		}
	}

	// Insert-time attribution through the in-memory helper 06 §6.5 names:
	// sentinel ASN, ccTLD-or-sentinel country. Re-querying the sentinels and
	// re-implementing the ccTLD probe here gave the rule two homes.
	_, err = lc.Q.DomainInsertLiveCheck(ctx, db.DomainInsertLiveCheckParams{
		Host: host, Kind: db.DomainKind(kind), ParentID: parentID,
		AsnID:     lc.Countries.SentinelASN,
		CountryID: lc.Countries.InsertCountryID(host),
		Tld:       &tld,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) { // ErrNoRows = conflict race, fine
		return "", err
	}
	return kind, nil
}

func ptr[T any](v T) *T { return &v }
