package crawler

import (
	"testing"
	"time"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/observe"
	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

var seqT0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func testCommitCfg() *CommitConfig {
	return &CommitConfig{
		MinConfirmSpacing: 12 * time.Hour,
		DeadStreak:        7,
		Schedule: ScheduleConfig{
			CadenceDefault:      24 * time.Hour,
			RecheckInconsistent: 2 * time.Hour,
			RecheckError:        6 * time.Hour,
			RecheckBackoffMax:   720 * time.Hour,
			SlowLaneEvery:       720 * time.Hour,
		},
	}
}

// machine drives ComputeCommit as a pure state transition: it holds the
// domain's evolving snapshot and applies (Δt, observations) steps.
type machine struct {
	t         *testing.T
	cfg       *CommitConfig
	s         ClaimedDomain
	resources bool // crawler.resources.enabled, as the mapper would fold it
}

func newMachine(t *testing.T) *machine {
	rank := int32(100)
	return &machine{
		t:   t,
		cfg: testCommitCfg(),
		s: ClaimedDomain{
			ID: 1, Host: "seq.example", Kind: domain.KindApex, Rank: &rank,
			ClaimedAt: seqT0, AsnID: 1, CountryID: 1,
			Dims: map[domain.Dimension]DimState{
				domain.DimBase: {}, domain.DimWWW: {}, domain.DimNS: {},
				domain.DimMX: {}, domain.DimConn: {}, domain.DimResources: {},
			},
		},
	}
}

// stableObs returns Observations where every core dim holds `supported`
// except the overridden dimension under test.
func stableObs(dim domain.Dimension, o domain.Observation) observe.Observations {
	obs := observe.Observations{
		Base: domain.ObsSupported, WWW: domain.ObsSupported, NS: domain.ObsSupported,
		MX: domain.ObsSupported, Conn: domain.ObsSupported, Resources: domain.ObsSupported,
		DNSSEC: domain.ObsSupported, PTR: domain.ObsSupported,
		SMTP: domain.ObsSupported, Parity: domain.ObsSupported,
	}
	switch dim {
	case domain.DimBase:
		obs.Base = o
	case domain.DimWWW:
		obs.WWW = o
	case domain.DimNS:
		obs.NS = o
	case domain.DimMX:
		obs.MX = o
	case domain.DimConn:
		obs.Conn = o
	case domain.DimResources:
		obs.Resources = o
	}
	return obs
}

// step applies one scan at T0+dt and folds the computed unit back into the
// snapshot (what the fenced UPDATE would persist).
func (m *machine) step(dt time.Duration, obs observe.Observations, unresolvable bool) *commitUnit {
	m.t.Helper()
	if !m.resources {
		// What MapObservations emits with the crawl off.
		obs.Resources = domain.ObsNotApplicable
		obs.ResourcesExcluded = true
	}
	u, err := ComputeCommit(&CommitInput{
		Snapshot: m.s, Obs: obs, Unresolvable: unresolvable,
		Attribution: &Attribution{AsnID: m.s.AsnID, CountryID: m.s.CountryID},
		T:           seqT0.Add(dt),
	}, m.cfg)
	if err != nil {
		m.t.Fatalf("ComputeCommit: %v", err)
	}
	m.fold(u)
	return u
}

func (m *machine) stepErr(dt time.Duration, obs observe.Observations) error {
	m.t.Helper()
	_, err := ComputeCommit(&CommitInput{
		Snapshot: m.s, Obs: obs, T: seqT0.Add(dt),
	}, m.cfg)
	return err
}

// fold applies the unit's fenced-UPDATE values back onto the snapshot.
func (m *machine) fold(u *commitUnit) {
	p := u.Domain
	m.s.Disabled = p.Disabled
	m.s.DisabledReason = nil
	if p.DisabledReason != nil {
		r := domain.DisabledReason(*p.DisabledReason)
		m.s.DisabledReason = &r
	}
	m.s.DisabledAt = postgres.TimePtr(p.DisabledAt)
	m.s.DeadStreak = p.DeadStreak
	m.s.ErrorStreak = p.ErrorStreak
	m.s.LastCountedAt = postgres.TimePtr(p.LastCountedAt)
	m.s.AsnID = p.AsnID
	m.s.CountryID = p.CountryID
	m.s.Dims = map[domain.Dimension]DimState{
		domain.DimBase:      {Status: fromDB(p.BaseStatus), Pending: fromDB(p.BasePending), PendingCount: p.BasePendingCount, Since: postgres.TimePtr(p.BaseSince)},
		domain.DimWWW:       {Status: fromDB(p.WwwStatus), Pending: fromDB(p.WwwPending), PendingCount: p.WwwPendingCount, Since: postgres.TimePtr(p.WwwSince)},
		domain.DimNS:        {Status: fromDB(p.NsStatus), Pending: fromDB(p.NsPending), PendingCount: p.NsPendingCount, Since: postgres.TimePtr(p.NsSince)},
		domain.DimMX:        {Status: fromDB(p.MxStatus), Pending: fromDB(p.MxPending), PendingCount: p.MxPendingCount, Since: postgres.TimePtr(p.MxSince)},
		domain.DimConn:      {Status: fromDB(p.ConnStatus), Pending: fromDB(p.ConnPending), PendingCount: p.ConnPendingCount, Since: postgres.TimePtr(p.ConnSince)},
		domain.DimResources: {Status: fromDB(p.ResourcesStatus), Pending: fromDB(p.ResourcesPending), PendingCount: p.ResourcesPendingCount, Since: postgres.TimePtr(p.ResourcesSince)},
	}
}

func fromDB(s *db.Ipv6Status) *domain.IPv6Status {
	if s == nil {
		return nil
	}
	v := domain.IPv6Status(*s)
	return &v
}

// assertDim checks one dimension's confirm/pending group.
func (m *machine) assertDim(d domain.Dimension, status, pending *domain.IPv6Status, count int16) {
	m.t.Helper()
	got := m.s.Dims[d]
	if !eqStatus(got.Status, status) {
		m.t.Errorf("%s status = %v, want %v", d, strStatus(got.Status), strStatus(status))
	}
	if !eqStatus(got.Pending, pending) {
		m.t.Errorf("%s pending = %v, want %v", d, strStatus(got.Pending), strStatus(pending))
	}
	if got.PendingCount != count {
		m.t.Errorf("%s pending_count = %d, want %d", d, got.PendingCount, count)
	}
}

func eqStatus(a, b *domain.IPv6Status) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func strStatus(s *domain.IPv6Status) string {
	if s == nil {
		return "NULL"
	}
	return string(*s)
}

func ptrStatus(s domain.IPv6Status) *domain.IPv6Status { return &s }

// TestCommitBootstrap (10-testing §5.1): the first definitive observation
// commits immediately with no changelog row, for N=2 and N=3 dims alike.
func TestCommitBootstrap(t *testing.T) {
	for _, dim := range []domain.Dimension{domain.DimBase, domain.DimWWW, domain.DimNS, domain.DimMX, domain.DimConn} {
		t.Run(string(dim), func(t *testing.T) {
			m := newMachine(t)
			u := m.step(0, stableObs(dim, domain.ObsSupported), false)
			if len(u.Changelog) != 0 {
				t.Errorf("bootstrap wrote %d changelog rows, want 0", len(u.Changelog))
			}
			if u.bootstraps == 0 {
				t.Error("bootstrap counter not incremented")
			}
			m.assertDim(dim, ptrStatus(domain.StatusSupported), nil, 0)
			if m.s.LastCountedAt == nil || !m.s.LastCountedAt.Equal(seqT0) {
				t.Errorf("last_counted_at = %v, want T0", m.s.LastCountedAt)
			}
		})
	}
}

// TestCommitN2Flip (10-testing §5.2): base flips on the 2nd spaced counted
// observation with exactly one changelog row.
func TestCommitN2Flip(t *testing.T) {
	m := newMachine(t)
	m.step(0, stableObs(domain.DimBase, domain.ObsSupported), false)

	u := m.step(24*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false)
	if len(u.Changelog) != 0 {
		t.Fatal("first unsupported must not flip")
	}
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 1)

	u = m.step(48*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false)
	if len(u.Changelog) != 1 {
		t.Fatalf("changelog rows = %d, want 1", len(u.Changelog))
	}
	cl := u.Changelog[0]
	if cl.Field != "base" || cl.OldValue != db.Ipv6StatusSupported || cl.NewValue != db.Ipv6StatusUnsupported {
		t.Errorf("changelog = %+v", cl)
	}
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusUnsupported), nil, 0)
}

