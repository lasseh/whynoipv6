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

	o.Base = mapAAAA(result(sr, "dns_aaaa_base"), false)
	if kind == domain.KindSubdomain {
		o.WWW = domain.ObsNotApplicable
	} else {
		o.WWW = mapAAAA(result(sr, "dns_aaaa_www"), true)
	}
	o.NS = mapNS(result(sr, "dns_ns_ipv6"))
	o.MX = mapMX(result(sr, "dns_mx_ipv6"))
	o.Conn, o.ConnDetail = composeConn(result(sr, "https_ipv6"), result(sr, "http_ipv6"), preflightPassedAt, now)

	if !resourcesEnabled {
		o.Resources = domain.ObsNotApplicable
		o.ResourcesExcluded = true
	} else {
		o.Resources = rollupResources(o.Conn, links)
	}

	// Informational dimensions (02 §7.4).
	o.DNSSEC = infoRaw(result(sr, "dns_dnssec"), false)
	o.PTR = infoRaw(result(sr, "dns_ptr_ipv6"), true)
	o.SMTP = infoSMTP(result(sr, "smtp_ipv6"))
	o.Parity = infoRaw(result(sr, "http_response_parity"), true)
	o.LatencyV4Ms = latencyMs(result(sr, "latency_ipv4"))
	o.LatencyV6Ms = latencyMs(result(sr, "latency_ipv6"))

	return o
}

// result reads one check with the §7.3 rule-7 defensive fallback.
func result(sr checker.ScanResult, name string) checker.Result {
	if r, ok := sr.Results[name]; ok {
		return r
	}
	slog.Error("check result missing", "check", name, "domain", sr.Domain)
	return checker.Result{Status: checker.StatusError}
}

// mapAAAA implements the §4 base/www composite tables over the §3 Result
// contract. www=true applies the two www substitutions (nxdomain→n/a,
// a_absent→n/a). The CD=1 rescue is transparent (reshaped upstream).
func mapAAAA(r checker.Result, www bool) domain.Observation {
	switch r.Status {
	case checker.StatusError:
		if inconsistent, _ := r.Details["inconsistent"].(bool); inconsistent {
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
		switch outcome, _ := r.Details["a_outcome"].(string); outcome {
		case "a_present":
			return domain.ObsUnsupported
		case "a_absent":
			if www {
				return domain.ObsNotApplicable
			}
			return domain.ObsNoRecord
		case "a_error":
			return domain.ObsError
		default:
			slog.Warn("a_outcome missing", "check_status", r.Status)
			return domain.ObsError
		}
	case checker.StatusPartial:
		// Unreachable: the AAAA checks never emit partial. Non-definitive.
		return domain.ObsError
	default:
		return domain.ObsError
	}
}

func mapNS(r checker.Result) domain.Observation {
	switch r.Status {
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

func mapMX(r checker.Result) domain.Observation {
	switch r.Status {
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
func composeConn(h, p checker.Result, preflightPassedAt, now time.Time) (domain.Observation, map[string]any) {
	preflightFresh := !preflightPassedAt.IsZero() && now.Sub(preflightPassedAt) <= preflightFreshness
	errType, _ := h.Details["error_type"].(string)

	var obs domain.Observation
	detail := map[string]any{"http_only": false}

	switch {
	case h.Status == checker.StatusSupported: // row 1
		obs = domain.ObsSupported
		detail["source"] = "https"
	case h.Status == checker.StatusUnsupported && errType == "connection_refused" && p.Status == checker.StatusSupported: // row 2
		obs = domain.ObsSupported
		detail["source"] = "http"
		detail["http_only"] = true
	case h.Status == checker.StatusUnsupported: // rows 3–4 (cert error, refused w/o http, no-AAAA)
		obs = domain.ObsUnsupported
	case h.Status == checker.StatusError && errType == "timeout" && preflightFresh: // row 5a
		obs = domain.ObsUnsupported
	case h.Status == checker.StatusError: // rows 5b–5c
		obs = domain.ObsError
	case h.Status == checker.StatusNotApplicable: // row 6
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
func infoRaw(r checker.Result, partialOK bool) domain.Observation {
	if r.Status == checker.StatusPartial && !partialOK {
		slog.Warn("unexpected partial on informational dimension")
		return domain.ObsError
	}
	return domain.Observation(r.Status)
}

func infoSMTP(r checker.Result) domain.Observation {
	if r.Status == checker.StatusPartial {
		return domain.ObsUnsupported // a half-working EHLO does not accept mail
	}
	return domain.Observation(r.Status)
}

func latencyMs(r checker.Result) *int32 {
	if r.Status != checker.StatusSupported {
		return nil
	}
	switch v := r.Details["avg_ms"].(type) {
	case int64:
		ms := int32(v)
		return &ms
	case float64:
		ms := int32(v)
		return &ms
	case int:
		ms := int32(v)
		return &ms
	default:
		return nil
	}
}

// QuorumHoist builds the details["consensus"] payload object (02 §7.5).
func QuorumHoist(sr checker.ScanResult) map[string]any {
	out := map[string]any{}
	if q, ok := result(sr, "dns_aaaa_base").Details["quorum"]; ok {
		out["base"] = q
	}
	if r, ok := sr.Results["dns_aaaa_www"]; ok {
		if q, ok := r.Details["quorum"]; ok {
			out["www"] = q
		}
	}
	return out
}
