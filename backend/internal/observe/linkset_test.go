package observe

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// fakeResources is the in-memory adapter at the ResourceReader seam. It
// matches hosts by exact equality, exactly as resource.sql's
// `host = ANY(@hosts::text[])` does — that exactness is the whole point.
type fakeResources struct {
	registry map[string]db.Ipv6Status // canonical host -> aaaa_status
	required []db.DomainRequiredLinksRow
	asked    []string
	err      error
}

func (f *fakeResources) DomainRequiredLinks(context.Context, int64) ([]db.DomainRequiredLinksRow, error) {
	return f.required, f.err
}

func (f *fakeResources) ResourceHostStatuses(_ context.Context, hosts []string) ([]db.ResourceHostStatusesRow, error) {
	f.asked = append(f.asked, hosts...)
	if f.err != nil {
		return nil, f.err
	}
	var out []db.ResourceHostStatusesRow
	for _, h := range hosts {
		if st, ok := f.registry[h]; ok {
			s := st
			out = append(out, db.ResourceHostStatusesRow{Host: h, AaaaStatus: &s})
		}
	}
	return out, nil
}

// discoveryScan builds a scan whose resource_discovery reports these hosts.
func discoveryScan(hosts ...string) checker.ScanResult {
	return scan(map[string]checker.Result{
		"resource_discovery": res(checker.StatusSupported,
			&checker.ResourceDiscoveryDetail{Hosts: hosts}),
	})
}

// The defect: the commit path canonicalized discovered hosts before writing
// the registry, the live path passed raw url.Hostname() output to a lookup
// that matches on exact equality. Unicode-vs-punycode, a trailing dot or an
// uppercase label therefore missed on the live path only, silently deferring
// the resources dimension. Both paths now canonicalize inside the
// constructor, so they resolve the same host identically.
func TestLiveLinksCanonicalizesBeforeTheRegistryLookup(t *testing.T) {
	// What the registry holds is what the commit path wrote: canonical.
	canonical, err := domain.Canonicalize("Example.COM.")
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	f := &fakeResources{registry: map[string]db.Ipv6Status{canonical: db.Ipv6Status(domain.StatusSupported)}}

	// What discovery hands over is a raw url.Hostname() string.
	links := LiveLinks(context.Background(), f, discoveryScan("Example.COM."), true)

	if len(links) != 1 {
		t.Fatalf("links = %+v, want one", links)
	}
	if links[0].Host != canonical {
		t.Errorf("LinkedResource.Host = %q, want the canonical %q", links[0].Host, canonical)
	}
	if links[0].AAAAStatus == nil {
		t.Fatal("registry hit was missed: a raw host string reached the lookup")
	}
	if got := strings.Join(f.asked, ","); got != canonical {
		t.Errorf("queried %q, want the canonical %q", got, canonical)
	}
}

// Both constructors produce the same host form for the same input, which is
// the invariant the two paths previously disagreed on.
func TestBothLinkSetPathsAgreeOnHostForm(t *testing.T) {
	canonical, err := domain.Canonicalize("CDN.Example.NET.")
	if err != nil {
		t.Fatal(err)
	}
	sup := db.Ipv6Status(domain.StatusSupported)

	commit := PersistedLinks(context.Background(), &fakeResources{
		required: []db.DomainRequiredLinksRow{{Host: canonical, AaaaStatus: &sup}},
	}, 1, true)
	live := LiveLinks(context.Background(), &fakeResources{
		registry: map[string]db.Ipv6Status{canonical: sup},
	}, discoveryScan("CDN.Example.NET."), true)

	if len(commit) != 1 || len(live) != 1 {
		t.Fatalf("commit = %+v, live = %+v", commit, live)
	}
	if commit[0].Host != live[0].Host {
		t.Errorf("host form differs: commit %q, live %q", commit[0].Host, live[0].Host)
	}
	if commit[0].AAAAStatus == nil || live[0].AAAAStatus == nil {
		t.Error("one path missed the registry the other hit")
	}
}

// A host that cannot be canonicalized is dropped rather than queried raw.
func TestDiscoveredHostsDropsUncanonicalizable(t *testing.T) {
	got := DiscoveredHosts(discoveryScan("good.example", "", "not a host", "also.example"))
	joined := strings.Join(got, ",")
	for _, want := range []string{"good.example", "also.example"} {
		if !strings.Contains(joined, want) {
			t.Errorf("hosts %q missing %s", joined, want)
		}
	}
	if strings.Contains(joined, " ") {
		t.Errorf("hosts %q kept an uncanonicalizable entry", joined)
	}
}

// A registry miss defers the dimension rather than passing it vacuously —
// the §5.1.4 rule, now reachable without postgres.
func TestLiveLinksRegistryMissDefers(t *testing.T) {
	f := &fakeResources{registry: map[string]db.Ipv6Status{}} // swept nothing
	links := LiveLinks(context.Background(), f, discoveryScan("cdn.example.net"), true)
	if len(links) != 1 || links[0].AAAAStatus != nil {
		t.Fatalf("links = %+v, want one entry with a nil status", links)
	}
	if got := rollupResources(domain.ObsSupported, links); got != domain.ObsError {
		t.Errorf("rollup = %s, want error (deferred)", got)
	}
}

// A failed read must not claim the hosts are unswept-but-fine; it defers.
func TestLiveLinksReadErrorDefers(t *testing.T) {
	f := &fakeResources{err: errors.New("pool exhausted")}
	links := LiveLinks(context.Background(), f, discoveryScan("cdn.example.net"), true)
	if len(links) != 1 || links[0].AAAAStatus != nil {
		t.Fatalf("links = %+v, want a deferred entry", links)
	}
}

// The commit path follows the same convention: a failed link read is an
// unknown set, not an empty one (which would roll up to not_applicable).
func TestPersistedLinksReadErrorDefers(t *testing.T) {
	f := &fakeResources{err: errors.New("pool exhausted")}
	links := PersistedLinks(context.Background(), f, 1, true)
	if len(links) != 1 || links[0].AAAAStatus != nil {
		t.Fatalf("links = %+v, want a deferred entry", links)
	}
	if got := rollupResources(domain.ObsSupported, links); got != domain.ObsError {
		t.Errorf("rollup = %s, want error (deferred)", got)
	}
}

// The resources crawl being off means no LinkSet at all, on both paths.
func TestLinkSetDisabled(t *testing.T) {
	f := &fakeResources{registry: map[string]db.Ipv6Status{"cdn.example.net": "supported"}}
	if got := LiveLinks(context.Background(), f, discoveryScan("cdn.example.net"), false); got != nil {
		t.Errorf("LiveLinks disabled = %+v, want nil", got)
	}
	if got := PersistedLinks(context.Background(), f, 1, false); got != nil {
		t.Errorf("PersistedLinks disabled = %+v, want nil", got)
	}
}
