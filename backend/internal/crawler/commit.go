package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/observe"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// CommitConfig carries the commit machine's knobs (03 §2; registry: 09-ops.md).
type CommitConfig struct {
	MinConfirmSpacing time.Duration // anti_flap.min_confirm_spacing, 12h
	DeadStreak        int16         // lifecycle.dead_streak, 7
	ResourcesEnabled  bool          // crawler.resources.enabled
	Schedule          ScheduleConfig
}

// Attribution is this scan's resolved (asn_id, country_id) pair (03 §3 A).
type Attribution struct {
	AsnID     int32
	CountryID int32
}

// CommitInput carries one domain's commit unit inputs (03 §3, §16).
type CommitInput struct {
	Snapshot     ClaimedDomain
	Obs          observe.Observations // core + informational + latency (02 §7.2)
	Unresolvable bool                 // the dead signal U (03 §4)
	Attribution  *Attribution         // nil = deferred (base non-definitive)
	Discovered   []string             // canonical resource hosts; consumed only when DiscoveryOK
	DiscoveryOK  bool                 // resources enabled AND discovery status ok
	BreakerOpen  bool                 // consensus.FastLaneSuppressed()
	Details      []byte               // scan_detail JSON payload (03 §14.2)
	DurationMS   int32
	T            time.Time // fixed once per domain
}

// Transition is one confirmed changelog row (dim, old, new).
type Transition struct {
	Dim domain.Dimension
	Old domain.IPv6Status
	New domain.IPv6Status
}

// CommitResult reports one commit's outcome.
type CommitResult struct {
	LeaseLost   bool
	Transitions []Transition
	Bootstraps  int
}

// commitUnit is the computed write unit: everything the flush queues.
type commitUnit struct {
	domainID    int64
	host        string
	lease       time.Time
	params      db.CommitDomainParams
	changelog   []db.InsertChangelogParams
	scan        db.InsertScanParams
	detail      db.InsertScanDetailParams
	resources   []string // hosts to ensure/upsert; empty = no link statements
	pruneLinks  bool
	transitions []Transition
	bootstraps  int
}

// dimWork is one dimension's working confirm/pending state during step 2.
type dimWork struct {
	status   *domain.IPv6Status
	pending  *domain.IPv6Status
	count    int16
	since    *time.Time
	observed *domain.Observation // nil only for the excluded resources dim
}

