package crawler

import (
	"testing"
	"time"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/observe"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

var seqT0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func testCommitCfg(resources bool) *CommitConfig {
	return &CommitConfig{
		MinConfirmSpacing: 12 * time.Hour,
		DeadStreak:        7,
		ResourcesEnabled:  resources,
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
	t   *testing.T
	cfg *CommitConfig
	s   ClaimedDomain
}

func newMachine(t *testing.T) *machine {
	rank := int32(100)
	return &machine{
		t:   t,
		cfg: testCommitCfg(false),
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
	p := u.params
	m.s.Disabled = p.Disabled
	m.s.DisabledReason = nil
	if p.DisabledReason != nil {
		r := domain.DisabledReason(*p.DisabledReason)
		m.s.DisabledReason = &r
	}
	m.s.DisabledAt = tsPtr(p.DisabledAt)
	m.s.DeadStreak = p.DeadStreak
	m.s.ErrorStreak = p.ErrorStreak
	m.s.LastCountedAt = tsPtr(p.LastCountedAt)
	m.s.AsnID = p.AsnID
	m.s.CountryID = p.CountryID
	m.s.Dims = map[domain.Dimension]DimState{
		domain.DimBase:      {Status: fromDB(p.BaseStatus), Pending: fromDB(p.BasePending), PendingCount: p.BasePendingCount, Since: tsPtr(p.BaseSince)},
		domain.DimWWW:       {Status: fromDB(p.WwwStatus), Pending: fromDB(p.WwwPending), PendingCount: p.WwwPendingCount, Since: tsPtr(p.WwwSince)},
		domain.DimNS:        {Status: fromDB(p.NsStatus), Pending: fromDB(p.NsPending), PendingCount: p.NsPendingCount, Since: tsPtr(p.NsSince)},
		domain.DimMX:        {Status: fromDB(p.MxStatus), Pending: fromDB(p.MxPending), PendingCount: p.MxPendingCount, Since: tsPtr(p.MxSince)},
		domain.DimConn:      {Status: fromDB(p.ConnStatus), Pending: fromDB(p.ConnPending), PendingCount: p.ConnPendingCount, Since: tsPtr(p.ConnSince)},
		domain.DimResources: {Status: fromDB(p.ResourcesStatus), Pending: fromDB(p.ResourcesPending), PendingCount: p.ResourcesPendingCount, Since: tsPtr(p.ResourcesSince)},
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
			if len(u.changelog) != 0 {
				t.Errorf("bootstrap wrote %d changelog rows, want 0", len(u.changelog))
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
	if len(u.changelog) != 0 {
		t.Fatal("first unsupported must not flip")
	}
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 1)

	u = m.step(48*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false)
	if len(u.changelog) != 1 {
		t.Fatalf("changelog rows = %d, want 1", len(u.changelog))
	}
	cl := u.changelog[0]
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
	if len(u.changelog) != 1 {
		t.Fatalf("changelog rows = %d, want 1 (spaced flip)", len(u.changelog))
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
	if len(u.changelog) != 1 || u.changelog[0].Field != "conn" {
		t.Fatalf("conn N=3 flip: %+v", u.changelog)
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
	if len(u.changelog) != 0 {
		t.Fatalf("shadow conn → not_applicable wrote a changelog row: %+v", u.changelog)
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
	if len(u.changelog) != 1 || u.changelog[0].Field != "conn" ||
		u.changelog[0].NewValue != db.Ipv6StatusSupported {
		t.Fatalf("conn not_applicable → supported must keep its row: %+v", u.changelog)
	}
}

// TestCommitNonDefinitive (10-testing §5.5): error/inconsistent touch
// nothing; the pending candidate survives and the flip lands afterwards.
func TestCommitNonDefinitive(t *testing.T) {
	m := newMachine(t)
	m.step(0, stableObs(domain.DimBase, domain.ObsSupported), false)
	m.step(24*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false)

	u := m.step(48*time.Hour, stableObs(domain.DimBase, domain.ObsError), false)
	if len(u.changelog) != 0 {
		t.Fatal("error scan wrote changelog")
	}
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 1)
	if obs := u.params.BaseObserved; obs == nil || *obs != db.ObservationError {
		t.Errorf("base_observed = %v, want error (always recorded)", obs)
	}

	m.step(72*time.Hour, stableObs(domain.DimBase, domain.ObsInconsistent), false)
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 1)

	u = m.step(96*time.Hour, stableObs(domain.DimBase, domain.ObsUnsupported), false)
	if len(u.changelog) != 1 {
		t.Fatalf("flip after interleaved non-definitive: %d rows", len(u.changelog))
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
	if len(u.changelog) != 1 || u.changelog[0].NewValue != db.Ipv6StatusNoRecord {
		t.Fatalf("no_record flip: %+v", u.changelog)
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
	if len(u.changelog) != 0 {
		t.Fatalf("step R wrote %d changelog rows, want 0 (clean changelog)", len(u.changelog))
	}
	if m.s.Disabled || m.s.DisabledReason != nil {
		t.Errorf("recovered row still disabled: %+v", m.s)
	}
	m.assertDim(domain.DimBase, ptrStatus(domain.StatusSupported), nil, 0)
	if u.params.Classification == "" {
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

	if u.params.ResourcesStatus != nil || u.params.ResourcesObserved != nil ||
		u.params.ResourcesPending != nil || u.params.ResourcesPendingCount != 0 {
		t.Errorf("resources columns written while excluded: %+v", u.params)
	}
	if u.scan.Resources != db.ObservationNotApplicable {
		t.Errorf("scan.resources = %s, want not_applicable", u.scan.Resources)
	}
	if u.params.Saint {
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
	if u.params.AsnID != 1 || u.params.CountryID != 1 {
		t.Errorf("attribution touched on non-definitive base: %d/%d", u.params.AsnID, u.params.CountryID)
	}

	u, err = ComputeCommit(&CommitInput{
		Snapshot: m.s, Obs: stableObs(domain.DimBase, domain.ObsSupported),
		Attribution: &Attribution{AsnID: 99, CountryID: 98}, T: seqT0.Add(24 * time.Hour),
	}, m.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if u.params.AsnID != 99 || u.params.CountryID != 98 {
		t.Errorf("attribution not refreshed on definitive base: %d/%d", u.params.AsnID, u.params.CountryID)
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

// TestSchedule (04 §17.3): the two backoff progressions, lane choice,
// breaker-open behavior, slow-lane override, band matching.
func TestSchedule(t *testing.T) {
	cfg := testCommitCfg(false).Schedule

	errWant := []time.Duration{6, 12, 24, 48, 96, 192, 384, 720, 720, 720}
	incWant := []time.Duration{2, 4, 8, 16, 32, 64, 128, 256, 512, 720}
	for i := range 10 {
		streak := int16(i + 1)
		if got := backoff(cfg.RecheckError, cfg.RecheckBackoffMax, streak); got != errWant[i]*time.Hour {
			t.Errorf("error lane streak %d = %v, want %vh", streak, got, errWant[i])
		}
		if got := backoff(cfg.RecheckInconsistent, cfg.RecheckBackoffMax, streak); got != incWant[i]*time.Hour {
			t.Errorf("inconsistent lane streak %d = %v, want %vh", streak, got, incWant[i])
		}
	}

	// Inconsistent beats error in lane choice.
	next := schedule(cfg, false, domain.ObsError, domain.ObsInconsistent, 1, nil, false, seqT0)
	if next.Sub(seqT0) != 2*time.Hour {
		t.Errorf("mixed lanes = %v, want 2h (inconsistent wins)", next.Sub(seqT0))
	}

	// Breaker open: cadence lane despite non-definitive.
	next = schedule(cfg, false, domain.ObsError, domain.ObsSupported, 3, nil, true, seqT0)
	if next.Sub(seqT0) != 24*time.Hour {
		t.Errorf("breaker-open = %v, want cadence 24h", next.Sub(seqT0))
	}

	// Disabled slow-lane override beats everything.
	next = schedule(cfg, true, domain.ObsError, domain.ObsError, 5, nil, false, seqT0)
	if next.Sub(seqT0) != 720*time.Hour {
		t.Errorf("disabled = %v, want 720h slow lane", next.Sub(seqT0))
	}

	// Non-consensus dims never pull in: definitive base+www → cadence.
	next = schedule(cfg, false, domain.ObsSupported, domain.ObsSupported, 0, nil, false, seqT0)
	if next.Sub(seqT0) != 24*time.Hour {
		t.Errorf("definitive = %v, want cadence 24h", next.Sub(seqT0))
	}

	// Cadence bands: first match wins, NULL rank never matches.
	bands := []Band{{MaxRank: 1000, Every: 12 * time.Hour}, {MinRank: 1001, Every: 72 * time.Hour}}
	r500, r5000 := int32(500), int32(5000)
	if got := cadence(&r500, cfg.CadenceDefault, bands); got != 12*time.Hour {
		t.Errorf("band rank 500 = %v", got)
	}
	if got := cadence(&r5000, cfg.CadenceDefault, bands); got != 72*time.Hour {
		t.Errorf("band rank 5000 = %v", got)
	}
	if got := cadence(nil, cfg.CadenceDefault, bands); got != 24*time.Hour {
		t.Errorf("NULL rank = %v, want default", got)
	}
	if err := ValidateBands([]Band{{Every: time.Hour}}); err == nil {
		t.Error("band without bounds must fail validation")
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
	if u.params.ErrorStreak != 1 {
		t.Errorf("error_streak = %d under open breaker, want 1", u.params.ErrorStreak)
	}
	if got := u.params.NextCheckAt.Time.Sub(seqT0.Add(24 * time.Hour)); got != 24*time.Hour {
		t.Errorf("breaker-open scheduling = %v, want cadence 24h", got)
	}
}