// TestCommitCountingGate (10-testing §5.3): close rechecks never advance the
// confirmation; the flip cannot happen faster than (N-1)×12h.
func TestCommitCountingGate(t *testing.T) {
	m := newMachine(t)
	m.step(0, stableObs(domain.DimBase, domain.ObsSupported), false)

	m.step(24*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false) // counts
	m.step(26*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false) // close: no
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 1)
	m.step(28*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false) // close: no
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 1)

	u := m.step(37*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false) // ≥12h: flip
	if len(u.Changelog) != 1 {
		t.Fatalf("changelog rows = %d, want 1 (spaced flip)", len(u.Changelog))
	}
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusUnsupported), nil, 0)
}

// TestCommitCountingGateBoundary pins the gate's edge (03 §7): an
// observation exactly min_confirm_spacing after the last counted one counts
// — the comparison is >=, so the 12h cadence itself is never "too close".
func TestCommitCountingGateBoundary(t *testing.T) {
	m := newMachine(t)
	m.step(0, stableObs(domain.DimBase, domain.ObsSupported), false)
	m.step(24*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false) // counts
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 1)

	u := m.step(36*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false) // exactly +12h: counts
	if len(u.Changelog) != 1 {
		t.Fatalf("changelog rows = %d, want 1 (an observation at exactly +12h counts)", len(u.Changelog))
	}
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusUnsupported), nil, 0)
}