// ComputeCommit runs 03 §5 steps 0–8 as a pure function: given the claimed
// snapshot and this scan's inputs it computes the next state + changelog
// rows with no I/O, so 10-testing §5 can drive it table-style.
func ComputeCommit(in *CommitInput, cfg *CommitConfig) (*commitUnit, error) {
	s := in.Snapshot
	t := in.T

	obsOf := map[domain.Dimension]domain.Observation{
		domain.DimBase: in.Obs.Base, domain.DimWWW: in.Obs.WWW,
		domain.DimNS: in.Obs.NS, domain.DimMX: in.Obs.MX,
		domain.DimConn: in.Obs.Conn, domain.DimResources: in.Obs.Resources,
	}

	// Step 0 — counting gate (03 §7).
	counting := s.LastCountedAt == nil || t.Sub(*s.LastCountedAt) >= cfg.MinConfirmSpacing

	// Step 0b — dimension set (fixed order).
	dims := []domain.Dimension{domain.DimBase, domain.DimWWW, domain.DimNS, domain.DimMX, domain.DimConn}
	if cfg.ResourcesEnabled {
		dims = append(dims, domain.DimResources)
	}

	// Working state from the snapshot.
	work := map[domain.Dimension]*dimWork{}
	for _, d := range domain.Dimensions {
		ds := s.Dims[d]
		work[d] = &dimWork{status: ds.Status, pending: ds.Pending, count: ds.PendingCount, since: ds.Since}
	}

	disabled := s.Disabled
	disabledReason := s.DisabledReason
	disabledAt := s.DisabledAt
	info := in.Obs // informational values written in step 8 (may be nulled by step R first)

	// Step 1 — lifecycle: dead detection & recovery.
	var deadStreak int16
	if in.Unresolvable {
		deadStreak = min(s.DeadStreak+1, cfg.DeadStreak)
	} else {
		deadStreak = 0
		if obsOf[domain.DimBase].Definitive() && s.Disabled &&
			s.DisabledReason != nil && *s.DisabledReason == domain.DisabledDead {
			// Step R (03 §6): re-enable + full reset; this scan's definitive
			// observations then bootstrap-commit against NULL state below.
			disabled = false
			disabledReason = nil
			disabledAt = nil
			for _, d := range domain.Dimensions {
				work[d] = &dimWork{}
			}
			// Informational columns and latency reset then re-populate in
			// step 8 from this scan (nothing extra to do — step 8 always
			// overwrites verbatim).
		}
	}

	// Step 2 — per-dimension confirm/pending loop.
	var changelog []db.InsertChangelogParams
	var transitions []Transition
	bootstraps := 0
	anyDefinitive := false
	for _, d := range dims {
		o := obsOf[d]
		if o == domain.ObsPartial {
			return nil, fmt.Errorf("commit defect: partial observation on core dimension %s", d)
		}
		w := work[d]
		obsCopy := o
		w.observed = &obsCopy // ALWAYS recorded, even error/inconsistent
		if !o.Definitive() {
			continue // non-definitive: touch nothing
		}
		anyDefinitive = true
		if !counting {
			continue // record-only scan
		}
		val := domain.IPv6Status(o)
		switch {
		case w.status == nil: // BOOTSTRAP: commits immediately, NO changelog
			w.status = &val
			w.since = &t
			w.pending = nil
			w.count = 0
			bootstraps++
		case *w.status == val: // steady state: cancels any pending candidate
			w.pending = nil
			w.count = 0
		case w.pending != nil && *w.pending == val: // pending re-observed
			w.count++
			if w.count >= domain.ConfirmN(d) {
				changelog = append(changelog, db.InsertChangelogParams{
					DomainID: s.ID, Ts: tstz(t), Field: string(d),
					OldValue: db.Ipv6Status(*w.status), NewValue: db.Ipv6Status(val),
				})
				transitions = append(transitions, Transition{Dim: d, Old: *w.status, New: val})
				w.status = &val
				w.since = &t
				w.pending = nil
				w.count = 0
			}
		default: // new candidate (or the candidate changed)
			w.pending = &val
			w.count = 1
		}
	}
	lastCounted := s.LastCountedAt
	if counting && anyDefinitive {
		lastCounted = &t
	}

	// Step 3 — classification over the post-step-2 CONFIRMED values.
	confirmed := map[domain.Dimension]*domain.IPv6Status{}
	for _, d := range domain.Dimensions {
		confirmed[d] = work[d].status
	}
	class, flags, gold := domain.Classify(confirmed)

	// Step 4 — dead trigger.
	deadTriggered := false
	if !disabled && deadStreak >= cfg.DeadStreak {
		disabled = true
		reason := domain.DisabledDead
		disabledReason = &reason
		disabledAt = &t
		deadStreak = 0
		deadTriggered = true
	}

	// Step 5 — error_streak maintenance (step 4's reset wins).
	var errorStreak int16
	if !obsOf[domain.DimBase].Definitive() || !obsOf[domain.DimWWW].Definitive() {
		if s.ErrorStreak < 32767 {
			errorStreak = s.ErrorStreak + 1
		} else {
			errorStreak = 32767
		}
	}
	if deadTriggered {
		errorStreak = 0
	}

	// Step 6 — scheduling.
	nextCheck := schedule(cfg.Schedule, disabled, obsOf[domain.DimBase], obsOf[domain.DimWWW],
		errorStreak, s.Rank, in.BreakerOpen, t)

	// Step 7 — attribution (deferred on non-definitive base).
	asnID, countryID := s.AsnID, s.CountryID
	if obsOf[domain.DimBase].Definitive() && in.Attribution != nil {
		asnID, countryID = in.Attribution.AsnID, in.Attribution.CountryID
	}

	if flags == nil {
		flags = []string{}
	}
	params := db.CommitDomainParams{
		Classification: db.Classification(class),
		ClassFlags:     flags,
		Gold:           gold,
		AsnID:          asnID,
		CountryID:      countryID,
		Disabled:       disabled,
		DisabledAt:     tstzPtr(disabledAt),
		DeadStreak:     deadStreak,
		ErrorStreak:    errorStreak,
		NextCheckAt:    tstz(nextCheck),
		Ts:             tstz(t),
		LastCountedAt:  tstzPtr(lastCounted),
		DomainID:       s.ID,
		// Step 8 — informational columns, overwritten verbatim every commit.
		DnssecObserved: obsDB(info.DNSSEC),
		PtrObserved:    obsDB(info.PTR),
		SmtpObserved:   obsDB(info.SMTP),
		ParityObserved: obsDB(info.Parity),
		LatencyV4Ms:    info.LatencyV4Ms,
		LatencyV6Ms:    info.LatencyV6Ms,
	}
	if disabledReason != nil {
		r := db.DisabledReason(*disabledReason)
		params.DisabledReason = &r
	}
	bindDim(&params.BaseStatus, &params.BaseObserved, &params.BasePending, &params.BasePendingCount, &params.BaseSince, work[domain.DimBase])
	bindDim(&params.WwwStatus, &params.WwwObserved, &params.WwwPending, &params.WwwPendingCount, &params.WwwSince, work[domain.DimWWW])
	bindDim(&params.NsStatus, &params.NsObserved, &params.NsPending, &params.NsPendingCount, &params.NsSince, work[domain.DimNS])
	bindDim(&params.MxStatus, &params.MxObserved, &params.MxPending, &params.MxPendingCount, &params.MxSince, work[domain.DimMX])
	bindDim(&params.ConnStatus, &params.ConnObserved, &params.ConnPending, &params.ConnPendingCount, &params.ConnSince, work[domain.DimConn])
	bindDim(&params.ResourcesStatus, &params.ResourcesObserved, &params.ResourcesPending, &params.ResourcesPendingCount, &params.ResourcesSince, work[domain.DimResources])

	u := &commitUnit{
		domainID:    s.ID,
		host:        s.Host,
		lease:       s.ClaimedAt,
		params:      params,
		changelog:   changelog,
		transitions: transitions,
		bootstraps:  bootstraps,
		scan: db.InsertScanParams{
			DomainID: s.ID, Ts: tstz(t),
			Base: db.Observation(in.Obs.Base), Www: db.Observation(in.Obs.WWW),
			Ns: db.Observation(in.Obs.NS), Mx: db.Observation(in.Obs.MX),
			Conn: db.Observation(in.Obs.Conn), Resources: db.Observation(in.Obs.Resources),
			Dnssec: obsDB(in.Obs.DNSSEC), Ptr: obsDB(in.Obs.PTR),
			Smtp: obsDB(in.Obs.SMTP), Parity: obsDB(in.Obs.Parity),
			LatencyV4Ms: in.Obs.LatencyV4Ms, LatencyV6Ms: in.Obs.LatencyV6Ms,
			Classification: db.Classification(class),
			CountryID:      &countryID, AsnID: &asnID,
		},
		detail: db.InsertScanDetailParams{
			DomainID: s.ID, Ts: tstz(t), Details: in.Details, DurationMs: &in.DurationMS,
		},
	}
	if cfg.ResourcesEnabled && in.DiscoveryOK {
		u.resources = in.Discovered
		u.pruneLinks = true
	}
	return u, nil
}

