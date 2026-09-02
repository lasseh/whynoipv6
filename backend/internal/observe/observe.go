// Package observe is the neutral observation-mapping module between the
// engine and its two consumers: the Result→observation mapper
// (02-observation-model.md §7) and the §5.1.3 public live-result shape.
// Both the crawler daemon (commit pipeline, scan_detail hoists) and the
// read API (live check, evidence) depend on it downward — the API never
// imports the daemon.
package observe

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// ConnKey is the scan_detail hoist key for the derived conn object
// (03 §14.2); shared with the crawler's buildDetails serialization.
const ConnKey = "conn"

// preflightFreshness is the §5 constant: conn=unsupported is definitive only
// with a preflight pass younger than this (mirrors checker.PreflightFreshness).
const preflightFreshness = 5 * time.Minute

// LinkedResource is one required host's confirmed status in the effective
// required-host set (persisted pre-upsert links ∪ folded discovery output D;
// 02 §6).
type LinkedResource struct {
	Host       string             // canonical resource host (dedup key for the D-fold)
	AAAAStatus *domain.IPv6Status // nil = host never swept, or a D-folded host
}

// Observations is the mapper output consumed by the commit (03-state-machine.md).
type Observations struct {
	// Core dimensions — always set (never "").
	Base, WWW, NS, MX, Conn, Resources domain.Observation

	// Informational dimensions — always set (never ""); no confirmation machinery.
	DNSSEC, PTR, SMTP, Parity domain.Observation

	// TTFB averages; nil unless the respective latency check returned supported.
	LatencyV4Ms, LatencyV6Ms *int32

	// ConnDetail is the derived payload object hoisted into
	// scan_detail.details["conn"] (02 §5).
	ConnDetail map[string]any

	// ResourcesExcluded is true when crawler.resources.enabled=false: the
	// commit must skip the resources dimension in the confirm/pending loop.
	ResourcesExcluded bool
}

// MapObservations converts one engine scan into per-dimension observations
// (02-observation-model.md §7). It is pure: the links input is produced by
// the caller's read-only pre-commit roll-up query.
func MapObservations(
	kind domain.Kind,
	sr checker.ScanResult,
	preflightPassedAt time.Time,
	now time.Time,
	links []LinkedResource,
	resourcesEnabled bool,
) Observations {
	var o Observations

	baseSt, baseD := need(sr.Domain, checker.NameDNSAAAABase, sr.AAAABase)
	o.Base = mapAAAA(baseSt, &baseD, false)
	if kind == domain.KindSubdomain {
		o.WWW = domain.ObsNotApplicable
	} else {
		wwwSt, wwwD := need(sr.Domain, checker.NameDNSAAAAWWW, sr.AAAAWWW)
		o.WWW = mapAAAA(wwwSt, &wwwD, true)
	}
	o.NS = mapNS(needStatus(sr.Domain, checker.NameDNSNS, sr.NS))
	o.MX = mapMX(needStatus(sr.Domain, checker.NameDNSMX, sr.MX))
	hSt, hD := need(sr.Domain, checker.NameHTTPS, sr.HTTPS)
	o.Conn, o.ConnDetail = composeConn(hSt, hD.ErrorType, needStatus(sr.Domain, checker.NameHTTP, sr.HTTP), preflightPassedAt, now)

	if !resourcesEnabled {
		o.Resources = domain.ObsNotApplicable
		o.ResourcesExcluded = true
	} else {
		o.Resources = rollupResources(o.Conn, links)
	}

	// Informational dimensions (02 §7.4).
	o.DNSSEC = infoRaw(needStatus(sr.Domain, checker.NameDNSSEC, sr.DNSSEC), false)
	o.PTR = infoRaw(needStatus(sr.Domain, checker.NamePTR, sr.PTR), true)
	o.SMTP = infoSMTP(needStatus(sr.Domain, checker.NameSMTP, sr.SMTP))
	o.Parity = infoRaw(needStatus(sr.Domain, checker.NameParity, sr.Parity), true)
	v4St, v4D := need(sr.Domain, checker.NameLatencyV4, sr.LatencyV4)
	o.LatencyV4Ms = latencyMs(v4St, &v4D)
	v6St, v6D := need(sr.Domain, checker.NameLatencyV6, sr.LatencyV6)
	o.LatencyV6Ms = latencyMs(v6St, &v6D)

	return o
}

