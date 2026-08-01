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

// Metrics is the per-process checkpointed crawler_metrics writer
// (04 §15). All counters are per-interval deltas, reset at every checkpoint.
type Metrics struct {
	pool   *pgxpool.Pool
	runID  uuid.UUID
	worker string

	// ActiveSlots reports busy worker slots at write time (nil = 0).
	ActiveSlots func() int32
	// GeoIPBuildEpoch reports the loaded mmdb build date (nil = NULL).
	GeoIPBuildEpoch func() time.Time
	// Heartbeat fires after every successful commit record (nil = disabled);
	// the notify client throttles it.
	Heartbeat func()

	mu             sync.Mutex
	processed      int32
	succeeded      int32
	failed         int32
	dims           map[domain.Dimension]map[domain.Observation]int
	leaseLost      int
	commitErrors   int
	unresolvable   int
	deadTriggered  int
	recovered      int
	bootstraps     int
	confirmedTrans int
	skips          map[string]int
	hist           [histBuckets]int
	lastCheckpoint time.Time
}

// NewMetrics builds the writer. worker is "<hostname>:<pid>" (04 §15.1).
func NewMetrics(pool *pgxpool.Pool, runID uuid.UUID, worker string) *Metrics {
	return &Metrics{
		pool: pool, runID: runID, worker: worker,
		dims:           map[domain.Dimension]map[domain.Observation]int{},
		skips:          map[string]int{},
		lastCheckpoint: time.Now(),
	}
}

// RunID returns the process run id (stamped on the per-run child logger).
func (m *Metrics) RunID() uuid.UUID { return m.runID }

// RecordScan tallies one processed domain (04 §15.1/§15.2). commitErr is a
// non-lease failure; res carries lease-lost and transitions.
func (m *Metrics) RecordScan(ctx context.Context, obs *observe.Observations, unresolvable bool,
	res CommitResult, commitErr error, dur time.Duration,
) {
	m.mu.Lock()
	m.processed++
	switch {
	case commitErr != nil:
		m.failed++
		m.commitErrors++
	case res.LeaseLost:
		m.failed++
		m.leaseLost++
	default:
		m.succeeded++
		m.bootstraps += res.Bootstraps
		for _, tr := range res.Transitions {
			if !shadowTransition(tr.Dim, tr.New) { // shadows write no changelog row (03 §11)
				m.confirmedTrans++
			}
		}
	}
	if obs != nil {
		for d, o := range map[domain.Dimension]domain.Observation{
			domain.DimBase: obs.Base, domain.DimWWW: obs.WWW, domain.DimNS: obs.NS,
			domain.DimMX: obs.MX, domain.DimConn: obs.Conn, domain.DimResources: obs.Resources,
		} {
			if m.dims[d] == nil {
				m.dims[d] = map[domain.Observation]int{}
			}
			m.dims[d][o]++
		}
	}
	if unresolvable {
		m.unresolvable++
	}
	m.recordLatencyLocked(dur)
	due := m.processed%checkpointEvery == 0
	m.mu.Unlock()

	if commitErr == nil && !res.LeaseLost && m.Heartbeat != nil {
		m.Heartbeat()
	}
	if due {
		m.Checkpoint(ctx, false)
	}
}

// RecordDeadTriggered / RecordRecovered tally lifecycle events (04 §15.2).
func (m *Metrics) RecordDeadTriggered() { m.mu.Lock(); m.deadTriggered++; m.mu.Unlock() }

// RecordRecovered counts step-R executions.
func (m *Metrics) RecordRecovered() { m.mu.Lock(); m.recovered++; m.mu.Unlock() }

// RecordSingletonSkip counts a TryRun ErrHeld skip by job name.
func (m *Metrics) RecordSingletonSkip(job string) {
	m.mu.Lock()
	m.skips[job]++
	m.mu.Unlock()
}

func (m *Metrics) recordLatencyLocked(dur time.Duration) {
	ms := dur.Milliseconds()
	idx := 0
	if ms > 0 {
		idx = bits.Len64(uint64(ms)) - 1 // integer log2 of the millisecond count
	}
	if idx >= histBuckets {
		idx = histBuckets - 1
	}
	m.hist[idx]++
}

