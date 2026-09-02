package crawler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/observe"
)

// The fakes below are the second adapters at the Worker's seams: they let
// the per-domain orchestration gates run without Postgres or network.

type fakeScanner struct{ sr checker.ScanResult }

func (f *fakeScanner) Run(context.Context, string, domain.Kind) checker.ScanResult { return f.sr }

type fakePreflight struct{ at time.Time }

func (f *fakePreflight) LastPass() time.Time { return f.at }

type fakeSink struct {
	in  *CommitInput
	res CommitResult
	err error
}

func (f *fakeSink) Commit(_ context.Context, in *CommitInput) (CommitResult, error) {
	f.in = in
	return f.res, f.err
}

type fakeEnricher struct {
	attribution *Attribution
	pivots      *Pivots
}

func (f *fakeEnricher) Attribution(context.Context, *ClaimedDomain, checker.ScanResult) *Attribution {
	return f.attribution
}

func (f *fakeEnricher) Pivots(checker.ScanResult) *Pivots {
	return f.pivots
}

// scanOK is a definitive-base scan with a successful resource discovery.
func scanOK() checker.ScanResult {
	return checker.ScanResult{Results: map[string]checker.Result{
		checker.NameDNSAAAABase: {Status: checker.StatusSupported,
			Detail: &checker.AAAADetail{Rcode: checker.RcodeNoError, Addresses: []string{"2001:db8::1"}}},
		checker.NameDNSAAAAWWW: {Status: checker.StatusSupported, Detail: &checker.AAAADetail{}},
		checker.NameDNSNS:      {Status: checker.StatusSupported, Detail: &checker.NSDetail{}},
		checker.NameDNSMX:      {Status: checker.StatusSupported, Detail: &checker.MXDetail{}},
		checker.NameHTTPS:      {Status: checker.StatusSupported, Detail: &checker.HTTPDetail{}},
		checker.NameHTTP:       {Status: checker.StatusSupported, Detail: &checker.HTTPDetail{}},
		checker.NameDNSSEC:     {Status: checker.StatusSupported, Detail: &checker.DNSSECDetail{}},
		checker.NamePTR:        {Status: checker.StatusSupported, Detail: &checker.PTRDetail{}},
		checker.NameSMTP:       {Status: checker.StatusSupported, Detail: &checker.SMTPDetail{}},
		checker.NameParity:     {Status: checker.StatusSupported, Detail: &checker.ParityDetail{}},
		checker.NameLatencyV4:  {Status: checker.StatusSupported, Detail: &checker.LatencyDetail{}},
		checker.NameLatencyV6:  {Status: checker.StatusSupported, Detail: &checker.LatencyDetail{}},
		checker.NameResourceDiscovery: {Status: checker.StatusSupported,
			Detail: &checker.ResourceDiscoveryDetail{Hosts: []string{"cdn.example"}}},
	}}
}

func testWorker(sink *fakeSink, enrich Enricher, sr checker.ScanResult) *Worker {
	return &Worker{
		Scanner:   &fakeScanner{sr: sr},
		Preflight: &fakePreflight{at: time.Now().UTC()},
		Committer: sink,
		Metrics:   NewMetrics(nil, uuid.New(), "test"),
		Enrich:    enrich,
	}
}

func claimed() ClaimedDomain {
	rank := int32(100)
	return ClaimedDomain{
		ID: 1, Host: "w.example", Kind: domain.KindApex, Rank: &rank,
		ClaimedAt: time.Now().UTC(),
		Dims: map[domain.Dimension]DimState{
			domain.DimBase: {}, domain.DimWWW: {}, domain.DimNS: {},
			domain.DimMX: {}, domain.DimConn: {}, domain.DimResources: {},
		},
	}
}