// TestCommitN3Flip (10-testing §5.4): conn (and resources) need three spaced
// counted observations.
func TestCommitN3Flip(t *testing.T) {
	m := newMachine(t)
	m.step(0, stableObs(domain.DimConn, domain.ObsSupported), false)

	m.step(24*time.Hour, stableObs(domain.DimConn, domain.ObsUnsupported), false)
	m.assertDim(domain.DimConn, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 1)
	m.step(48*time.Hour, stableObs(domain.DimConn, domain.ObsUnsupported), false)
	m.assertDim(domain.DimConn, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 2)
	u := m.step(72*time.Hour, stableObs(domain.DimConn, domain.ObsUnsupported), false)
	if len(u.Changelog) != 1 || u.Changelog[0].Field != "conn" {
		t.Fatalf("conn N=3 flip: %+v", u.Changelog)
	}
	m.assertDim(domain.DimConn, ptrStatus(domain.StatusUnsupported), nil, 0)
}

// TestCommitShadowSuppressed (03 §11): a confirmed conn → not_applicable
// flip is a shadow of the base/www AAAA-loss rows — the status flips and
// the Transition is reported, but no changelog row is written. Same rule
// for resources → not_applicable.
func TestCommitShadowSuppressed(t *testing.T) {
	m := newMachine(t)
	m.step(0, stableObs(domain.DimConn, domain.ObsSupported), false)

	m.step(24*time.Hour, stableObs(domain.DimConn, domain.ObsNotApplicable), false)
	m.step(48*time.Hour, stableObs(domain.DimConn, domain.ObsNotApplicable), false)
	u := m.step(72*time.Hour, stableObs(domain.DimConn, domain.ObsNotApplicable), false)
	if len(u.Changelog) != 0 {
		t.Fatalf("shadow conn → not_applicable wrote a changelog row: %+v", u.Changelog)
	}
	if len(u.transitions) != 1 || u.transitions[0].Dim != domain.DimConn ||
		u.transitions[0].New != domain.StatusNotApplicable {
		t.Fatalf("flip must still report a Transition: %+v", u.transitions)
	}
	m.assertDim(domain.DimConn, ptrStatus(domain.StatusNotApplicable), nil, 0)

	// The reverse direction is real news and keeps its row.
	m.step(96*time.Hour, stableObs(domain.DimConn, domain.ObsSupported), false)
	m.step(120*time.Hour, stableObs(domain.DimConn, domain.ObsSupported), false)
	u = m.step(144*time.Hour, stableObs(domain.DimConn, domain.ObsSupported), false)
	if len(u.Changelog) != 1 || u.Changelog[0].Field != "conn" ||
		u.Changelog[0].NewValue != db.Ipv6StatusSupported {
		t.Fatalf("conn not_applicable → supported must keep its row: %+v", u.Changelog)
	}
}

