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
	m.resources = true
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

// TestCommitResourcesNotApplicableIsNews replaces the old
// TestCommitResourcesShadowPinned (review issue 02, 03 §11 erratum). The
// shipped rule suppressed every resources → not_applicable flip on the
// premise that it only happens when conn leaves supported. It does not:
// 02 §6's roll-up returns not_applicable whenever the effective link set is
// empty — a pruned or swept-clean dependency — with conn still supported.
// That flip clears resources_v4only, sets saint and turns ipv6_only
// supported, so it is public news and writes its row.
func TestCommitResourcesNotApplicableIsNews(t *testing.T) {
	m := newResourcesMachine(t)
	m.step(0, stableObs(domain.DimResources, domain.ObsUnsupported), false)
	m.step(24*time.Hour, stableObs(domain.DimResources, domain.ObsNotApplicable), false)
	m.step(48*time.Hour, stableObs(domain.DimResources, domain.ObsNotApplicable), false)
	u := m.step(72*time.Hour, stableObs(domain.DimResources, domain.ObsNotApplicable), false)

	if len(u.Changelog) != 1 || u.Changelog[0].Field != "resources" ||
		u.Changelog[0].OldValue != db.Ipv6StatusUnsupported ||
		u.Changelog[0].NewValue != db.Ipv6StatusNotApplicable {
		t.Fatalf("changelog = %+v, want one resources unsupported → not_applicable row: "+
			"conn never left supported, so this is not a shadow", u.Changelog)
	}
	if len(u.transitions) != 1 || u.transitions[0].Shadow {
		t.Errorf("transitions = %+v, want one non-shadow flip", u.transitions)
	}
	m.assertDim(domain.DimResources, ptrStatus(domain.StatusNotApplicable), nil, 0)
	if !u.Domain.Saint {
		t.Error("resources not_applicable with everything else supported is saint by the ladder (ADR 0003)")
	}
}

// TestCommitResourcesShadowFollowsConn is the other half of the erratum:
// when conn actually leaves supported in the same confirmation window, the
// resources flip that trails it IS a deterministic shadow and stays out of
// the changelog. conn's own row carries the news.
func TestCommitResourcesShadowFollowsConn(t *testing.T) {
	m := newResourcesMachine(t)
	m.step(0, stableObs(domain.DimResources, domain.ObsUnsupported), false)

	// base and www lose their AAAA, so conn and resources both go
	// not_applicable together. conn confirms at N=2, resources at N=3.
	obs := stableObs(domain.DimBase, domain.ObsNoRecord)
	obs.WWW = domain.ObsNoRecord
	obs.Conn = domain.ObsNotApplicable
	obs.Resources = domain.ObsNotApplicable
	var u *commitUnit
	for i := 1; i <= 3; i++ {
		u = m.step(time.Duration(i)*24*time.Hour, obs, false)
	}

	m.assertDim(domain.DimConn, ptrStatus(domain.StatusNotApplicable), nil, 0)
	m.assertDim(domain.DimResources, ptrStatus(domain.StatusNotApplicable), nil, 0)
	for _, row := range u.Changelog {
		if row.Field == "resources" {
			t.Errorf("resources wrote %+v: conn left supported, so this flip is a shadow", row)
		}
	}
	var seen bool
	for _, tr := range u.transitions {
		if tr.Dim == domain.DimResources {
			seen = true
			if !tr.Shadow {
				t.Errorf("resources transition %+v must be marked Shadow", tr)
			}
		}
	}
	if !seen {
		t.Fatalf("no resources transition in %+v", u.transitions)
	}
}

// TestCommitResourcesKeyedOnTheObservation (02 §7.2, review issue 14): one
// source of truth. The same commit config yields a resources dimension or
// not, decided only by the mapper's ResourcesExcluded signal — the commit
// carries no second copy of crawler.resources.enabled to disagree with it.
func TestCommitResourcesKeyedOnTheObservation(t *testing.T) {
	for _, tt := range []struct {
		name        string
		excluded    bool
		wantColumns bool
	}{
		{"mapper observed the dimension", false, true},
		{"mapper excluded the dimension", true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newMachine(t)
			obs := stableObs(domain.DimResources, domain.ObsSupported)
			if tt.excluded {
				obs.Resources = domain.ObsNotApplicable
				obs.ResourcesExcluded = true
			}
			u, err := ComputeCommit(&CommitInput{
				Snapshot: m.s, Obs: obs, Discovered: []string{"cdn.example"}, DiscoveryOK: true,
				Attribution: &Attribution{AsnID: 1, CountryID: 1}, T: seqT0,
			}, m.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got := u.Domain.ResourcesStatus != nil; got != tt.wantColumns {
				t.Errorf("resources columns written = %t, want %t", got, tt.wantColumns)
			}
			if got := u.PruneLinks; got != tt.wantColumns {
				t.Errorf("discovered links consumed = %t, want %t", got, tt.wantColumns)
			}
		})
	}
}