// need reads one check through its typed accessor with the §7.3 rule-7
// defensive fallback: a missing check logs and lands on status error.
func need[D any](host, name string, f func() (checker.CheckStatus, D, bool)) (st checker.CheckStatus, d D) {
	st, d, ok := f()
	if !ok {
		slog.Error("check result missing", "check", name, "domain", host)
	}
	return st, d
}

// needStatus is need for the consumers that only read the status.
func needStatus[D any](host, name string, f func() (checker.CheckStatus, D, bool)) checker.CheckStatus {
	st, _ := need(host, name, f)
	return st
}

// mapAAAA implements the §4 base/www composite tables over the §3 Result
// contract. www=true applies the two www substitutions (nxdomain→n/a,
// a_absent→n/a). The CD=1 rescue is transparent (reshaped upstream).
func mapAAAA(st checker.CheckStatus, d *checker.AAAADetail, www bool) domain.Observation {
	switch st {
	case checker.StatusError:
		if d.Inconsistent {
			return domain.ObsInconsistent // the only source of `inconsistent`
		}
		return domain.ObsError
	case checker.StatusSupported:
		return domain.ObsSupported
	case checker.StatusNotApplicable: // quorum nxdomain
		if www {
			return domain.ObsNotApplicable
		}
		return domain.ObsNoRecord
	case checker.StatusUnsupported: // quorum empty → by a_outcome
		switch d.AOutcome {
		case checker.AOutcomePresent:
			return domain.ObsUnsupported
		case checker.AOutcomeAbsent:
			if www {
				return domain.ObsNotApplicable
			}
			return domain.ObsNoRecord
		case checker.AOutcomeError:
			return domain.ObsError
		default:
			slog.Warn("a_outcome missing", "check_status", st)
			return domain.ObsError
		}
	default:
		// Includes the unreachable AAAA partial; the defensive default
		// warns and defers the dimension.
		return obsFromStatusDefensive(st)
	}
}

func mapNS(st checker.CheckStatus) domain.Observation {
	switch st {
	case checker.StatusSupported, checker.StatusPartial: // ≥1-host rule
		return domain.ObsSupported
	case checker.StatusUnsupported:
		return domain.ObsUnsupported
	case checker.StatusNotApplicable:
		slog.Warn("unexpected ns not_applicable")
		return domain.ObsError // defensive: can never confirm
	case checker.StatusError:
		return domain.ObsError
	default:
		return obsFromStatusDefensive(st)
	}
}

func mapMX(st checker.CheckStatus) domain.Observation {
	switch st {
	case checker.StatusSupported, checker.StatusPartial: // ≥1-host rule
		return domain.ObsSupported
	case checker.StatusUnsupported:
		return domain.ObsUnsupported
	case checker.StatusNotApplicable: // null-MX; subdomain no-explicit-MX
		return domain.ObsNotApplicable
	case checker.StatusError:
		return domain.ObsError
	default:
		return obsFromStatusDefensive(st)
	}
}