func bindDim(status **db.Ipv6Status, observed **db.Observation, pending **db.Ipv6Status,
	count *int16, since *pgtype.Timestamptz, w *dimWork,
) {
	if w.status != nil {
		v := db.Ipv6Status(*w.status)
		*status = &v
	}
	if w.observed != nil {
		v := db.Observation(*w.observed)
		*observed = &v
	}
	if w.pending != nil {
		v := db.Ipv6Status(*w.pending)
		*pending = &v
	}
	*count = w.count
	*since = tstzPtr(w.since)
}

func obsDB(o domain.Observation) *db.Observation {
	if o == "" {
		return nil
	}
	v := db.Observation(o)
	return &v
}

func tstz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func tstzPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// The two multi-CTE resource statements live here as constants (the
// 05-schema.md §10.2 escape hatch); the SQL text is 03 §12.3's.
const (
	sqlUpsertDomainResource = `WITH rh AS (
  SELECT id FROM resource_host WHERE host = $1
), ins AS (
  INSERT INTO domain_resource (domain_id, resource_host_id, source, required, first_seen, last_seen)
  SELECT $2, rh.id, 'discovered', TRUE, $3, $3 FROM rh
  ON CONFLICT (domain_id, resource_host_id) DO NOTHING
  RETURNING resource_host_id
), bump AS (
  UPDATE resource_host SET dependent_count = dependent_count + 1
  WHERE id IN (SELECT resource_host_id FROM ins)
)
UPDATE domain_resource SET last_seen = $3
WHERE domain_resource.domain_id = $2
  AND domain_resource.resource_host_id IN (SELECT id FROM rh)
  AND NOT EXISTS (SELECT 1 FROM ins)`

	sqlPruneDomainResources = `WITH del AS (
  DELETE FROM domain_resource
  WHERE domain_id = $1
    AND source = 'discovered'
    AND last_seen < $2::timestamptz - INTERVAL '30 days'
  RETURNING resource_host_id
)
UPDATE resource_host SET dependent_count = dependent_count - 1
WHERE id IN (SELECT resource_host_id FROM del)`
)