// percentileLocked estimates a percentile by linear interpolation within the
// log-scale bucket (04 §15.1 Decision).
func (m *Metrics) percentileLocked(p float64) int32 {
	total := 0
	for _, n := range m.hist {
		total += n
	}
	if total == 0 {
		return 0
	}
	target := p * float64(total)
	cum := 0.0
	for i, n := range m.hist {
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

// RunIdleLoop writes an idle checkpoint whenever none has been written for
// idleCheckpointAfter (keeps alert A1 valid on a drained frontier).
func (m *Metrics) RunIdleLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			stale := time.Since(m.lastCheckpoint) >= idleCheckpointAfter
			m.mu.Unlock()
			if stale {
				m.Checkpoint(ctx, false)
			}
		}
	}
}

// Checkpoint flushes the current interval as one crawler_metrics row and
// resets the counters. final=true writes the shutdown row (is_final).
func (m *Metrics) Checkpoint(ctx context.Context, final bool) {
	m.mu.Lock()
	now := time.Now()
	interval := now.Sub(m.lastCheckpoint).Seconds()
	processed, succeeded, failed := m.processed, m.succeeded, m.failed
	var qps float32
	if interval > 0 {
		qps = float32(float64(processed) / interval)
	}
	p50, p99 := m.percentileLocked(0.50), m.percentileLocked(0.99)
	counters := m.dimCountersLocked()
	m.processed, m.succeeded, m.failed = 0, 0, 0
	m.dims = map[domain.Dimension]map[domain.Observation]int{}
	m.leaseLost, m.commitErrors, m.unresolvable, m.deadTriggered, m.recovered = 0, 0, 0, 0, 0
	m.bootstraps, m.confirmedTrans = 0, 0
	m.skips = map[string]int{}
	m.hist = [histBuckets]int{}
	m.lastCheckpoint = now
	m.mu.Unlock()

	q := db.New(m.pool)
	depth64, err := q.QueueDepth(ctx)
	if err != nil {
		slog.Warn("queue depth probe failed", "err", err.Error())
	}
	depth := int32(depth64)

	var slots int32
	if m.ActiveSlots != nil {
		slots = m.ActiveSlots()
	}
	params := db.InsertCrawlerMetricsParams{
		RunID:       pgtype.UUID{Bytes: m.runID, Valid: true},
		Worker:      m.worker,
		Processed:   &processed,
		Succeeded:   &succeeded,
		Failed:      &failed,
		Qps:         &qps,
		P50Ms:       &p50,
		P99Ms:       &p99,
		ActiveSlots: &slots,
		QueueDepth:  &depth,
		DimCounters: counters,
		IsFinal:     final,
	}
	if m.GeoIPBuildEpoch != nil {
		if epoch := m.GeoIPBuildEpoch(); !epoch.IsZero() {
			params.GeoipBuildEpoch = pgtype.Timestamptz{Time: epoch, Valid: true}
		}
	}
	if err := q.InsertCrawlerMetrics(ctx, params); err != nil {
		slog.Error("metrics checkpoint failed", "err", err.Error())
	}
}

// dimCountersLocked builds the §15.2 JSONB payload; zero-count keys omitted.
func (m *Metrics) dimCountersLocked() []byte {
	out := map[string]any{}
	for d, counts := range m.dims {
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
	if m.leaseLost > 0 {
		out["lease_lost"] = m.leaseLost
	}
	if m.commitErrors > 0 {
		out["commit_error"] = m.commitErrors
	}
	if m.unresolvable > 0 {
		out["unresolvable"] = m.unresolvable
	}
	if m.deadTriggered > 0 {
		out["dead_triggered"] = m.deadTriggered
	}
	if m.recovered > 0 {
		out["recovered"] = m.recovered
	}
	if m.bootstraps > 0 {
		out["bootstrap_commits"] = m.bootstraps
	}
	if m.confirmedTrans > 0 {
		out["confirmed_transitions"] = m.confirmedTrans
	}
	if len(m.skips) > 0 {
		out["singleton_skipped"] = m.skips
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return []byte("{}")
	}
	return raw
}