// TestCommitNonDefinitive (10-testing §5.5): error/inconsistent touch
// nothing; the pending candidate survives and the flip lands afterwards.
func TestCommitNonDefinitive(t *testing.T) {
	m := newMachine(t)
	m.step(0, stableObs(domain.DimBase, domain.ObsSupported), false)
	m.step(24*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false)

	u := m.step(48*time.Hour, stableObs(domain.DimBase, domain.ObsError), false)
	if len(u.Changelog) != 0 {
		t.Fatal("error scan wrote changelog")
	}
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 1)
	if obs := u.Domain.BaseObserved; obs == nil || *obs != db.ObservationError {
		t.Errorf("base_observed = %v, want error (always recorded)", obs)
	}

	m.step(72*time.Hour, stableObs(domain.DimBase, domain.ObsInconsistent), false)
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 1)

	u = m.step(96*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false)
	if len(u.Changelog) != 1 {
		t.Fatalf("flip after interleaved non-definitive: %d rows", len(u.Changelog))
	}
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusUnsupported), nil, 0)
}

// TestCommitPendingReset (10-testing §5.6): a value differing from both
// status and pending replaces the candidate with count 1.
func TestCommitPendingReset(t *testing.T) {
	m := newMachine(t)
	m.step(0, stableObs(domain.DimBase, domain.ObsSupported), false)
	m.step(24*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false)
	m.step(48*time.Hour, stableObs(domain.DimBase, domain.ObsNoRecord), false)
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusNoRecord), 1)

	u := m.step(72*time.Hour, stableObs(domain.DimBase, domain.ObsNoRecord), false)
	if len(u.Changelog) != 1 || u.Changelog[0].NewValue != db.Ipv6StatusNoRecord {
		t.Fatalf("no_record flip: %+v", u.Changelog)
	}
}

// TestCommitStepR (10-testing §5.7): a definitive base observation on a dead
// row fires step R — full reset, immediate bootstrap, zero changelog rows.
func TestCommitStepR(t *testing.T) {
	m := newMachine(t)
	m.step(0, stableObs(domain.DimBase, domain.ObsSupported), false)

	// Kill it: 7 unresolvable scans, daily.
	for i := 1; i <= 7; i++ {
		m.step(time.Duration(i)*24*time.Hour, stableObs(domain.DimBase, domain.ObsNoRecord), true)
	}
	if !m.s.Disabled || m.s.DisabledReason == nil || *m.s.DisabledReason != domain.DisabledDead {
		t.Fatalf("not dead after 7 unresolvable scans: %+v", m.s)
	}

	// An error base on a dead row leaves it disabled.
	m.step(8*24*time.Hour, stableObs(domain.DimBase, domain.ObsError), false)
	if !m.s.Disabled {
		t.Fatal("error base must not recover a dead row")
	}

	// Recovery: definitive base observation.
	u := m.step(38*24*time.Hour, stableObs(domain.DimBase, domain.ObsSupported), false)
	if len(u.Changelog) != 0 {
		t.Fatalf("step R wrote %d changelog rows, want 0 (clean changelog)", len(u.Changelog))
	}
	if m.s.Disabled || m.s.DisabledReason != nil {
		t.Errorf("recovered row still disabled: %+v", m.s)
	}
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusSupported), nil, 0)
	if u.Domain.Classification == "" {
		t.Error("classification missing after step R")
	}
}