// Committer flushes commit units under the lease fence (03 §12–§13).
type Committer struct {
	pool *pgxpool.Pool
	cfg  *CommitConfig
	log  *slog.Logger

	// Counters consumed by the metrics checkpointer (03 §15).
	LeaseLost    atomic.Int64
	CommitErrors atomic.Int64
}

// NewCommitter builds the committer.
func NewCommitter(pool *pgxpool.Pool, cfg *CommitConfig, log *slog.Logger) *Committer {
	return &Committer{pool: pool, cfg: cfg, log: log}
}

// Commit runs 03 §5–§12 for one domain: pure state computation, then the
// fenced single-round-trip flush. LeaseLost=true means nothing was written.
func (c *Committer) Commit(ctx context.Context, in *CommitInput) (CommitResult, error) {
	u, err := ComputeCommit(in, c.cfg)
	if err != nil {
		c.CommitErrors.Add(1)
		c.log.Error("commit compute failed", "domain", in.Snapshot.Host, "err", err.Error())
		return CommitResult{}, err
	}
	leaseLost, err := c.flush(ctx, u)
	if err != nil {
		c.CommitErrors.Add(1)
		c.log.Error("commit flush failed", "domain", in.Snapshot.Host, "err", err.Error())
		return CommitResult{}, err
	}
	if leaseLost {
		return CommitResult{LeaseLost: true}, nil
	}
	return CommitResult{Transitions: u.transitions, Bootstraps: u.bootstraps}, nil
}

