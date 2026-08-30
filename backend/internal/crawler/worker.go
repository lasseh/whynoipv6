package crawler

import (
	"bytes"
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

// The Worker's consumer-side seams: each names exactly what Process needs
// from a collaborator, so the per-domain orchestration (gate order,
// deferrals, pivot/metric conditions) is unit-testable with fakes — the
// second adapter at each seam.

// Scanner runs the engine for one host — *checker.Runner in production.
type Scanner interface {
	Run(ctx context.Context, host string, kind domain.Kind) checker.ScanResult
}

// PreflightState exposes the vantage-health timestamp the observation
// mapper needs — *checker.Preflight in production.
type PreflightState interface {
	LastPass() time.Time
}

// CommitSink accepts one scan's commit — *Committer in production.
type CommitSink interface {
	Commit(ctx context.Context, in *CommitInput) (CommitResult, error)
}

// Enricher computes this scan's attribution input and provider pivots —
// *GeoEnricher in production; nil skips both (attribution then defers to
// the snapshot on every scan). Both run before the commit; the pivots ride
// the fenced UPDATE inside it.
type Enricher interface {
	Attribution(ctx context.Context, d *ClaimedDomain, sr checker.ScanResult) *Attribution
	Pivots(sr checker.ScanResult) *Pivots
}

// Worker is the per-domain slot body (04 §12 a–f): engine run → map →
// schedule/commit → metrics, plus attribution and the pivot stamps.
type Worker struct {
	Pool      *pgxpool.Pool // required-link reads (observe.PersistedLinks)
	Scanner   Scanner
	Preflight PreflightState
	Committer CommitSink
	Metrics   *Metrics

	// BreakerOpen reports the consensus fast-lane breaker state (nil = closed).
	BreakerOpen func() bool

	// Enrich is the attribution + pivot seam (nil = neither runs).
	Enrich Enricher

	// Links loads the persisted required-host statuses for the resources
	// roll-up (nil = the observe.PersistedLinks adapter over Pool).
	Links func(ctx context.Context, domainID int64) []observe.LinkedResource

	ResourcesEnabled bool
}

// Process runs one claimed domain end to end.
func (w *Worker) Process(ctx context.Context, d ClaimedDomain) { //nolint:gocritic // the Frontier slot contract passes by value
	start := time.Now()
	t := time.Now().UTC() // T — fixed once per domain (03 §3)

	sr := w.Scanner.Run(ctx, d.Host, d.Kind)

	var links []observe.LinkedResource
	var discovered []string
	discoveryOK := false
	if w.ResourcesEnabled {
		if st, _, ok := sr.ResourceDiscovery(); ok && st == checker.StatusSupported {
			discoveryOK = true
			discovered = observe.DiscoveredHosts(sr)
		}
		if w.Links != nil {
			links = w.Links(ctx, d.ID)
		} else {
			links = observe.PersistedLinks(ctx, observe.Resources(w.Pool), d.ID, true)
		}
		// The 02 §6 D-fold: hosts discovered this scan but not yet persisted
		// enter the roll-up as NULL entries, deferring the resources
		// dimension instead of confirming a vacuous not_applicable.
		links = observe.FoldDiscovered(links, discovered)
	}
	obs := observe.MapObservations(d.Kind, sr, w.Preflight.LastPass(), t, links, w.ResourcesEnabled)

	unresolvable := Unresolvable(sr)
	var attribution *Attribution
	var pivots *Pivots
	if w.Enrich != nil {
		attribution = w.Enrich.Attribution(ctx, &d, sr)
		pivots = w.Enrich.Pivots(sr)
	}

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
		Pivots:       pivots,
		BreakerOpen:  breakerOpen,
		Details:      details,
		DurationMS:   int32(time.Since(start).Milliseconds()),
		T:            t,
	}
	if discoveryOK {
		in.DiscoveryOK = true
		in.Discovered = discovered
	}

	res, err := w.Committer.Commit(ctx, in)
	if err == nil && !res.LeaseLost && res.Recovered {
		w.Metrics.RecordRecovered()
	}
	w.Metrics.RecordScan(ctx, &obs, unresolvable, res, err, time.Since(start))
	slog.Debug("domain processed", "domain", d.Host,
		"duration_ms", time.Since(start).Milliseconds(),
		"lease_lost", res.LeaseLost, "transitions", len(res.Transitions))
}