// TestCommitDeadStreak (10-testing §5.8): seven and never fewer; a
// resolvable scan resets the streak.
func TestCommitDeadStreak(t *testing.T) {
	m := newMachine(t)
	for i := 1; i <= 6; i++ {
		m.step(time.Duration(i)*24*time.Hour, stableObs(domain.DimBase, domain.ObsNoRecord), true)
		if m.s.Disabled {
			t.Fatalf("disabled after %d unresolvable scans, want 7", i)
		}
		if m.s.DeadStreak != int16(i) {
			t.Fatalf("dead_streak = %d after scan %d", m.s.DeadStreak, i)
		}
	}
	// A resolvable scan anywhere resets.
	m.step(7*24*time.Hour, stableObs(domain.DimBase, domain.ObsSupported), false)
	if m.s.DeadStreak != 0 {
		t.Fatalf("dead_streak = %d after resolvable scan, want 0", m.s.DeadStreak)
	}
	for i := 8; i <= 14; i++ {
		m.step(time.Duration(i)*24*time.Hour, stableObs(domain.DimBase, domain.ObsNoRecord), true)
	}
	if !m.s.Disabled || m.s.DeadStreak != 0 || m.s.ErrorStreak != 0 {
		t.Fatalf("dead trigger state: %+v", m.s)
	}
}

// TestCommitResourcesExcluded (03 §17.9): while the flag is false the
// resources columns stay NULL and scan.resources is not_applicable.
func TestCommitResourcesExcluded(t *testing.T) {
	m := newMachine(t)
	obs := stableObs(domain.DimBase, domain.ObsSupported)
	obs.Resources = domain.ObsNotApplicable
	obs.ResourcesExcluded = true
	u := m.step(0, obs, false)

	if u.Domain.ResourcesStatus != nil || u.Domain.ResourcesObserved != nil ||
		u.Domain.ResourcesPending != nil || u.Domain.ResourcesPendingCount != 0 {
		t.Errorf("resources columns written while excluded: %+v", u.Domain)
	}
	if u.Scan.Resources != db.ObservationNotApplicable {
		t.Errorf("scan.resources = %s, want not_applicable", u.Scan.Resources)
	}
	if u.Domain.Saint {
		t.Error("saint while resources disabled")
	}
}

// TestCommitPartialDefect (03 §1): a partial core observation aborts the
// commit with an error.
func TestCommitPartialDefect(t *testing.T) {
	m := newMachine(t)
	if err := m.stepErr(0, stableObs(domain.DimBase, domain.ObsPartial)); err == nil {
		t.Fatal("partial core observation must abort the commit")
	}
}

