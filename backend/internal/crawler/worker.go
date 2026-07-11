package crawler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	"github.com/lasseh/whynoipv6/internal/geoip"
	"github.com/lasseh/whynoipv6/internal/ingest"
	"github.com/lasseh/whynoipv6/internal/observe"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// Worker is the per-domain slot body (04 §12 a–f): engine run → map →
// schedule/commit → metrics, plus attribution and the pivot stamps.
type Worker struct {
	Pool      *pgxpool.Pool
	Runner    *checker.Runner
	Preflight *checker.Preflight
	Committer *Committer
	Metrics   *Metrics

	// BreakerOpen reports the consensus fast-lane breaker state (nil = closed).
	BreakerOpen func() bool

	// Attribution inputs (06 §6). Attr may be nil (no mmdb — attribution
	// then defers to the snapshot on every scan).
	Attr      *geoip.Attributor
	Countries *geoip.CountryMap

	// Providers is the ns_host → provider snapshot (nil = no stamping).
	Providers *ingest.ProviderMapping

	ResourcesEnabled bool
}

// Process runs one claimed domain end to end.
func (w *Worker) Process(ctx context.Context, d ClaimedDomain) { //nolint:gocritic // the Frontier slot contract passes by value
	start := time.Now()
	t := time.Now().UTC() // T — fixed once per domain (03 §3)

	sr := w.Runner.Run(ctx, d.Host, checker.Kind(d.Kind))

	links := w.readLinks(ctx, d.ID)
	obs := observe.MapObservations(d.Kind, sr, w.Preflight.LastPass(), t, links, w.ResourcesEnabled)

	unresolvable := Unresolvable(sr)
	attribution := w.attribution(ctx, &d, sr)

	details := buildDetails(sr, &obs)
	breakerOpen := false
	if w.BreakerOpen != nil {
		breakerOpen = w.BreakerOpen()
	}

	in := &CommitInput{
		Snapshot:     d,
		Obs:          obs,
		Unresolvable: unresolvable,
		Attribution:  attribution,
		BreakerOpen:  breakerOpen,
		Details:      details,
		DurationMS:   int32(time.Since(start).Milliseconds()),
		T:            t,
	}
	if w.ResourcesEnabled {
		if st, _, ok := sr.ResourceDiscovery(); ok && st == checker.StatusSupported {
			in.DiscoveryOK = true
			in.Discovered = discoveredHosts(sr)
		}
	}

	res, err := w.Committer.Commit(ctx, in)
	if err == nil && !res.LeaseLost {
		w.stampPivots(ctx, &d, sr)
		if wasStepR(&d, &obs) {
			w.Metrics.RecordRecovered()
		}
	}
	w.Metrics.RecordScan(ctx, &obs, unresolvable, res, err, time.Since(start))
	slog.Debug("domain processed", "domain", d.Host,
		"duration_ms", time.Since(start).Milliseconds(),
		"lease_lost", res.LeaseLost, "transitions", len(res.Transitions))
}

// readLinks loads the persisted required-host statuses for the roll-up
// (02 §6); only meaningful while resources are enabled.
func (w *Worker) readLinks(ctx context.Context, domainID int64) []observe.LinkedResource {
	if !w.ResourcesEnabled {
		return nil
	}
	statuses, err := db.New(w.Pool).DomainRequiredLinks(ctx, domainID)
	if err != nil {
		slog.Warn("resource link read failed", "err", err.Error())
		return nil
	}
	var links []observe.LinkedResource
	for _, status := range statuses {
		var l observe.LinkedResource
		if status != nil {
			s := domain.IPv6Status(*status)
			l.AAAAStatus = &s
		}
		links = append(links, l)
	}
	return links
}

// attribution computes A (06 §6.2–§6.4): input IP from this scan's base
// answers (AAAA wins over the conditional A), ASN ensure-by-number, ccTLD
// country. nil = deferred (no readers, or non-definitive base handled by
// the commit itself).
func (w *Worker) attribution(ctx context.Context, d *ClaimedDomain, sr checker.ScanResult) *Attribution {
	if w.Attr == nil || w.Countries == nil {
		return nil
	}
	ip := attributionIP(sr)

	countryID := w.Attr.CountryID(d.Host, ip)
	asnID := w.Countries.SentinelASN
	if res := w.Attr.ASN(ip); res.Number != 0 {
		if id, err := w.ensureASN(ctx, int64(res.Number), res.Org); err == nil {
			asnID = id
		}
	}
	return &Attribution{AsnID: asnID, CountryID: countryID}
}