// composeConn is the §5 decision table (first match wins) plus the final
// preflight guard, and builds the scan_detail.details["conn"] payload.
func composeConn(hSt checker.CheckStatus, errType string, pSt checker.CheckStatus, preflightPassedAt, now time.Time) (obs domain.Observation, detail map[string]any) {
	preflightFresh := !preflightPassedAt.IsZero() && now.Sub(preflightPassedAt) <= preflightFreshness

	detail = map[string]any{"http_only": false}

	switch {
	case hSt == checker.StatusSupported: // row 1
		obs = domain.ObsSupported
		detail["source"] = "https"
	case hSt == checker.StatusUnsupported && errType == checker.ErrTypeConnRefused && pSt == checker.StatusSupported: // row 2
		obs = domain.ObsSupported
		detail["source"] = "http"
		detail["http_only"] = true
	case hSt == checker.StatusUnsupported: // rows 3–4 (cert error, refused w/o http, no-AAAA)
		obs = domain.ObsUnsupported
	case hSt == checker.StatusError && errType == checker.ErrTypeTimeout && preflightFresh: // row 5a
		obs = domain.ObsUnsupported
	case hSt == checker.StatusError: // rows 5b–5c
		obs = domain.ObsError
	case hSt == checker.StatusNotApplicable: // row 6
		obs = domain.ObsNotApplicable
	default:
		obs = obsFromStatusDefensive(hSt)
	}

	// Final preflight guard: EVERY conn=unsupported requires a fresh pass.
	if obs == domain.ObsUnsupported && !preflightFresh {
		obs = domain.ObsError
	}

	detail["status"] = string(obs)
	if errType != "" {
		detail["error_type"] = errType
	}
	if obs != domain.ObsSupported {
		delete(detail, "source")
	}
	return obs, detail
}

// rollupResources is the §6 registry roll-up (all branches normative).
func rollupResources(conn domain.Observation, links []LinkedResource) domain.Observation {
	switch conn {
	case domain.ObsError, domain.ObsInconsistent:
		return domain.ObsError // defer with conn
	case domain.ObsSupported:
		// fall through to the host fold below
	default:
		return domain.ObsNotApplicable // v6-unreachable site: deps moot
	}

	remaining := 0
	anyUnsupported := false
	for _, l := range links {
		if l.AAAAStatus == nil {
			return domain.ObsError // host not yet swept: defer
		}
		switch *l.AAAAStatus {
		case domain.StatusNoRecord, domain.StatusNotApplicable:
			continue // dead references are not evidence of v4-only dependence
		case domain.StatusUnsupported:
			anyUnsupported = true
			remaining++
		case domain.StatusSupported:
			remaining++
		}
	}
	switch {
	case remaining == 0:
		return domain.ObsNotApplicable // no (live) external deps
	case anyUnsupported:
		return domain.ObsUnsupported
	default:
		return domain.ObsSupported
	}
}

// obsFromStatusDefensive is the shared defensive default of the
// per-dimension bridge tables (mapAAAA/mapNS/mapMX/composeConn): a status a
// decision table does not enumerate is non-definitive. It warns — so an
// enum addition or an unreachable branch is loud instead of silently
// landing on error — and defers the dimension.
func obsFromStatusDefensive(st checker.CheckStatus) domain.Observation {
	slog.Warn("check status outside bridge table", "status", st)
	return domain.ObsError
}

// obsFromStatus is the single CheckStatus→Observation value bridge. The two
// enums are unrelated string types; this table is the one place their value
// correspondence is written down — an unknown status lands on error rather
// than leaking through a silent cast.
func obsFromStatus(st checker.CheckStatus) domain.Observation {
	switch st {
	case checker.StatusSupported:
		return domain.ObsSupported
	case checker.StatusPartial:
		return domain.ObsPartial
	case checker.StatusUnsupported:
		return domain.ObsUnsupported
	case checker.StatusNotApplicable:
		return domain.ObsNotApplicable
	case checker.StatusError:
		return domain.ObsError
	default:
		slog.Warn("unknown check status", "status", st)
		return domain.ObsError
	}
}

// infoRaw stores the raw engine status; partialOK=false defensively maps an
// illegal partial to error (02 §7.4).
func infoRaw(st checker.CheckStatus, partialOK bool) domain.Observation {
	if st == checker.StatusPartial && !partialOK {
		slog.Warn("unexpected partial on informational dimension")
		return domain.ObsError
	}
	return obsFromStatus(st)
}

func infoSMTP(st checker.CheckStatus) domain.Observation {
	if st == checker.StatusPartial {
		return domain.ObsUnsupported // a half-working EHLO does not accept mail
	}
	return obsFromStatus(st)
}

func latencyMs(st checker.CheckStatus, d *checker.LatencyDetail) *int32 {
	if st != checker.StatusSupported || d.AvgMS == nil {
		return nil
	}
	ms := int32(*d.AvgMS) //nolint:gosec // milliseconds under the probe timeout
	return &ms
}

