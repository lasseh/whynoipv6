package checker

import (
	"encoding/json"
	"fmt"
	"time"
)

// Canonical check names — the keys of ScanResult.Results and the dispatch
// table for detail re-typing. Single-sourced here; each check's Name()
// returns its constant.
const (
	NameDNSAAAABase       = "dns_aaaa_base"
	NameDNSAAAAWWW        = "dns_aaaa_www"
	NameDNSNS             = "dns_ns_ipv6"
	NameDNSMX             = "dns_mx_ipv6"
	NameDNSSEC            = "dns_dnssec"
	NameHTTP              = "http_ipv6"
	NameHTTPS             = "https_ipv6"
	NameTLS               = "tls_ipv6"
	NameParity            = "http_response_parity"
	NameSMTP              = "smtp_ipv6"
	NameSPF               = "spf_ipv6"
	NamePTR               = "dns_ptr_ipv6"
	NameLatencyV4         = "latency_ipv4"
	NameLatencyV6         = "latency_ipv6"
	NameResourceDiscovery = "resource_discovery"
)

// Detail is the typed per-check payload on a Result — the "check detail"
// contract (see CONTEXT.md). Only the detail structs below satisfy it (via
// the embedded CommonDetail), so a raw map can never occupy Result.Detail
// again. The JSON field names are the scan_detail wire keys (03 §14.2) and
// must never drift.
type Detail interface {
	common() *CommonDetail
}

