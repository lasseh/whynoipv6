// Package lock is the advisory-lock singleton coordination
// (04-lifecycle-scheduling.md §10): session-scoped Postgres advisory locks
// gate every singleton job across the two identical crawler processes and
// v6ctl-triggered runs. Adding a lock key is a spec change to 04 §10.
package lock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ClassID is the whynoipv6 advisory-lock namespace; never change.
// (Two-int form — distinct from golang-migrate's single-bigint lock.)
const ClassID int32 = 60660

// The complete job registry (04 §10).
const (
	JobDailyTick     int32 = 1 // the daily tick, all steps, one lock for the whole sequence
	JobTrancoImport  int32 = 2 // Tranco import (coordinator cycle + `v6ctl tranco import`)
	JobCampaignSync  int32 = 3 // campaign sync (tick step 5 + webhook + `v6ctl campaign sync`)
	JobDatasetExport int32 = 4 // nightly dataset snapshot export (`v6ctl export`)
)

// JobName returns the log name for a job key.
func JobName(job int32) string {
	switch job {
	case JobDailyTick:
		return "daily_tick"
	case JobTrancoImport:
		return "tranco_import"
	case JobCampaignSync:
		return "campaign_sync"
	case JobDatasetExport:
		return "dataset_export"
	default:
		return fmt.Sprintf("job_%d", job)
	}
}

// ErrHeld is returned by TryRun when the lock is busy.
var ErrHeld = errors.New("singleton lock held elsewhere")

// TryRun acquires (ClassID, job) via pg_try_advisory_lock on a connection
// checked out from the pool for the job's WHOLE duration. If the lock is
// busy it returns ErrHeld without running fn. On return (or process crash /
// connection loss) the lock is released: pg_advisory_unlock on the success
// path, session teardown otherwise.
func TryRun(ctx context.Context, pool *pgxpool.Pool, job int32, fn func(ctx context.Context) error) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("lock %s: acquire conn: %w", JobName(job), err)
	}
	defer conn.Release()

	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1, $2)", ClassID, job).Scan(&got); err != nil {
		return fmt.Errorf("lock %s: try: %w", JobName(job), err)
	}
	if !got {
		return ErrHeld
	}
	defer unlock(ctx, conn, job)
	return fn(ctx)
}

// Run is the blocking variant: pg_advisory_lock with a wait deadline that
// covers the pool acquire as well as the lock wait. Used by v6ctl (and the
// tick's nested campaign-sync step) so an explicitly requested run always
// executes once the concurrent one finishes; deadline exceeded → error
// "another <job> is running" (v6ctl exits 1 with it); a cancelled caller
// context is reported as such, not as contention.
func Run(ctx context.Context, pool *pgxpool.Pool, job int32, wait time.Duration, fn func(ctx context.Context) error) error {
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	conn, err := pool.Acquire(waitCtx)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("lock %s: %w", JobName(job), ctx.Err())
		}
		if waitCtx.Err() != nil {
			return fmt.Errorf("another %s is running", JobName(job))
		}
		return fmt.Errorf("lock %s: acquire conn: %w", JobName(job), err)
	}
	defer conn.Release()

	if _, err := conn.Exec(waitCtx, "SELECT pg_advisory_lock($1, $2)", ClassID, job); err != nil {
		if waitCtx.Err() != nil {
			// The interrupted backend session may be poisoned; drop it so the
			// half-taken lock (if any) is released with the session.
			conn.Conn().Close(context.WithoutCancel(ctx)) //nolint:errcheck // teardown
			if ctx.Err() != nil {
				return fmt.Errorf("lock %s: %w", JobName(job), ctx.Err())
			}
			return fmt.Errorf("another %s is running", JobName(job))
		}
		return fmt.Errorf("lock %s: wait: %w", JobName(job), err)
	}
	defer unlock(ctx, conn, job)
	return fn(ctx)
}

// unlockTimeout bounds the explicit unlock. The lock connection sits idle
// for the whole job, so a dead peer is only discovered here; without a
// deadline the tick or coordinator would hang until TCP gives up.
const unlockTimeout = 5 * time.Second

// unlock releases the lock explicitly. A backend that does not answer in
// time is closed rather than released: session teardown frees the lock,
// and a possibly poisoned connection never returns to the pool.
func unlock(ctx context.Context, conn *pgxpool.Conn, job int32) {
	uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()
	if _, err := conn.Exec(uctx, "SELECT pg_advisory_unlock($1, $2)", ClassID, job); err != nil {
		conn.Conn().Close(uctx) //nolint:errcheck // teardown
	}
}
