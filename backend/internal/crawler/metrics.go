package crawler

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/bits"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/observe"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// Checkpoint constants (04 §15.1 — compile-time, never config).
const checkpointEvery = 1000

// idleCheckpointAfter is a var only so tests can shrink the idle window.
var idleCheckpointAfter = 5 * time.Minute

// histBuckets: powers of two from 1 ms to 2^19 ms plus overflow (21 counters).
const histBuckets = 21

// counters is one interval's tallies. Checkpoint swaps a fresh set in under
// the mutex and then works entirely from the snapshot, which keeps the
// QueueDepth probe and the INSERT off the lock and makes a failed write
// recoverable: the snapshot folds back into the live set instead of being
// dropped.
type counters struct {
	processed, succeeded, failed                     int32
	dims                                             map[domain.Dimension]map[domain.Observation]int
	leaseLost, commitErrors, unresolvable, recovered int
	bootstraps, confirmedTrans                       int
	skips                                            map[string]int
	hist                                             [histBuckets]int
}

func newCounters() counters {
	return counters{
		dims:  map[domain.Dimension]map[domain.Observation]int{},
		skips: map[string]int{},
	}
}

// add merges o into c — the fold-back after a checkpoint INSERT failed.
func (c *counters) add(o *counters) {
	c.processed += o.processed
	c.succeeded += o.succeeded
	c.failed += o.failed
	c.leaseLost += o.leaseLost
	c.commitErrors += o.commitErrors
	c.unresolvable += o.unresolvable
	c.recovered += o.recovered
	c.bootstraps += o.bootstraps
	c.confirmedTrans += o.confirmedTrans
	for d, counts := range o.dims {
		if c.dims[d] == nil {
			c.dims[d] = map[domain.Observation]int{}
		}
		for obs, n := range counts {
			c.dims[d][obs] += n
		}
	}
	for job, n := range o.skips {
		c.skips[job] += n
	}
	for i, n := range o.hist {
		c.hist[i] += n
	}
}

// Metrics is the per-process checkpointed crawler_metrics writer
// (04 §15). All counters are per-interval deltas, reset at every checkpoint.
type Metrics struct {
	pool   *pgxpool.Pool
	runID  uuid.UUID
	worker string

	// GeoIPBuildEpoch reports the loaded mmdb build date (nil = NULL).
	GeoIPBuildEpoch func() time.Time
	// Heartbeat fires after every successful commit record (nil = disabled);
	// the notify client throttles it.
	Heartbeat func()

	// due carries the per-1000 signal to Run. Capacity 1 and a
	// non-blocking send: a scan never waits on the checkpointer, and a
	// signal raised while one is already pending simply joins it.
	due chan struct{}

	mu             sync.Mutex
	c              counters
	lastCheckpoint time.Time
}

// NewMetrics builds the writer. worker is "<hostname>:<pid>" (04 §15.1).
func NewMetrics(pool *pgxpool.Pool, runID uuid.UUID, worker string) *Metrics {
	return &Metrics{
		pool: pool, runID: runID, worker: worker,
		due:            make(chan struct{}, 1),
		c:              newCounters(),
		lastCheckpoint: time.Now(),
	}
}

// RecordScan tallies one processed domain (04 §15.1/§15.2). commitErr is a
// non-lease failure; res carries lease-lost and transitions.
func (m *Metrics) RecordScan(obs *observe.Observations, unresolvable bool,
	res CommitResult, commitErr error, dur time.Duration,
) {
	m.mu.Lock()
	m.c.processed++
	switch {
	case commitErr != nil:
		m.c.failed++
		m.c.commitErrors++
	case res.LeaseLost:
		m.c.failed++
		m.c.leaseLost++
	default:
		m.c.succeeded++
		m.c.bootstraps += res.Bootstraps
		for _, tr := range res.Transitions {
			if !shadowTransition(tr.Dim, tr.New) { // shadows write no changelog row (03 §11)
				m.c.confirmedTrans++
			}
		}
	}
	if obs != nil {
		for d, o := range map[domain.Dimension]domain.Observation{
			domain.DimBase: obs.Base, domain.DimWWW: obs.WWW, domain.DimNS: obs.NS,
			domain.DimMX: obs.MX, domain.DimConn: obs.Conn, domain.DimResources: obs.Resources,
		} {
			if m.c.dims[d] == nil {
				m.c.dims[d] = map[domain.Observation]int{}
			}
			m.c.dims[d][o]++
		}
	}
	if unresolvable {
		m.c.unresolvable++
	}
	m.c.recordLatency(dur)
	due := m.c.processed%checkpointEvery == 0
	m.mu.Unlock()

	if commitErr == nil && !res.LeaseLost && m.Heartbeat != nil {
		m.Heartbeat()
	}
	if due {
		// Signal the checkpointer instead of writing here: the INSERT and
		// its O(due-set) QueueDepth probe would otherwise run inside one of
		// the worker slots, and two writers for one interval could each
		// record a near-empty one (04 §13 row 7 — one checkpointer).
		select {
		case m.due <- struct{}{}:
		default:
		}
	}
}

// RecordRecovered counts step-R executions.
func (m *Metrics) RecordRecovered() { m.mu.Lock(); m.c.recovered++; m.mu.Unlock() }

// RecordSingletonSkip counts a TryRun ErrHeld skip by job name.
func (m *Metrics) RecordSingletonSkip(job string) {
	m.mu.Lock()
	m.c.skips[job]++
	m.mu.Unlock()
}

