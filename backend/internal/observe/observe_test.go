package observe

import (
	"maps"
	"testing"
	"time"

	"github.com/lasseh/whynoipv6/internal/checker"
	"github.com/lasseh/whynoipv6/internal/domain"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

var t0 = time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

// scan builds a synthetic ScanResult with every expected check present
// (the runner records every registered check), overridden per test.
func scan(overrides map[string]checker.Result) checker.ScanResult {
	results := map[string]checker.Result{}
	for _, name := range []string{
		"dns_aaaa_base", "dns_aaaa_www", "dns_ns_ipv6", "dns_mx_ipv6", "dns_dnssec",
		"http_ipv6", "https_ipv6", "tls_ipv6", "http_response_parity",
		"smtp_ipv6", "spf_ipv6", "dns_ptr_ipv6", "latency_ipv4", "latency_ipv6",
	} {
		results[name] = checker.Result{Status: checker.StatusNotApplicable}
	}
	maps.Copy(results, overrides)
	return checker.ScanResult{Domain: "t.example", Results: results}
}

func res(status checker.CheckStatus, d checker.Detail) checker.Result {
	return checker.Result{Status: status, Detail: d}
}

func aOut(status checker.CheckStatus, outcome string) checker.Result {
	return res(status, &checker.AAAADetail{AOutcome: outcome})
}

func mapOne(t *testing.T, kind domain.Kind, overrides map[string]checker.Result) Observations {
	t.Helper()
	return MapObservations(kind, scan(overrides), t0.Add(-time.Minute), t0, nil, true)
}

// TestMapObservations covers every row of the 10-testing §4.1–§4.4 tables.
func TestMapObservations(t *testing.T) {
	// §4.1 base composite vectors.
	baseRows := []struct {
		name string
		r    checker.Result
		want domain.Observation
	}{
		{"base_exists", res(checker.StatusSupported, nil), domain.ObsSupported},
		{"base_empty_apresent", aOut(checker.StatusUnsupported, "a_present"), domain.ObsUnsupported},
		{"base_empty_aabsent", aOut(checker.StatusUnsupported, "a_absent"), domain.ObsNoRecord},
		{"base_empty_aerror", aOut(checker.StatusUnsupported, "a_error"), domain.ObsError},
		{"base_nxdomain", res(checker.StatusNotApplicable, nil), domain.ObsNoRecord},
		{"base_error", res(checker.StatusError, nil), domain.ObsError},
		{"base_inconsistent", res(checker.StatusError, &checker.AAAADetail{Inconsistent: true}), domain.ObsInconsistent},
	}
	for _, tc := range baseRows {
		t.Run(tc.name, func(t *testing.T) {
			o := mapOne(t, domain.KindApex, map[string]checker.Result{"dns_aaaa_base": tc.r})
			if o.Base != tc.want {
				t.Errorf("base = %s, want %s", o.Base, tc.want)
			}
		})
	}
	t.Run("base_subdomain_self", func(t *testing.T) {
		o := mapOne(t, domain.KindSubdomain, map[string]checker.Result{
			"dns_aaaa_base": res(checker.StatusSupported, nil),
		})
		if o.Base != domain.ObsSupported {
			t.Errorf("subdomain base = %s, want supported", o.Base)
		}
	})

	// §4.2 www composite vectors.
	wwwRows := []struct {
		name string
		r    checker.Result
		want domain.Observation
	}{
		{"www_exists", res(checker.StatusSupported, nil), domain.ObsSupported},
		{"www_empty_apresent", aOut(checker.StatusUnsupported, "a_present"), domain.ObsUnsupported},
		{"www_empty_aabsent", aOut(checker.StatusUnsupported, "a_absent"), domain.ObsNotApplicable},
		{"www_empty_aerror", aOut(checker.StatusUnsupported, "a_error"), domain.ObsError},
		{"www_nxdomain", res(checker.StatusNotApplicable, nil), domain.ObsNotApplicable},
		{"www_error", res(checker.StatusError, nil), domain.ObsError},
		{"www_inconsistent", res(checker.StatusError, &checker.AAAADetail{Inconsistent: true}), domain.ObsInconsistent},
	}
	for _, tc := range wwwRows {
		t.Run(tc.name, func(t *testing.T) {
			o := mapOne(t, domain.KindApex, map[string]checker.Result{"dns_aaaa_www": tc.r})
			if o.WWW != tc.want {
				t.Errorf("www = %s, want %s", o.WWW, tc.want)
			}
		})
	}
	t.Run("www_subdomain_forced_na", func(t *testing.T) {
		o := mapOne(t, domain.KindSubdomain, map[string]checker.Result{
			"dns_aaaa_www": res(checker.StatusSupported, nil), // even a supported result is ignored
		})
		if o.WWW != domain.ObsNotApplicable {
			t.Errorf("subdomain www = %s, want not_applicable", o.WWW)
		}
	})

	// Property: www never yields no_record over the full cross-product.
	t.Run("www_never_no_record", func(t *testing.T) {
		statuses := []checker.CheckStatus{checker.StatusSupported, checker.StatusUnsupported,
			checker.StatusNotApplicable, checker.StatusError}
		outcomes := []string{"", "a_present", "a_absent", "a_error"}
		for _, st := range statuses {
			for _, ao := range outcomes {
				o := mapOne(t, domain.KindApex, map[string]checker.Result{"dns_aaaa_www": aOut(st, ao)})
				if o.WWW == domain.ObsNoRecord {
					t.Fatalf("www produced no_record for status=%s a_outcome=%s", st, ao)
				}
			}
		}
	})

	// §4.4 ns / mx / informational vectors.
	dimRows := []struct {
		name   string
		check  string
		status checker.CheckStatus
		get    func(Observations) domain.Observation
		want   domain.Observation
	}{
		{"ns_supported", "dns_ns_ipv6", checker.StatusSupported, func(o Observations) domain.Observation { return o.NS }, domain.ObsSupported},
		{"ns_partial_to_supported", "dns_ns_ipv6", checker.StatusPartial, func(o Observations) domain.Observation { return o.NS }, domain.ObsSupported},
		{"ns_unsupported", "dns_ns_ipv6", checker.StatusUnsupported, func(o Observations) domain.Observation { return o.NS }, domain.ObsUnsupported},
		{"ns_error", "dns_ns_ipv6", checker.StatusError, func(o Observations) domain.Observation { return o.NS }, domain.ObsError},
		{"ns_no_zone_defensive", "dns_ns_ipv6", checker.StatusNotApplicable, func(o Observations) domain.Observation { return o.NS }, domain.ObsError},
		{"mx_supported", "dns_mx_ipv6", checker.StatusSupported, func(o Observations) domain.Observation { return o.MX }, domain.ObsSupported},
		{"mx_partial_to_supported", "dns_mx_ipv6", checker.StatusPartial, func(o Observations) domain.Observation { return o.MX }, domain.ObsSupported},
		{"mx_unsupported", "dns_mx_ipv6", checker.StatusUnsupported, func(o Observations) domain.Observation { return o.MX }, domain.ObsUnsupported},
		{"mx_nullmx", "dns_mx_ipv6", checker.StatusNotApplicable, func(o Observations) domain.Observation { return o.MX }, domain.ObsNotApplicable},
		{"mx_error", "dns_mx_ipv6", checker.StatusError, func(o Observations) domain.Observation { return o.MX }, domain.ObsError},
		{"ptr_partial_verbatim", "dns_ptr_ipv6", checker.StatusPartial, func(o Observations) domain.Observation { return o.PTR }, domain.ObsPartial},
		{"parity_partial_verbatim", "http_response_parity", checker.StatusPartial, func(o Observations) domain.Observation { return o.Parity }, domain.ObsPartial},
		{"smtp_partial_to_unsupported", "smtp_ipv6", checker.StatusPartial, func(o Observations) domain.Observation { return o.SMTP }, domain.ObsUnsupported},
		{"dnssec_verbatim", "dns_dnssec", checker.StatusUnsupported, func(o Observations) domain.Observation { return o.DNSSEC }, domain.ObsUnsupported},
	}
	for _, tc := range dimRows {
		t.Run(tc.name, func(t *testing.T) {
			o := mapOne(t, domain.KindApex, map[string]checker.Result{tc.check: res(tc.status, nil)})
			if got := tc.get(o); got != tc.want {
				t.Errorf("%s = %s, want %s", tc.check, got, tc.want)
			}
		})
	}

	// Totality guard: partial appears only for ptr/parity (02 §8.6).
	t.Run("partial_only_ptr_parity", func(t *testing.T) {
		all := []string{"dns_aaaa_base", "dns_aaaa_www", "dns_ns_ipv6", "dns_mx_ipv6",
			"dns_dnssec", "https_ipv6", "http_ipv6", "smtp_ipv6"}
		for _, name := range all {
			o := mapOne(t, domain.KindApex, map[string]checker.Result{name: res(checker.StatusPartial, nil)})
			for dim, obs := range map[string]domain.Observation{
				"base": o.Base, "www": o.WWW, "ns": o.NS, "mx": o.MX,
				"conn": o.Conn, "resources": o.Resources, "dnssec": o.DNSSEC, "smtp": o.SMTP,
			} {
				if obs == domain.ObsPartial {
					t.Errorf("partial leaked into %s (via %s)", dim, name)
				}
			}
		}
	})

	// Latency hoist.
	t.Run("latency_avg_ms", func(t *testing.T) {
		o := mapOne(t, domain.KindApex, map[string]checker.Result{
			"latency_ipv6": res(checker.StatusSupported, &checker.LatencyDetail{AvgMS: ptr(int64(42))}),
			"latency_ipv4": res(checker.StatusError, nil),
		})
		if o.LatencyV6Ms == nil || *o.LatencyV6Ms != 42 {
			t.Errorf("LatencyV6Ms = %v, want 42", o.LatencyV6Ms)
		}
		if o.LatencyV4Ms != nil {
			t.Errorf("LatencyV4Ms = %v, want nil", o.LatencyV4Ms)
		}
	})
}

// TestConnComposition covers every 10-testing §4.3 row incl. the preflight
// guard downgrades.
func TestConnComposition(t *testing.T) {
	fresh := t0.Add(-time.Minute)
	stale := t0.Add(-6 * time.Minute)

	rows := []struct {
		name      string
		h         checker.Result
		p         checker.Result
		preflight time.Time
		want      domain.Observation
		source    string
		httpOnly  bool
	}{
		{"conn_https_ok", res(checker.StatusSupported, nil), res(checker.StatusError, nil), fresh, domain.ObsSupported, "https", false},
		{"conn_http_fallback", res(checker.StatusUnsupported, &checker.HTTPDetail{ErrorType: "connection_refused"}),
			res(checker.StatusSupported, nil), fresh, domain.ObsSupported, "http", true},
		{"conn_cert_error", res(checker.StatusUnsupported, &checker.HTTPDetail{ErrorType: "certificate_error"}),
			res(checker.StatusSupported, nil), fresh, domain.ObsUnsupported, "", false},
		{"conn_refused_no_http", res(checker.StatusUnsupported, &checker.HTTPDetail{ErrorType: "connection_refused"}),
			res(checker.StatusUnsupported, nil), fresh, domain.ObsUnsupported, "", false},
		{"conn_no_aaaa_on_host", res(checker.StatusUnsupported, nil),
			res(checker.StatusSupported, nil), fresh, domain.ObsUnsupported, "", false},
		{"conn_timeout_preflight_fresh", res(checker.StatusError, &checker.HTTPDetail{ErrorType: "timeout"}),
			res(checker.StatusSupported, nil), fresh, domain.ObsUnsupported, "", false},
		{"conn_timeout_preflight_stale", res(checker.StatusError, &checker.HTTPDetail{ErrorType: "timeout"}),
			res(checker.StatusSupported, nil), stale, domain.ObsError, "", false},
		{"conn_error_other", res(checker.StatusError, &checker.HTTPDetail{ErrorType: "unknown"}),
			res(checker.StatusSupported, nil), fresh, domain.ObsError, "", false},
		{"conn_phase2_skipped", res(checker.StatusNotApplicable, nil),
			res(checker.StatusNotApplicable, nil), fresh, domain.ObsNotApplicable, "", false},
		{"guard_cert_stale_downgrade", res(checker.StatusUnsupported, &checker.HTTPDetail{ErrorType: "certificate_error"}),
			res(checker.StatusSupported, nil), stale, domain.ObsError, "", false},
		{"guard_refused_stale_downgrade", res(checker.StatusUnsupported, &checker.HTTPDetail{ErrorType: "connection_refused"}),
			res(checker.StatusUnsupported, nil), stale, domain.ObsError, "", false},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			o := MapObservations(domain.KindApex, scan(map[string]checker.Result{
				"https_ipv6": tc.h, "http_ipv6": tc.p,
			}), tc.preflight, t0, nil, true)
			if o.Conn != tc.want {
				t.Fatalf("conn = %s, want %s", o.Conn, tc.want)
			}
			if tc.source != "" {
				if got, _ := o.ConnDetail["source"].(string); got != tc.source {
					t.Errorf("source = %v, want %s", o.ConnDetail["source"], tc.source)
				}
			} else if _, ok := o.ConnDetail["source"]; ok && o.Conn != domain.ObsSupported {
				t.Errorf("source present on non-supported outcome")
			}
			if got, _ := o.ConnDetail["http_only"].(bool); got != tc.httpOnly {
				t.Errorf("http_only = %v, want %t", o.ConnDetail["http_only"], tc.httpOnly)
			}
		})
	}
}

