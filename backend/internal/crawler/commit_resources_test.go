package crawler

import (
	"testing"
	"time"

	"github.com/lasseh/whynoipv6/internal/domain"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// newResourcesMachine is newMachine with the resources dimension in the
// step-2 loop (crawler.resources.enabled = true).
func newResourcesMachine(t *testing.T) *machine {
	t.Helper()
	m := newMachine(t)
	m.cfg = testCommitCfg(true)
	return m
}

// TestCommitResourcesDimension (03 §5, ADR 0002/0003): with the resources
// crawl on, the dimension bootstraps on the first definitive scan, is left
// untouched by a non-definitive scan, flips only after N=3 counted
// observations with exactly one changelog row, and drives the saint flag.
func TestCommitResourcesDimension(t *testing.T) {
	m := newResourcesMachine(t)
	u := m.step(0, stableObs(domain.DimResources, domain.ObsSupported), false)
	m.assertDim(domain.DimResources, ptrStatus(domain.StatusSupported), nil, 0)
	if len(u.Changelog) != 0 {
		t.Fatalf("bootstrap wrote changelog rows: %+v", u.Changelog)
	}
	if u.Domain.Classification != db.ClassificationHero || !u.Domain.Saint {
		t.Fatalf("all-supported bootstrap: class=%s saint=%t, want hero saint", u.Domain.Classification, u.Domain.Saint)
	}

	// A non-definitive resources observation touches nothing.
	u = m.step(24*time.Hour, stableObs(domain.DimResources, domain.ObsError), false)
	m.assertDim(domain.DimResources, ptrStatus(domain.StatusSupported), nil, 0)
	if !u.Domain.Saint {
		t.Error("an error scan must not drop saint")
	}

	// N=3: two counted unsupported scans stay pending, the third flips.
	m.step(48*time.Hour, stableObs(domain.DimResources, domain.ObsUnsupported), false)
	m.assertDim(domain.DimResources, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 1)
	m.step(72*time.Hour, stableObs(domain.DimResources, domain.ObsUnsupported), false)
	m.assertDim(domain.DimResources, ptrStatus(domain.StatusSupported), ptrStatus(domain.StatusUnsupported), 2)
	u = m.step(96*time.Hour, stableObs(domain.DimResources, domain.ObsUnsupported), false)
	m.assertDim(domain.DimResources, ptrStatus(domain.StatusUnsupported), nil, 0)
	if len(u.Changelog) != 1 || u.Changelog[0].Field != "resources" ||
		u.Changelog[0].OldValue != db.Ipv6StatusSupported || u.Changelog[0].NewValue != db.Ipv6StatusUnsupported {
		t.Fatalf("resources flip changelog = %+v, want one supported → unsupported row", u.Changelog)
	}
	if !u.Domain.ResourcesSince.Valid || !u.Domain.ResourcesSince.Time.Equal(seqT0.Add(96*time.Hour)) {
		t.Errorf("resources_since = %+v, want the flip's T", u.Domain.ResourcesSince)
	}
	if u.Domain.Saint || u.Domain.Classification != db.ClassificationHero {
		t.Errorf("after the flip: class=%s saint=%t, want hero without saint", u.Domain.Classification, u.Domain.Saint)
	}
}

// TestCommitResourcesShadowPinned pins the shipped 03 §11 rule for
// resources → not_applicable while conn stays supported: the flip commits
// and is reported as a Transition but writes no changelog row. Issue 02 of
// the 2026-09 backend review questions the rule's premise; change this
// test together with it.
func TestCommitResourcesShadowPinned(t *testing.T) {
	m := newResourcesMachine(t)
	m.step(0, stableObs(domain.DimResources, domain.ObsUnsupported), false)
	m.step(24*time.Hour, stableObs(domain.DimResources, domain.ObsNotApplicable), false)
	m.step(48*time.Hour, stableObs(domain.DimResources, domain.ObsNotApplicable), false)
	u := m.step(72*time.Hour, stableObs(domain.DimResources, domain.ObsNotApplicable), false)
	if len(u.Changelog) != 0 {
		t.Fatalf("resources → not_applicable wrote a changelog row: %+v (03 §11 shadow rule)", u.Changelog)
	}
	if len(u.transitions) != 1 || u.transitions[0].Dim != domain.DimResources ||
		u.transitions[0].New != domain.StatusNotApplicable {
		t.Fatalf("flip must still report a Transition: %+v", u.transitions)
	}
	m.assertDim(domain.DimResources, ptrStatus(domain.StatusNotApplicable), nil, 0)
	if !u.Domain.Saint {
		t.Error("resources not_applicable with everything else supported is saint by the ladder (ADR 0003)")
	}
}