// GeoEnricher is the production Enricher: geoip attribution plus the
// provider pivot stamps, sharing one pool and one mmdb reader.
type GeoEnricher struct {
	Pool *pgxpool.Pool

	// Attr may be nil (no mmdb — attribution then defers to the snapshot
	// on every scan).
	Attr      *geoip.Attributor
	Countries *geoip.CountryMap

	// Providers is the ns_host → provider snapshot (nil = no stamping).
	Providers *ingest.ProviderMapping
}

// Attribution computes A (06 §6.2–§6.4): input IP from this scan's base
// answers (AAAA wins over the conditional A), ASN ensure-by-number, ccTLD
// country. nil = deferred (no readers, or non-definitive base handled by
// the commit itself).
func (e *GeoEnricher) Attribution(ctx context.Context, d *ClaimedDomain, sr checker.ScanResult) *Attribution {
	if e.Attr == nil || e.Countries == nil {
		return nil
	}
	ip := attributionIP(sr)

	countryID := e.Attr.CountryID(d.Host, ip)
	asnID := e.Countries.SentinelASN
	if res := e.Attr.ASN(ip); res.Number != 0 {
		if id, err := e.ensureASN(ctx, int64(res.Number), res.Org); err == nil {
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

func (e *GeoEnricher) ensureASN(ctx context.Context, number int64, org string) (int32, error) {
	q := db.New(e.Pool)
	if org == "" {
		org = "Unknown"
	}
	id, err := q.ASNEnsure(ctx, db.ASNEnsureParams{Number: number, Name: org})
	if err == nil {
		return id, nil
	}
	return q.ASNIDByNumber(ctx, number) // conflict: re-read
}

// Pivots computes the dns_provider_id / hosting_provider attribution
// stamps (06 §6.10): definitive-base scans only, idempotent, self-healing
// next scan. Pure snapshot/mmdb lookups — the write rides the commit's
// fenced UPDATE, so a lost lease discards the pivots with the rest of the
// scan.
func (e *GeoEnricher) Pivots(sr checker.ScanResult) *Pivots {
	baseSt, _, _ := sr.AAAABase()
	definitive := baseSt == checker.StatusSupported ||
		baseSt == checker.StatusUnsupported || baseSt == checker.StatusNotApplicable
	if !definitive {
		return nil // deferred scans never touch the pivots (06 §6.6)
	}

	p := &Pivots{}
	if e.Providers != nil {
		p.StampDNS = true
		p.DNSProvider = e.Providers.ProviderForNSSet(nsHosts(sr))
	}

	// Hosting tag: CNAME-CDN detection from the www check + the resolved
	// input IP's ASN (already looked up — no new queries).
	_, www, _ := sr.AAAAWWW()
	cdnDetected, chain := www.CDNDetected, www.CNAMEChain
	var asn uint
	if e.Attr != nil {
		if ip := attributionIP(sr); ip.IsValid() {
			n, _ := e.Attr.Meta.ASN(ip)
			asn = n
		}
	}
	p.Hosting = ingest.NormalizeHosting(cdnDetected, chain, asn)
	return p
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

// nulEscape is how json.Marshal writes a NUL byte. Spelled byte-by-byte
// because a Go literal of it is the very thing it looks for.
var nulEscape = []byte{'\\', 'u', '0', '0', '0', '0'}

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
	if bytes.Contains(raw, nulEscape) {
		// Some remote string reached the payload without passing
		// checker.sanitizeText. jsonb rejects the escape (SQLSTATE 22P05)
		// and takes the entire commit batch with it, so drop the details
		// and keep the scan. A literal backslash before the escape text is
		// a false positive; costing one scan's details is the cheaper error.
		slog.Error("scan detail carries a NUL escape, dropping details", "domain", sr.Domain)
		return []byte(`{}`)
	}
	return raw
}