// TestResourcesRollup covers every 10-testing §4.5 branch.
func TestResourcesRollup(t *testing.T) {
	sup, uns, nr, na := domain.StatusSupported, domain.StatusUnsupported, domain.StatusNoRecord, domain.StatusNotApplicable
	link := func(s *domain.IPv6Status) LinkedResource { return LinkedResource{AAAAStatus: s} }
	p := func(s domain.IPv6Status) *domain.IPv6Status { return &s }

	rows := []struct {
		name  string
		conn  domain.Observation
		links []LinkedResource
		want  domain.Observation
	}{
		{"res_conn_error_defer", domain.ObsError, []LinkedResource{link(p(sup))}, domain.ObsError},
		{"res_conn_inconsistent_defer", domain.ObsInconsistent, []LinkedResource{link(p(sup))}, domain.ObsError},
		{"res_conn_unsupported_na", domain.ObsUnsupported, []LinkedResource{link(p(sup))}, domain.ObsNotApplicable},
		{"res_conn_notapplicable_na", domain.ObsNotApplicable, []LinkedResource{link(p(sup))}, domain.ObsNotApplicable},
		{"res_null_host_defer", domain.ObsSupported, []LinkedResource{link(p(sup)), link(nil)}, domain.ObsError},
		{"res_empty_after_prune_na", domain.ObsSupported, []LinkedResource{link(p(nr)), link(p(na))}, domain.ObsNotApplicable},
		{"res_no_links_na", domain.ObsSupported, nil, domain.ObsNotApplicable},
		{"res_any_unsupported", domain.ObsSupported, []LinkedResource{link(p(sup)), link(p(uns))}, domain.ObsUnsupported},
		{"res_all_supported", domain.ObsSupported, []LinkedResource{link(p(sup)), link(p(sup))}, domain.ObsSupported},
		{"res_dead_ref_excluded", domain.ObsSupported, []LinkedResource{link(p(sup)), link(p(nr))}, domain.ObsSupported},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			if got := rollupResources(tc.conn, tc.links); got != tc.want {
				t.Errorf("rollup = %s, want %s", got, tc.want)
			}
		})
	}

	t.Run("res_disabled_gate", func(t *testing.T) {
		o := MapObservations(domain.KindApex, scan(map[string]checker.Result{
			"https_ipv6": res(checker.StatusSupported, nil),
		}), t0.Add(-time.Minute), t0, []LinkedResource{link(p(uns))}, false)
		if o.Resources != domain.ObsNotApplicable || !o.ResourcesExcluded {
			t.Errorf("disabled gate = %s excluded=%t, want not_applicable/true", o.Resources, o.ResourcesExcluded)
		}
	})
}