// CommonDetail carries the two cross-check keys every check (and the
// runner's synthesized skip/error results) may emit.
type CommonDetail struct {
	Error  string `json:"error,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func (c *CommonDetail) common() *CommonDetail { return c }

// AAAADetail is the payload of dns_aaaa_base and dns_aaaa_www: the consensus
// answer plus the conditional-A / CD-rescue outcomes (02 §2.7). Rcode is
// present on every real check result (may be ""); pointer fields mark keys
// that only some paths emit.
type AAAADetail struct {
	CommonDetail
	Rcode        string      `json:"rcode"`
	CNAMEChain   []string    `json:"cname_chain,omitempty"`
	CNAMETarget  string      `json:"cname_target,omitempty"` // www only
	CDNDetected  bool        `json:"cdn_detected,omitempty"` // www only
	Quorum       *QuorumInfo `json:"quorum,omitempty"`
	AOutcome     string      `json:"a_outcome,omitempty"`
	AAddress     string      `json:"a_address,omitempty"` // base only (06 §6.2)
	CDOutcome    string      `json:"cd_outcome,omitempty"`
	Inconsistent bool        `json:"inconsistent,omitempty"`
	Addresses    []string    `json:"addresses,omitempty"`
	TTL          *int        `json:"ttl,omitempty"` // present iff addresses are
}

// NSHost is one nameserver's AAAA evidence inside NSDetail.
type NSHost struct {
	HasIPv6   bool     `json:"has_ipv6"`
	Addresses []string `json:"addresses"`
}

// NSDetail is the dns_ns_ipv6 payload: the walked-up zone and the capped
// per-nameserver AAAA sample.
type NSDetail struct {
	CommonDetail
	Zone        string            `json:"zone,omitempty"`
	Nameservers map[string]NSHost `json:"nameservers,omitempty"`
	Total       int               `json:"total,omitempty"`
	Checked     int               `json:"checked,omitempty"`
	IPv6Count   *int              `json:"ipv6_count,omitempty"` // 0 is meaningful
}

// MXHost is one MX target's AAAA evidence inside MXDetail.
type MXHost struct {
	Preference uint16   `json:"preference"`
	HasIPv6    bool     `json:"has_ipv6"`
	Addresses  []string `json:"addresses"`
}

// MXDetail is the dns_mx_ipv6 payload; Addresses carries the implicit-MX
// fallback answer (RFC 5321 §5.1).
type MXDetail struct {
	CommonDetail
	Addresses []string          `json:"addresses,omitempty"`
	MXRecords map[string]MXHost `json:"mx_records,omitempty"`
	Total     int               `json:"total,omitempty"`
	IPv6Count *int              `json:"ipv6_count,omitempty"` // 0 is meaningful
}

// DSRecord is one DS record's identity inside DNSSECDetail.
type DSRecord struct {
	KeyTag     uint16 `json:"key_tag"`
	Algorithm  string `json:"algorithm"`
	DigestType uint8  `json:"digest_type"`
}

// DNSSECDetail is the dns_dnssec payload; Signed is present on every path.
type DNSSECDetail struct {
	CommonDetail
	Signed        bool       `json:"signed"`
	DSRecords     []DSRecord `json:"ds_records,omitempty"`
	ChainComplete *bool      `json:"chain_complete,omitempty"`
	ADFlag        *bool      `json:"ad_flag,omitempty"`
}

// HTTPDetail is the payload of http_ipv6 and https_ipv6: the terminal
// error classification or the successful response summary.
type HTTPDetail struct {
	CommonDetail
	ErrorType      string `json:"error_type,omitempty"`
	Address        string `json:"address,omitempty"`
	StatusCode     int    `json:"status_code,omitempty"`
	ResponseTimeMS *int64 `json:"response_time_ms,omitempty"`
	Server         string `json:"server,omitempty"`
	TLSVersion     string `json:"tls_version,omitempty"` // https only
}

// TLSDetail is the tls_ipv6 payload: handshake outcome and leaf-certificate
// summary.
type TLSDetail struct {
	CommonDetail
	Address       string   `json:"address,omitempty"`
	TLSVersion    string   `json:"tls_version,omitempty"`
	CipherSuite   string   `json:"cipher_suite,omitempty"`
	Valid         *bool    `json:"valid,omitempty"`
	Issuer        string   `json:"issuer,omitempty"`
	Subject       string   `json:"subject,omitempty"`
	SAN           []string `json:"san,omitempty"`
	NotBefore     string   `json:"not_before,omitempty"`
	NotAfter      string   `json:"not_after,omitempty"`
	ExpiresInDays *int     `json:"expires_in_days,omitempty"`
	ExpiresSoon   *bool    `json:"expires_soon,omitempty"`
}

// ParityFetch is one protocol's response summary inside ParityDetail.
type ParityFetch struct {
	Address        string `json:"address"`
	StatusCode     int    `json:"status_code"`
	ContentType    string `json:"content_type"`
	ContentLength  int64  `json:"content_length"`
	ResponseTimeMS int64  `json:"response_time_ms"`
}

// ParityDetail is the http_response_parity payload: both fetches and the
// three comparison verdicts.
type ParityDetail struct {
	CommonDetail
	IPv4                 *ParityFetch `json:"ipv4,omitempty"`
	IPv6                 *ParityFetch `json:"ipv6,omitempty"`
	StatusMatch          *bool        `json:"status_match,omitempty"`
	ContentTypeMatch     *bool        `json:"content_type_match,omitempty"`
	ContentLengthDiffPct *float64     `json:"content_length_diff_pct,omitempty"`
}

// SMTPDetail is the smtp_ipv6 payload: the dialogue with the first
// reachable MX over IPv6.
type SMTPDetail struct {
	CommonDetail
	MXHost          string  `json:"mx_host,omitempty"`
	MXPreference    *uint16 `json:"mx_preference,omitempty"`
	Address         string  `json:"address,omitempty"`
	Banner          string  `json:"banner,omitempty"`
	EHLOResponse    string  `json:"ehlo_response,omitempty"`
	STARTTLSOffered *bool   `json:"starttls_offered,omitempty"`
}

// SPFDetail is the spf_ipv6 payload: the record and the mechanism
// evaluation. The two omitzero slices are present-but-empty on the
// evaluated path (a non-nil empty slice serializes as []).
type SPFDetail struct {
	CommonDetail
	SPFRecord       string   `json:"spf_record,omitempty"`
	HasIP6Mechanism *bool    `json:"has_ip6_mechanism,omitempty"`
	IP6Mechanisms   []string `json:"ip6_mechanisms,omitzero"`
	IncludeHasIP6   *bool    `json:"include_has_ip6,omitempty"`
	IncludeChain    []string `json:"include_chain,omitzero"`
	LookupCount     *int     `json:"lookup_count,omitempty"`
	Implicit        bool     `json:"implicit,omitempty"`
}

// PTRCheck is one address's reverse-DNS evidence inside PTRDetail.
type PTRCheck struct {
	Address          string `json:"address"`
	PTRName          string `json:"ptr_name"`
	ForwardConfirmed bool   `json:"forward_confirmed"`
}

// PTRDetail is the dns_ptr_ipv6 payload.
type PTRDetail struct {
	CommonDetail
	Checks       []PTRCheck `json:"checks,omitempty"`
	AllConfirmed *bool      `json:"all_confirmed,omitempty"`
}

// LatencyDetail is the payload of latency_ipv4 and latency_ipv6.
type LatencyDetail struct {
	CommonDetail
	Address      string  `json:"address,omitempty"`
	TTFBMS       *int64  `json:"ttfb_ms,omitempty"`
	Measurements []int64 `json:"measurements,omitempty"`
	AvgMS        *int64  `json:"avg_ms,omitempty"`
}

// ResourceDiscoveryDetail is the resource_discovery payload; Hosts is
// present-but-empty when discovery succeeded with no external dependencies.
type ResourceDiscoveryDetail struct {
	CommonDetail
	Hosts      []string `json:"hosts,omitzero"`
	TotalHosts *int     `json:"total_hosts,omitempty"`
}

// newDetail returns the empty detail struct for a check name — the
// UnmarshalJSON re-typing dispatch. Unknown names fall back to the bare
// CommonDetail. The name↔type pairing lives here AND in each accessor's
// type argument below; detailOf reports drift between the two as a missing
// detail (ok=false) instead of a silent zero value.
func newDetail(name string) Detail {
	switch name {
	case NameDNSAAAABase, NameDNSAAAAWWW:
		return &AAAADetail{}
	case NameDNSNS:
		return &NSDetail{}
	case NameDNSMX:
		return &MXDetail{}
	case NameDNSSEC:
		return &DNSSECDetail{}
	case NameHTTP, NameHTTPS:
		return &HTTPDetail{}
	case NameTLS:
		return &TLSDetail{}
	case NameParity:
		return &ParityDetail{}
	case NameSMTP:
		return &SMTPDetail{}
	case NameSPF:
		return &SPFDetail{}
	case NamePTR:
		return &PTRDetail{}
	case NameLatencyV4, NameLatencyV6:
		return &LatencyDetail{}
	case NameResourceDiscovery:
		return &ResourceDiscoveryDetail{}
	default:
		return &CommonDetail{}
	}
}

// UnmarshalJSON re-types each check's details by name, so a ScanResult
// loaded from a stored scan_detail payload carries the same typed details
// as a fresh engine run — the accessors below behave identically on both.
// Extra envelope keys (the conn/consensus hoists) are ignored.
func (sr *ScanResult) UnmarshalJSON(b []byte) error {
	var raw struct {
		Domain    string                     `json:"domain"`
		Results   map[string]json.RawMessage `json:"results"`
		ScannedAt time.Time                  `json:"scanned_at"`
		Duration  time.Duration              `json:"duration"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	sr.Domain = raw.Domain
	sr.ScannedAt = raw.ScannedAt
	sr.Duration = raw.Duration
	sr.Results = make(map[string]Result, len(raw.Results))

	for name, rr := range raw.Results {
		var r struct {
			Status  CheckStatus     `json:"status"`
			Details json.RawMessage `json:"details"`
			Latency time.Duration   `json:"latency"`
		}
		if err := json.Unmarshal(rr, &r); err != nil {
			return fmt.Errorf("check %s: %w", name, err)
		}
		res := Result{Status: r.Status, Latency: r.Latency}
		if len(r.Details) > 0 && string(r.Details) != "null" {
			d := newDetail(name)
			if err := json.Unmarshal(r.Details, d); err != nil {
				return fmt.Errorf("check %s details: %w", name, err)
			}
			res.Detail = d
		}
		sr.Results[name] = res
	}
	return nil
}