// QuorumHoist builds the details["consensus"] payload object (02 §7.5).
func QuorumHoist(sr checker.ScanResult) map[string]any {
	out := map[string]any{}
	if _, d, ok := sr.AAAABase(); ok && d.Quorum != nil {
		out["base"] = d.Quorum
	}
	if _, d, ok := sr.AAAAWWW(); ok && d.Quorum != nil {
		out["www"] = d.Quorum
	}
	return out
}

// MapLiveResult renders one engine scan as the §5.1.3 public result object —
// the ONE mapping with four consumers: the live-check consumer, the
// domain-side dedupe path, the §4.3 evidence block, and (via
// MapObservations) the frontier commit. Statuses are raw observations
// (incl. inconsistent on base/www quorum splits), explicitly NOT confirmed
// state.
func MapLiveResult(
	kind domain.Kind,
	sr checker.ScanResult,
	preflightPassedAt time.Time,
	now time.Time,
	links []LinkedResource,
	resourcesEnabled bool,
) map[string]any {
	o := MapObservations(kind, sr, preflightPassedAt, now, links, resourcesEnabled)

	status := func(v domain.Observation) map[string]any {
		return map[string]any{"status": string(v)}
	}
	// tls/parity/dnssec/ptr/spf ride the raw engine status; smtp maps
	// partial → unsupported (07 §5.1.4).
	raw := func(st checker.CheckStatus) map[string]any {
		return map[string]any{"status": string(st)}
	}
	checks := map[string]any{
		string(domain.DimBase):      status(o.Base),
		string(domain.DimWWW):       status(o.WWW),
		string(domain.DimNS):        status(o.NS),
		string(domain.DimMX):        status(o.MX),
		string(domain.DimConn):      status(o.Conn),
		string(domain.DimResources): status(o.Resources),
		"tls":                       raw(needStatus(sr.Domain, checker.NameTLS, sr.TLS)),
		"smtp":                      status(infoSMTP(needStatus(sr.Domain, checker.NameSMTP, sr.SMTP))),
		"parity":                    raw(needStatus(sr.Domain, checker.NameParity, sr.Parity)),
		"dnssec":                    raw(needStatus(sr.Domain, checker.NameDNSSEC, sr.DNSSEC)),
		"ptr":                       raw(needStatus(sr.Domain, checker.NamePTR, sr.PTR)),
		"spf":                       raw(needStatus(sr.Domain, checker.NameSPF, sr.SPF)),
	}
	var v4, v6 any
	if o.LatencyV4Ms != nil {
		v4 = *o.LatencyV4Ms
	}
	if o.LatencyV6Ms != nil {
		v6 = *o.LatencyV6Ms
	}
	return map[string]any{
		"checked_at":  sr.ScannedAt.UTC().Format(time.RFC3339),
		"duration_ms": sr.Duration.Milliseconds(),
		"checks":      checks,
		"latency":     map[string]any{"v4_ms": v4, "v6_ms": v6},
	}
}

// The LinkSet constructors: PersistedLinks (commit path) and LiveLinks
// (live-check path) both normalize registry state onto the one roll-up
// convention — a missing or NULL status stays nil and defers the resources
// dimension in rollupResources. The pure folds below are the tested core.

// linksFromRows normalizes the persisted required-link rows (02 §6).
func linksFromRows(rows []db.DomainRequiredLinksRow) []LinkedResource {
	links := make([]LinkedResource, 0, len(rows))
	for _, row := range rows {
		l := LinkedResource{Host: row.Host}
		if row.AaaaStatus != nil {
			s := domain.IPv6Status(*row.AaaaStatus)
			l.AAAAStatus = &s
		}
		links = append(links, l)
	}
	return links
}