// TestObsFromStatus pins the one CheckStatus→Observation value bridge:
// every engine status maps to its same-named observation; unknown → error.
func TestObsFromStatus(t *testing.T) {
	rows := []struct {
		st   checker.CheckStatus
		want domain.Observation
	}{
		{checker.StatusSupported, domain.ObsSupported},
		{checker.StatusPartial, domain.ObsPartial},
		{checker.StatusUnsupported, domain.ObsUnsupported},
		{checker.StatusNotApplicable, domain.ObsNotApplicable},
		{checker.StatusError, domain.ObsError},
		{checker.CheckStatus("bogus"), domain.ObsError},
	}
	for _, tc := range rows {
		if got := obsFromStatus(tc.st); got != tc.want {
			t.Errorf("obsFromStatus(%s) = %s, want %s", tc.st, got, tc.want)
		}
	}
}

// TestLinkSetConstructorsAgree pins the two LinkSet constructors to one
// convention: the same registry state, expressed as persisted statuses and
// as a discovered-host probe, must roll up identically — including the
// missing/unswept → nil → defer case.
func TestLinkSetConstructorsAgree(t *testing.T) {
	sup := db.Ipv6StatusSupported
	rows := []struct {
		name      string
		persisted []*db.Ipv6Status
		hosts     []string
		byHost    map[string]domain.IPv6Status
		want      domain.Observation
	}{
		{"all_known", []*db.Ipv6Status{&sup, &sup}, []string{"a", "b"},
			map[string]domain.IPv6Status{"a": domain.StatusSupported, "b": domain.StatusSupported},
			domain.ObsSupported},
		{"unswept_defers", []*db.Ipv6Status{&sup, nil}, []string{"a", "b"},
			map[string]domain.IPv6Status{"a": domain.StatusSupported},
			domain.ObsError},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			p := rollupResources(domain.ObsSupported, linksFromStatuses(tc.persisted))
			l := rollupResources(domain.ObsSupported, linksForHosts(tc.hosts, tc.byHost))
			if p != l || p != tc.want {
				t.Errorf("persisted=%s live=%s, want both %s", p, l, tc.want)
			}
		})
	}
}
