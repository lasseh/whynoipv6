// Package crawler contains the scan→observe→commit pipeline around the
// lifted engine: the Result→observation mapper (02-observation-model.md §7),
// the confirmed-status commit machine (03), and the frontier/scheduling
// machinery (04).
package crawler

import (
	"log/slog"
	"time"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
)

// Conditional-A outcome tokens (02 §2.7 — mirrored from internal/consensus).
const (
	aPresent = "a_present"
	aAbsent  = "a_absent"
	aError   = "a_error"
)

// error_type wire tokens (01-engine.md §11.7 — the conn table keys off them).
const (
	errTypeConnRefused = "connection_refused"
	errTypeTimeout     = "timeout"
)

// preflightFreshness is the §5 constant: conn=unsupported is definitive only
// with a preflight pass younger than this (mirrors checker.PreflightFreshness).
const preflightFreshness = 5 * time.Minute

// LinkedResource is one required host's confirmed status in the effective
// required-host set (persisted pre-upsert links ∪ folded discovery output D;
// 02 §6).
type LinkedResource struct {
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
		case aPresent:
			return domain.ObsUnsupported
		case aAbsent:
			if www {
				return domain.ObsNotApplicable
			}
			return domain.ObsNoRecord
		case aError:
			return domain.ObsError
		default:
			slog.Warn("a_outcome missing", "check_status", st)
			return domain.ObsError
		}
	case checker.StatusPartial:
		// Unreachable: the AAAA checks never emit partial. Non-definitive.
		return domain.ObsError
	default:
		return domain.ObsError
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
		return domain.ObsError
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
		return domain.ObsError
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
	case hSt == checker.StatusUnsupported && errType == errTypeConnRefused && pSt == checker.StatusSupported: // row 2
		obs = domain.ObsSupported
		detail["source"] = "http"
		detail["http_only"] = true
	case hSt == checker.StatusUnsupported: // rows 3–4 (cert error, refused w/o http, no-AAAA)
		obs = domain.ObsUnsupported
	case hSt == checker.StatusError && errType == errTypeTimeout && preflightFresh: // row 5a
		obs = domain.ObsUnsupported
	case hSt == checker.StatusError: // rows 5b–5c
		obs = domain.ObsError
	case hSt == checker.StatusNotApplicable: // row 6
		obs = domain.ObsNotApplicable
	default:
		obs = domain.ObsError
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

// infoRaw stores the raw engine status; partialOK=false defensively maps an
// illegal partial to error (02 §7.4).
func infoRaw(st checker.CheckStatus, partialOK bool) domain.Observation {
	if st == checker.StatusPartial && !partialOK {
		slog.Warn("unexpected partial on informational dimension")
		return domain.ObsError
	}
	return domain.Observation(st)
}

func infoSMTP(st checker.CheckStatus) domain.Observation {
	if st == checker.StatusPartial {
		return domain.ObsUnsupported // a half-working EHLO does not accept mail
	}
	return domain.Observation(st)
}

func latencyMs(st checker.CheckStatus, d *checker.LatencyDetail) *int32 {
	if st != checker.StatusSupported || d.AvgMS == nil {
		return nil
	}
	ms := int32(*d.AvgMS)
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
		connKey:                     status(o.Conn),
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
