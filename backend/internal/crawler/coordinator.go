package crawler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/ingest"
	"github.com/lasseh/whynoipv6/internal/lock"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// Coordinator runs the per-process singleton schedules (04 §9, §13 row 5):
// the 03:30 UTC daily tick and the 23:15 UTC Tranco import cycle with its
// 2h retry and 48h staleness warning. Both processes run it; the advisory
// locks deduplicate.
type Coordinator struct {
	Pool    *pgxpool.Pool
	Tick    *Tick
	Tranco  *ingest.TrancoImporter
	Metrics *Metrics
	Notify  func(ctx context.Context, msg string)

	ImportAt      string        // tranco.import_at, "23:15" UTC
	RetryInterval time.Duration // tranco.retry_interval, 2h
	StaleWarn     time.Duration // tranco.stale_warn_after, 48h

	lastStaleWarn time.Time
}

// Run blocks until ctx is cancelled, firing the two schedules.
func (c *Coordinator) Run(ctx context.Context) {
	tickCh := scheduleAt(ctx, TickAt)
	trancoCh := scheduleAt(ctx, c.ImportAt)
	var retry <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-tickCh:
			c.runTick(ctx)
		case <-trancoCh:
			retry = c.runTrancoAttempt(ctx)
		case <-retry:
			retry = c.runTrancoAttempt(ctx)
		}
	}
}

func (c *Coordinator) runTick(ctx context.Context) {
	err := lock.TryRun(ctx, c.Pool, lock.JobDailyTick, c.Tick.Run)
	switch {
	case errors.Is(err, lock.ErrHeld):
		slog.Info("singleton skipped, held elsewhere", "job", "daily_tick")
		if c.Metrics != nil {
			c.Metrics.RecordSingletonSkip("daily_tick")
		}
	case err != nil:
		slog.Error("daily tick failed", "err", err.Error())
	}
}

// runTrancoAttempt runs one import attempt; a non-success outcome schedules
// a retry after RetryInterval (superseded when the next 23:15 cycle fires).
func (c *Coordinator) runTrancoAttempt(ctx context.Context) <-chan time.Time {
	c.maybeStaleWarn(ctx)

	var rep *ingest.TrancoReport
	err := lock.TryRun(ctx, c.Pool, lock.JobTrancoImport, func(ctx context.Context) error {
		var err error
		rep, err = c.Tranco.Import(ctx, false)
		return err
	})
	switch {
	case errors.Is(err, lock.ErrHeld):
		slog.Info("singleton skipped, held elsewhere", "job", "tranco_import")
		if c.Metrics != nil {
			c.Metrics.RecordSingletonSkip("tranco_import")
		}
		return nil // the holder owns this cycle
	case err != nil:
		slog.Error("tranco import attempt failed", "err", err.Error())
		return time.After(c.RetryInterval)
	}

	switch rep.Outcome {
	case ingest.TrancoImported:
		return nil // cycle done
	case ingest.TrancoAborted:
		if c.Notify != nil {
			c.Notify(ctx, "tranco import aborted: "+rep.Note)
		}
		return time.After(c.RetryInterval)
	default: // no_new_list, aborted_previously, not_modified
		return time.After(c.RetryInterval)
	}
}

// maybeStaleWarn sends the 48h no-import warning, rate-limited to once per
// 24h in process memory (06 §2.1).
func (c *Coordinator) maybeStaleWarn(ctx context.Context) {
	if c.Notify == nil || time.Since(c.lastStaleWarn) < 24*time.Hour {
		return
	}
	last, err := db.New(c.Pool).TrancoLastSuccessAt(ctx)
	if err != nil || !last.Valid {
		return
	}
	if age := time.Since(last.Time); age > c.StaleWarn {
		c.lastStaleWarn = time.Now()
		c.Notify(ctx, "no new Tranco list for "+age.Round(time.Hour).String()+"; ranks frozen")
		slog.Warn("tranco import stale", "age", age.Round(time.Hour).String())
	}
}

// scheduleAt returns a channel that fires daily at the given UTC HH:MM.
func scheduleAt(ctx context.Context, hhmm string) <-chan time.Time {
	ch := make(chan time.Time, 1)
	go func() {
		for {
			next := nextOccurrence(time.Now().UTC(), hhmm)
			timer := time.NewTimer(time.Until(next))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case t := <-timer.C:
				select {
				case ch <- t:
				default:
				}
			}
		}
	}()
	return ch
}

// nextOccurrence computes the next UTC occurrence of HH:MM after now.
func nextOccurrence(now time.Time, hhmm string) time.Time {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		t, _ = time.Parse("15:04", "03:30")
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}