// detailOf pairs a check name with its detail type for the accessors. A
// missing check reports (StatusError, zero, false) — the §7.3 rule-7
// defensive default. A runner-synthesized result (skip/panic/cancel)
// carries a bare *CommonDetail; it is folded into the check's own struct so
// callers see one type on every path. Any OTHER concrete type means the
// accessor's type argument and newDetail's dispatch have drifted — reported
// as ok=false, never a silent zero detail.
func detailOf[D any](sr ScanResult, name string) (CheckStatus, D, bool) {
	var zero D
	r, ok := sr.Results[name]
	if !ok {
		return StatusError, zero, false
	}
	if r.Detail == nil {
		return r.Status, zero, true
	}
	if d, ok := any(r.Detail).(*D); ok {
		return r.Status, *d, true
	}
	if cd, ok := any(r.Detail).(*CommonDetail); ok {
		if dd, ok := any(&zero).(Detail); ok {
			*dd.common() = *cd
		}
		return r.Status, zero, true
	}
	return r.Status, zero, false
}

// AAAABase returns the dns_aaaa_base result.
func (sr ScanResult) AAAABase() (CheckStatus, AAAADetail, bool) {
	return detailOf[AAAADetail](sr, NameDNSAAAABase)
}

// AAAAWWW returns the dns_aaaa_www result.
func (sr ScanResult) AAAAWWW() (CheckStatus, AAAADetail, bool) {
	return detailOf[AAAADetail](sr, NameDNSAAAAWWW)
}