// flush sends the whole unit as one pgx.Batch in one pgx.Tx (03 §12.2).
func (c *Committer) flush(ctx context.Context, u *commitUnit) (leaseLost bool, err error) {
	batch := &pgx.Batch{}
	p := u.params
	batch.Queue(db.CommitDomain,
		p.BaseStatus, p.BaseObserved, p.BasePending, p.BasePendingCount, p.BaseSince,
		p.WwwStatus, p.WwwObserved, p.WwwPending, p.WwwPendingCount, p.WwwSince,
		p.NsStatus, p.NsObserved, p.NsPending, p.NsPendingCount, p.NsSince,
		p.MxStatus, p.MxObserved, p.MxPending, p.MxPendingCount, p.MxSince,
		p.ConnStatus, p.ConnObserved, p.ConnPending, p.ConnPendingCount, p.ConnSince,
		p.ResourcesStatus, p.ResourcesObserved, p.ResourcesPending, p.ResourcesPendingCount, p.ResourcesSince,
		p.DnssecObserved, p.PtrObserved, p.SmtpObserved, p.ParityObserved,
		p.LatencyV4Ms, p.LatencyV6Ms,
		p.Classification, p.ClassFlags, p.Gold,
		p.AsnID, p.CountryID,
		p.Disabled, p.DisabledReason, p.DisabledAt,
		p.DeadStreak, p.ErrorStreak,
		p.NextCheckAt, p.Ts, p.LastCountedAt,
		p.DomainID, tstz(u.lease))
	for _, cl := range u.changelog {
		batch.Queue(db.InsertChangelog, cl.DomainID, cl.Ts, cl.Field, cl.OldValue, cl.NewValue)
	}
	sc := u.scan
	batch.Queue(db.InsertScan,
		sc.DomainID, sc.Ts, sc.Base, sc.Www, sc.Ns, sc.Mx, sc.Conn, sc.Resources,
		sc.Dnssec, sc.Ptr, sc.Smtp, sc.Parity, sc.LatencyV4Ms, sc.LatencyV6Ms,
		sc.Classification, sc.CountryID, sc.AsnID)
	batch.Queue(db.InsertScanDetail, u.detail.DomainID, u.detail.Ts, u.detail.Details, u.detail.DurationMs)
	for _, host := range u.resources {
		batch.Queue(db.EnsureResourceHost, host)
		batch.Queue(sqlUpsertDomainResource, host, u.domainID, u.params.Ts)
	}
	if u.pruneLinks {
		batch.Queue(sqlPruneDomainResources, u.domainID, u.params.Ts)
	}

	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

	br := tx.SendBatch(ctx, batch)
	tag, firstErr := br.Exec() // statement 1: the fenced domain UPDATE
	leaseLost = firstErr == nil && tag.RowsAffected() == 0
	for i := 1; i < batch.Len(); i++ { // drain every remaining result
		if _, e := br.Exec(); e != nil && firstErr == nil {
			firstErr = e
		}
	}
	if e := br.Close(); e != nil && firstErr == nil {
		firstErr = e
	}
	if leaseLost {
		c.LeaseLost.Add(1)
		c.log.Warn("lease lost, commit discarded", "domain", u.host)
		return true, nil // deferred Rollback discards EVERYTHING
	}
	if firstErr != nil {
		return false, fmt.Errorf("commit batch: %w", firstErr)
	}
	return false, tx.Commit(ctx)
}

// Dead-signal evidence tokens (03 §4).
const (
	rcodeNXDomain = "NXDOMAIN"
	cdFail        = "cd_fail"
	rcServfail    = "SERVFAIL"
	rcRefused     = "REFUSED"
)

// Unresolvable computes the dead signal U from raw engine/consensus
// evidence (03 §4): (a) apex AAAA quorum NXDOMAIN with no delegated zone
// found by the NS walk-up, or (b) all 3 providers answering explicit
// SERVFAIL/REFUSED with the CD=1 re-query also failing (cd_fail). Timeouts
// never count; a 2-of-2 degraded fan-out can never satisfy branch (b).
func Unresolvable(sr checker.ScanResult) bool {
	_, base, ok := sr.AAAABase()
	if !ok {
		return false
	}

	// Branch (a): NXDOMAIN + no delegated zone for the host.
	if base.Rcode == rcodeNXDomain && !nsZoneFound(sr) {
		return true
	}

	// Branch (b): explicit all-SERVFAIL/REFUSED + cd_fail.
	if base.CDOutcome != cdFail {
		return false
	}
	if base.Quorum == nil || len(base.Quorum.Rcodes) != 3 {
		return false // degraded 2-of-2: dead detection requires all 3
	}
	for _, rc := range base.Quorum.Rcodes {
		if rc != rcServfail && rc != rcRefused {
			return false // a timeout/transport non-answer disqualifies
		}
	}
	return true
}

// nsZoneFound reads the NS walk-up evidence from the raw result (03 §4):
// a `zone` key means a delegated zone above the input was found; NS records
// at the input itself (a non-error status) also count. Only the walk-up's
// explicit "no NS records found" outcome counts as no-zone; other errors
// (resolver trouble) are treated conservatively as zone-found.
func nsZoneFound(sr checker.ScanResult) bool {
	st, ns, ok := sr.NS()
	if !ok {
		return true // conservative
	}
	if ns.Zone != "" {
		return true
	}
	if st != checker.StatusError {
		return true // NS found at the input host itself
	}
	return ns.Error != "no NS records found"
}