// TestProcessCommitInput pins the input-assembly gates: enricher
// attribution rides the input, and discovery only opens with the resources
// crawl enabled.
func TestProcessCommitInput(t *testing.T) {
	sink := &fakeSink{}
	attr := &Attribution{AsnID: 7, CountryID: 9}
	w := testWorker(sink, &fakeEnricher{attribution: attr}, scanOK())

	w.Process(context.Background(), claimed())
	if sink.in == nil {
		t.Fatal("commit never received input")
	}
	if sink.in.Attribution != attr {
		t.Errorf("attribution = %+v, want the enricher's", sink.in.Attribution)
	}
	if sink.in.DiscoveryOK || len(sink.in.Discovered) != 0 {
		t.Error("discovery must stay closed while resources are disabled")
	}
	if sink.in.Unresolvable {
		t.Error("a supported base scan is never the dead signal")
	}

	// With resources enabled (links injected), discovery opens and carries
	// the canonicalized host.
	sink.in = nil
	w2 := testWorker(sink, &fakeEnricher{}, scanOK())
	w2.ResourcesEnabled = true
	w2.Links = func(context.Context, int64) []observe.LinkedResource { return nil }
	w2.Process(context.Background(), claimed())
	if !sink.in.DiscoveryOK || len(sink.in.Discovered) != 1 || sink.in.Discovered[0] != "cdn.example" {
		t.Errorf("discovery = %t/%v, want open with cdn.example", sink.in.DiscoveryOK, sink.in.Discovered)
	}
	// The D-fold (02 §6): the freshly discovered host has no persisted
	// status, so resources defers instead of confirming not_applicable.
	if sink.in.Obs.Resources != domain.ObsError {
		t.Errorf("resources = %s, want error (D-fold defer)", sink.in.Obs.Resources)
	}

	// Once the discovered host is persisted with a swept status, the
	// roll-up advances on the persisted link, not the fold.
	sink.in = nil
	sup := domain.StatusSupported
	w3 := testWorker(sink, &fakeEnricher{}, scanOK())
	w3.ResourcesEnabled = true
	w3.Links = func(context.Context, int64) []observe.LinkedResource {
		return []observe.LinkedResource{{Host: "cdn.example", AAAAStatus: &sup}}
	}
	w3.Process(context.Background(), claimed())
	if sink.in.Obs.Resources != domain.ObsSupported {
		t.Errorf("resources = %s, want supported once the link is persisted", sink.in.Obs.Resources)
	}
}

// TestProcessNilEnricher: without an enricher, attribution and the pivots
// both defer (nil) — the commit leaves the pivot columns untouched.
func TestProcessNilEnricher(t *testing.T) {
	sink := &fakeSink{}
	w := testWorker(sink, nil, scanOK())
	w.Process(context.Background(), claimed())
	if sink.in.Attribution != nil {
		t.Errorf("attribution = %+v, want deferred nil", sink.in.Attribution)
	}
	if sink.in.Pivots != nil {
		t.Errorf("pivots = %+v, want deferred nil", sink.in.Pivots)
	}
}

// TestProcessPivotsDelivery: the enricher's pivots ride the CommitInput —
// the fenced UPDATE is what gates them on lease loss, so the worker's only
// job is delivery.
func TestProcessPivotsDelivery(t *testing.T) {
	sink := &fakeSink{}
	id := int64(4)
	p := &Pivots{StampDNS: true, DNSProvider: &id}
	w := testWorker(sink, &fakeEnricher{pivots: p}, scanOK())
	w.Process(context.Background(), claimed())
	if sink.in.Pivots != p {
		t.Errorf("pivots = %+v, want the enricher's", sink.in.Pivots)
	}
}

// TestProcessRecoveryMetric: the step-R recovery counter follows the commit
// result — the pure machine decides, the edge only tallies — and rides the
// same success gate as the pivots.
func TestProcessRecoveryMetric(t *testing.T) {
	cases := []struct {
		name string
		res  CommitResult
		want int
	}{
		{"recovered", CommitResult{Recovered: true}, 1},
		{"no step R", CommitResult{}, 0},
		{"lease lost", CommitResult{LeaseLost: true, Recovered: true}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := testWorker(&fakeSink{res: tc.res}, nil, scanOK())
			w.Process(context.Background(), claimed())
			if got := w.Metrics.c.recovered; got != tc.want {
				t.Errorf("recovered = %d, want %d", got, tc.want)
			}
		})
	}
}