// attributionIP extracts the input IP from the base composite (06 §6.2):
// the first recorded AAAA wins; else the conditional-A address; else zero.
func attributionIP(sr checker.ScanResult) netip.Addr {
	var zero netip.Addr
	_, base, ok := sr.AAAABase()
	if !ok {
		return zero
	}
	if len(base.Addresses) > 0 {
		if a, err := netip.ParseAddr(base.Addresses[0]); err == nil {
			return a
		}
	}
	if base.AAddress != "" {
		if a, err := netip.ParseAddr(base.AAddress); err == nil {
			return a
		}
	}
	return zero
}

func (w *Worker) ensureASN(ctx context.Context, number int64, org string) (int32, error) {
	q := db.New(w.Pool)
	if org == "" {
		org = "Unknown"
	}
	id, err := q.ASNEnsure(ctx, db.ASNEnsureParams{Number: number, Name: org})
	if err == nil {
		return id, nil
	}
	return q.ASNIDByNumber(ctx, number) // conflict: re-read
}

// stampPivots writes the dns_provider_id / hosting_provider attribution
// pivots after a successful commit (06 §6.10): definitive-base scans only,
// idempotent, self-healing next scan. (03 §12.1's pinned fenced UPDATE does
// not carry these columns, so they ride separate pivot-only statements.)
func (w *Worker) stampPivots(ctx context.Context, d *ClaimedDomain, sr checker.ScanResult) {
	baseSt, _, _ := sr.AAAABase()
	definitive := baseSt == checker.StatusSupported ||
		baseSt == checker.StatusUnsupported || baseSt == checker.StatusNotApplicable
	if !definitive {
		return // deferred scans never touch the pivots (06 §6.6)
	}
	q := db.New(w.Pool)

	if w.Providers != nil {
		if err := ingest.StampDNSProvider(ctx, q, w.Providers, d.ID, nsHosts(sr)); err != nil {
			slog.Warn("dns provider stamp failed", "domain", d.Host, "err", err.Error())
		}
	}

	// Hosting tag: CNAME-CDN detection from the www check + the resolved
	// input IP's ASN (already looked up — no new queries).
	_, www, _ := sr.AAAAWWW()
	cdnDetected, chain := www.CDNDetected, www.CNAMEChain
	var asn uint
	if w.Attr != nil {
		if ip := attributionIP(sr); ip.IsValid() {
			n, _ := w.Attr.Meta.ASN(ip)
			asn = n
		}
	}
	tag := ingest.NormalizeHosting(cdnDetected, chain, asn)
	if err := ingest.StampHostingProvider(ctx, q, d.ID, tag); err != nil {
		slog.Warn("hosting stamp failed", "domain", d.Host, "err", err.Error())
	}
}

// nsHosts extracts the observed nameserver-host set from the NS check
// (06 §6.10 step 1).
func nsHosts(sr checker.ScanResult) []string {
	_, ns, ok := sr.NS()
	if !ok || len(ns.Nameservers) == 0 {
		return nil
	}
	hosts := make([]string, 0, len(ns.Nameservers))
	for h := range ns.Nameservers {
		hosts = append(hosts, h)
	}
	return hosts
}

// discoveredHosts canonicalizes the resource_discovery host list
// (canonicalization failures are skipped — 06 §1 call-site table).
func discoveredHosts(sr checker.ScanResult) []string {
	_, d, ok := sr.ResourceDiscovery()
	if !ok {
		return nil
	}
	out := make([]string, 0, len(d.Hosts))
	for _, h := range d.Hosts {
		if canonical, err := domain.Canonicalize(h); err == nil {
			out = append(out, canonical)
		}
	}
	return out
}

// wasStepR detects a dead-recovery commit for the metrics counter.
func wasStepR(d *ClaimedDomain, obs *observe.Observations) bool {
	return d.Disabled && d.DisabledReason != nil &&
		*d.DisabledReason == domain.DisabledDead && obs.Base.Definitive()
}

// buildDetails assembles the scan_detail payload (03 §14.2): the engine
// ScanResult serialization plus the conn and consensus hoists.
func buildDetails(sr checker.ScanResult, obs *observe.Observations) []byte {
	payload := map[string]any{
		"domain":        sr.Domain,
		"scanned_at":    sr.ScannedAt.UTC().Format(time.RFC3339),
		"duration":      sr.Duration.Nanoseconds(),
		"results":       sr.Results,
		observe.ConnKey: obs.ConnDetail,
	}
	if consensusHoist := observe.QuorumHoist(sr); len(consensusHoist) > 0 {
		payload["consensus"] = consensusHoist
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.Error("scan detail marshal failed", "domain", sr.Domain, "err", err.Error())
		return []byte(`{}`)
	}
	return raw
}