// NS returns the dns_ns_ipv6 result.
func (sr ScanResult) NS() (CheckStatus, NSDetail, bool) {
	return detailOf[NSDetail](sr, NameDNSNS)
}

// MX returns the dns_mx_ipv6 result.
func (sr ScanResult) MX() (CheckStatus, MXDetail, bool) {
	return detailOf[MXDetail](sr, NameDNSMX)
}

// DNSSEC returns the dns_dnssec result.
func (sr ScanResult) DNSSEC() (CheckStatus, DNSSECDetail, bool) {
	return detailOf[DNSSECDetail](sr, NameDNSSEC)
}

// HTTP returns the http_ipv6 result.
func (sr ScanResult) HTTP() (CheckStatus, HTTPDetail, bool) {
	return detailOf[HTTPDetail](sr, NameHTTP)
}

// HTTPS returns the https_ipv6 result.
func (sr ScanResult) HTTPS() (CheckStatus, HTTPDetail, bool) {
	return detailOf[HTTPDetail](sr, NameHTTPS)
}

// TLS returns the tls_ipv6 result.
func (sr ScanResult) TLS() (CheckStatus, TLSDetail, bool) {
	return detailOf[TLSDetail](sr, NameTLS)
}

// Parity returns the http_response_parity result.
func (sr ScanResult) Parity() (CheckStatus, ParityDetail, bool) {
	return detailOf[ParityDetail](sr, NameParity)
}

// SMTP returns the smtp_ipv6 result.
func (sr ScanResult) SMTP() (CheckStatus, SMTPDetail, bool) {
	return detailOf[SMTPDetail](sr, NameSMTP)
}

// SPF returns the spf_ipv6 result.
func (sr ScanResult) SPF() (CheckStatus, SPFDetail, bool) {
	return detailOf[SPFDetail](sr, NameSPF)
}

// PTR returns the dns_ptr_ipv6 result.
func (sr ScanResult) PTR() (CheckStatus, PTRDetail, bool) {
	return detailOf[PTRDetail](sr, NamePTR)
}

// LatencyV4 returns the latency_ipv4 result.
func (sr ScanResult) LatencyV4() (CheckStatus, LatencyDetail, bool) {
	return detailOf[LatencyDetail](sr, NameLatencyV4)
}

// LatencyV6 returns the latency_ipv6 result.
func (sr ScanResult) LatencyV6() (CheckStatus, LatencyDetail, bool) {
	return detailOf[LatencyDetail](sr, NameLatencyV6)
}

// ResourceDiscovery returns the resource_discovery result.
func (sr ScanResult) ResourceDiscovery() (CheckStatus, ResourceDiscoveryDetail, bool) {
	return detailOf[ResourceDiscoveryDetail](sr, NameResourceDiscovery)
}