func (c *counters) recordLatency(dur time.Duration) {
	ms := dur.Milliseconds()
	idx := 0
	if ms > 0 {
		idx = bits.Len64(uint64(ms)) - 1 // integer log2 of the millisecond count
	}
	if idx >= histBuckets {
		idx = histBuckets - 1
	}
	c.hist[idx]++
}

// percentile estimates a percentile by linear interpolation within the
// log-scale bucket (04 §15.1 Decision).
func (c *counters) percentile(p float64) int32 {
	total := 0
	for _, n := range c.hist {
		total += n
	}
	if total == 0 {
		return 0
	}
	target := p * float64(total)
	cum := 0.0
	for i, n := range c.hist {
		if n == 0 {
			continue
		}
		lo, hi := float64(int64(1)<<i), float64(int64(1)<<(i+1))
		if cum+float64(n) >= target {
			frac := (target - cum) / float64(n)
			return int32(lo + frac*(hi-lo))
		}
		cum += float64(n)
	}
	return int32(int64(1) << (histBuckets - 1))
}

// Run is the checkpointer goroutine 04 §13 row 7 names: the single writer of
// every crawler_metrics row except the final one. It serves the per-1000
// signal RecordScan raises and writes an idle row whenever none has been
// written for idleCheckpointAfter (which keeps alert A1 valid on a drained
// frontier). Stops with ctx; whatever is still tallied then goes into the
// final checkpoint the caller writes.
func (m *Metrics) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.due:
			m.Checkpoint(ctx, false)
		case <-ticker.C:
			if m.idleDue() {
				m.Checkpoint(ctx, false)
			}
		}
	}
}

// idleDue reports whether the idle window has elapsed with no checkpoint
// written — the rule Run's ticker consults, named so a test can assert it
// without waiting out a real window.
func (m *Metrics) idleDue() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return time.Since(m.lastCheckpoint) >= idleCheckpointAfter
}

// Checkpoint flushes the current interval as one crawler_metrics row and
// resets the counters. final=true writes the shutdown row (is_final).
func (m *Metrics) Checkpoint(ctx context.Context, final bool) {
	// Swap a fresh set in and work from the snapshot: everything below —
	// the marshal, the queue-depth probe, the INSERT — runs off the lock.
	m.mu.Lock()
	now := time.Now()
	since := m.lastCheckpoint
	snap := m.c
	m.c = newCounters()
	m.lastCheckpoint = now
	m.mu.Unlock()

	processed, succeeded, failed := snap.processed, snap.succeeded, snap.failed
	var qps float32
	if interval := now.Sub(since).Seconds(); interval > 0 {
		qps = float32(float64(processed) / interval)
	}
	p50, p99 := snap.percentile(0.50), snap.percentile(0.99)

	raw, err := json.Marshal(snap.dimCounters())
	if err != nil {
		raw = []byte("{}")
	}

	q := db.New(m.pool)
	depth64, err := q.QueueDepth(ctx)
	if err != nil {
		slog.Warn("queue depth probe failed", "err", err.Error())
	}
	depth := int32(depth64) //nolint:gosec // a due-set count, far below int32

	params := db.InsertCrawlerMetricsParams{
		RunID:       pgtype.UUID{Bytes: m.runID, Valid: true},
		Worker:      m.worker,
		Processed:   &processed,
		Succeeded:   &succeeded,
		Failed:      &failed,
		Qps:         &qps,
		P50Ms:       &p50,
		P99Ms:       &p99,
		QueueDepth:  &depth,
		DimCounters: raw,
		IsFinal:     final,
	}
	if m.GeoIPBuildEpoch != nil {
		if epoch := m.GeoIPBuildEpoch(); !epoch.IsZero() {
			params.GeoipBuildEpoch = pgtype.Timestamptz{Time: epoch, Valid: true}
		}
	}
	if err := q.InsertCrawlerMetrics(ctx, params); err != nil {
		// The interval is not lost: fold it back so the next row carries the
		// merged delta. /stats/crawler sums processed, so dropping a failed
		// interval would silently under-report up to checkpointEvery scans.
		slog.Error("metrics checkpoint failed, folding the interval back",
			"err", err.Error(), "processed", processed)
		m.foldBack(&snap, since)
	}
}

// foldBack merges a failed checkpoint's snapshot into the live counters and
// rewinds lastCheckpoint, so the next row's qps still spans the whole range
// the tallies came from.
func (m *Metrics) foldBack(snap *counters, since time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.c.add(snap)
	if since.Before(m.lastCheckpoint) {
		m.lastCheckpoint = since
	}
}

// dimCounters builds the §15.2 JSONB payload; zero-count keys omitted.
// Checkpoint marshals it from the snapshot.
func (c *counters) dimCounters() map[string]any {
	out := map[string]any{}
	for d, counts := range c.dims {
		if len(counts) == 0 {
			continue
		}
		obj := map[string]int{}
		for o, n := range counts {
			if n > 0 {
				obj[string(o)] = n
			}
		}
		if len(obj) > 0 {
			out[string(d)] = obj
		}
	}
	if c.leaseLost > 0 {
		out["lease_lost"] = c.leaseLost
	}
	if c.commitErrors > 0 {
		out["commit_error"] = c.commitErrors
	}
	if c.unresolvable > 0 {
		out["unresolvable"] = c.unresolvable
	}
	if c.recovered > 0 {
		out["recovered"] = c.recovered
	}
	if c.bootstraps > 0 {
		out["bootstrap_commits"] = c.bootstraps
	}
	if c.confirmedTrans > 0 {
		out["confirmed_transitions"] = c.confirmedTrans
	}
	if len(c.skips) > 0 {
		out["singleton_skipped"] = c.skips
	}
	return out
}