// FoldDiscovered appends this scan's discovery output D to the persisted
// link set as NULL-status entries — the 02 §6 D-fold. A discovered host not
// yet among the persisted links contributes a nil status, which defers the
// roll-up (error) until its swept status is persisted; it can never falsely
// advance. Hosts already persisted keep their real status.
func FoldDiscovered(links []LinkedResource, discovered []string) []LinkedResource {
	if len(discovered) == 0 {
		return links
	}
	seen := make(map[string]bool, len(links))
	for _, l := range links {
		seen[l.Host] = true
	}
	for _, h := range discovered {
		if !seen[h] {
			seen[h] = true
			links = append(links, LinkedResource{Host: h})
		}
	}
	return links
}

// linksForHosts normalizes discovered hosts against the registry probe; a
// host absent from byHost has no registry row (or a NULL status).
func linksForHosts(rawHosts []string, byHost map[string]domain.IPv6Status) []LinkedResource {
	links := make([]LinkedResource, 0, len(rawHosts))
	for _, h := range rawHosts {
		if s, ok := byHost[h]; ok {
			links = append(links, LinkedResource{Host: h, AAAAStatus: &s})
		} else {
			links = append(links, LinkedResource{Host: h}) // missing/unswept → error in the roll-up
		}
	}
	return links
}

// ResourceReader is the seam under the two LinkSet constructors — the only
// database access in this package. *db.Queries satisfies it in production
// (via Resources); the package tests substitute an in-memory reader, which
// is what makes the constructors reachable without postgres.
type ResourceReader interface {
	DomainRequiredLinks(ctx context.Context, domainID int64) ([]db.DomainRequiredLinksRow, error)
	ResourceHostStatuses(ctx context.Context, hosts []string) ([]db.ResourceHostStatusesRow, error)
}

// Resources adapts a pool to the ResourceReader seam.
func Resources(pool *pgxpool.Pool) ResourceReader { return db.New(pool) }

// DiscoveredHosts canonicalizes a scan's discovered resource hosts —
// the one host-form convention both LinkSet paths share (06 §1
// call-site table; canonicalization failures are skipped).
//
// The registry is keyed on the canonical form and ResourceHostStatuses
// matches host equality exactly, so a raw url.Hostname() string —
// unicode where the registry holds punycode, an IP literal, a
// trailing-dot FQDN — silently misses and defers the resources
// dimension. Canonicalizing here rather than in a caller is what keeps
// the commit and live paths agreeing.
func DiscoveredHosts(sr checker.ScanResult) []string {
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

// PersistedLinks loads the persisted required-host statuses for one domain —
// the commit-path LinkSet constructor (02 §6), sibling of LiveLinks.
func PersistedLinks(ctx context.Context, r ResourceReader, domainID int64, enabled bool) []LinkedResource {
	if !enabled {
		return nil
	}
	rows, err := r.DomainRequiredLinks(ctx, domainID)
	if err != nil {
		// The LinkSet convention (CONTEXT.md, and LiveLinks): a read that
		// failed is an unknown set, never an empty one — an empty set
		// would roll up to a definitive not_applicable. One nil-status
		// entry defers the dimension for this scan.
		slog.Warn("resource link read failed", "err", err.Error())
		return []LinkedResource{{}}
	}
	return linksFromRows(rows)
}

// LiveLinks resolves the run's discovered resource hosts against the
// confirmed registry — read-only, no registry rows written (Rule 0). A
// discovered host with no registry row maps to a nil status (→ error in
// the roll-up, per §5.1.4 "missing/unswept → NULL → error").
func LiveLinks(ctx context.Context, r ResourceReader, sr checker.ScanResult, enabled bool) []LinkedResource {
	if !enabled {
		return nil
	}
	hosts := DiscoveredHosts(sr)
	if len(hosts) == 0 {
		return nil
	}
	// One set-based probe, not a per-host loop.
	byHost := map[string]domain.IPv6Status{}
	rows, err := r.ResourceHostStatuses(ctx, hosts)
	if err != nil {
		slog.Warn("resource host status read failed", "err", err.Error())
	}
	for _, row := range rows {
		if row.AaaaStatus != nil {
			byHost[row.Host] = domain.IPv6Status(*row.AaaaStatus)
		}
	}
	return linksForHosts(hosts, byHost)
}