// TestCommitAttributionDeferred (03 §5 step 7): non-definitive base keeps
// the snapshot attribution.
func TestCommitAttributionDeferred(t *testing.T) {
	m := newMachine(t)
	m.step(0, stableObs(domain.DimBase, domain.ObsSupported), false)

	u, err := ComputeCommit(&CommitInput{
		Snapshot: m.s, Obs: stableObs(domain.DimBase, domain.ObsError),
		Attribution: &Attribution{AsnID: 99, CountryID: 99}, T: seqT0.Add(24 * time.Hour),
	}, m.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if u.Domain.AsnID != 1 || u.Domain.CountryID != 1 {
		t.Errorf("attribution touched on non-definitive base: %d/%d", u.Domain.AsnID, u.Domain.CountryID)
	}

	u, err = ComputeCommit(&CommitInput{
		Snapshot: m.s, Obs: stableObs(domain.DimBase, domain.ObsSupported),
		Attribution: &Attribution{AsnID: 99, CountryID: 98}, T: seqT0.Add(24 * time.Hour),
	}, m.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if u.Domain.AsnID != 99 || u.Domain.CountryID != 98 {
		t.Errorf("attribution not refreshed on definitive base: %d/%d", u.Domain.AsnID, u.Domain.CountryID)
	}
}

// TestUnresolvable covers the 03 §4 dead-signal branches.
func TestUnresolvable(t *testing.T) {
	mk := func(base *checker.AAAADetail, nsResult checker.Result) checker.ScanResult {
		return checker.ScanResult{Results: map[string]checker.Result{
			"dns_aaaa_base": {Status: checker.StatusNotApplicable, Detail: base},
			"dns_ns_ipv6":   nsResult,
		}}
	}
	noZone := checker.Result{Status: checker.StatusError,
		Detail: &checker.NSDetail{CommonDetail: checker.CommonDetail{Error: "no NS records found"}}}
	zoneAbove := checker.Result{Status: checker.StatusError,
		Detail: &checker.NSDetail{CommonDetail: checker.CommonDetail{Error: "x"}, Zone: "parent.example"}}
	nsOK := checker.Result{Status: checker.StatusSupported, Detail: &checker.NSDetail{}}

	if !Unresolvable(mk(&checker.AAAADetail{Rcode: "NXDOMAIN"}, noZone)) {
		t.Error("branch (a): NXDOMAIN + no zone must be unresolvable")
	}
	if Unresolvable(mk(&checker.AAAADetail{Rcode: "NXDOMAIN"}, zoneAbove)) {
		t.Error("zone found above the host must not be unresolvable")
	}
	if Unresolvable(mk(&checker.AAAADetail{Rcode: "NXDOMAIN"}, nsOK)) {
		t.Error("NS at host must not be unresolvable")
	}
	if Unresolvable(mk(&checker.AAAADetail{Rcode: "NOERROR"}, noZone)) {
		t.Error("NOERROR-empty is a live zone, never unresolvable")
	}

	qi := func(rc1, rc2, rc3 string) *checker.QuorumInfo {
		return &checker.QuorumInfo{Rcodes: map[string]string{"cloudflare": rc1, "google": rc2, "quad9": rc3}}
	}
	// Branch (b): all-SERVFAIL + cd_fail.
	if !Unresolvable(mk(&checker.AAAADetail{CDOutcome: "cd_fail",
		Quorum: qi("SERVFAIL", "SERVFAIL", "REFUSED")}, nsOK)) {
		t.Error("branch (b): all-SERVFAIL/REFUSED + cd_fail must be unresolvable")
	}
	// cd_present rescues: never unresolvable.
	if Unresolvable(mk(&checker.AAAADetail{Rcode: "NOERROR", CDOutcome: "cd_present",
		Quorum: qi("SERVFAIL", "SERVFAIL", "SERVFAIL")}, nsOK)) {
		t.Error("cd_present must not be unresolvable")
	}
	// Timeouts never count.
	if Unresolvable(mk(&checker.AAAADetail{CDOutcome: "cd_fail",
		Quorum: qi("SERVFAIL", "", "SERVFAIL")}, nsOK)) {
		t.Error("a timeout non-answer disqualifies branch (b)")
	}
	// Degraded 2-of-2 fan-out can never satisfy branch (b).
	if Unresolvable(mk(&checker.AAAADetail{CDOutcome: "cd_fail",
		Quorum: &checker.QuorumInfo{Rcodes: map[string]string{"cloudflare": "SERVFAIL", "google": "SERVFAIL"}}}, nsOK)) {
		t.Error("2-of-2 degraded mode must not mark dead")
	}
}

// TestCommitErrorStreakBreakerOpen (04 §5.1 Decision): the streak still
// increments while the breaker is open; only scheduling is suspended.
func TestCommitErrorStreakBreakerOpen(t *testing.T) {
	m := newMachine(t)
	m.step(0, stableObs(domain.DimBase, domain.ObsSupported), false)

	u, err := ComputeCommit(&CommitInput{
		Snapshot: m.s, Obs: stableObs(domain.DimBase, domain.ObsError),
		BreakerOpen: true, T: seqT0.Add(24 * time.Hour),
	}, m.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if u.Domain.ErrorStreak != 1 {
		t.Errorf("error_streak = %d under open breaker, want 1", u.Domain.ErrorStreak)
	}
	if got := u.Domain.NextCheckAt.Time.Sub(seqT0.Add(24 * time.Hour)); got != 24*time.Hour {
		t.Errorf("breaker-open scheduling = %v, want cadence 24h", got)
	}
}
