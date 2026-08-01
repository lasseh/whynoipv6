package ingest

import "testing"

func testMapping() *ProviderMapping {
	return &ProviderMapping{suffixes: map[string]int64{
		"cloudflare.com":    1,
		"awsdns.com":        2,
		"ns.cloudflare.com": 3, // longer, more specific
		"azure-dns.com":     4,
	}}
}

// TestProviderMapping covers 06-ingest §6.10: longest-suffix precedence,
// label-boundary matching, unknown → nil, and multi-NS resolution.
func TestProviderMapping(t *testing.T) {
	m := testMapping()

	t.Run("longest_suffix_precedence", func(t *testing.T) {
		id, _, ok := m.ProviderForNSHost("gina.ns.cloudflare.com")
		if !ok || id != 3 {
			t.Errorf("ProviderForNSHost = %d/%t, want 3 (longest suffix wins)", id, ok)
		}
	})

	t.Run("label_boundary", func(t *testing.T) {
		// notcloudflare.com must NOT match cloudflare.com.
		if _, _, ok := m.ProviderForNSHost("ns1.notcloudflare.com"); ok {
			t.Error("substring match across a label boundary must not resolve")
		}
		// Exact-equality match does.
		if id, _, ok := m.ProviderForNSHost("cloudflare.com"); !ok || id != 1 {
			t.Errorf("exact suffix = %d/%t, want 1", id, ok)
		}
	})

	t.Run("wire_form_fqdn", func(t *testing.T) {
		// The NS check stores nameservers as served: FQDN with the root dot,
		// case unfolded. Both must still resolve, or the whole mapping is
		// inert against real scan data.
		id, _, ok := m.ProviderForNSHost("gina.ns.cloudflare.com.")
		if !ok || id != 3 {
			t.Errorf("trailing-dot FQDN = %d/%t, want 3", id, ok)
		}
		if id, _, ok := m.ProviderForNSHost("GINA.NS.CloudFlare.COM."); !ok || id != 3 {
			t.Errorf("mixed-case FQDN = %d/%t, want 3", id, ok)
		}
		if got := m.ProviderForNSSet([]string{"ns1.awsdns.com.", "ns2.awsdns.com."}); got == nil || *got != 2 {
			t.Errorf("FQDN NS set = %v, want 2", got)
		}
	})

	t.Run("unknown_ns", func(t *testing.T) {
		if got := m.ProviderForNSSet([]string{"ns1.example.org", "ns2.example.org"}); got != nil {
			t.Errorf("unknown NS set = %v, want nil", got)
		}
		if got := m.ProviderForNSSet(nil); got != nil {
			t.Errorf("empty NS set = %v, want nil", got)
		}
	})

	t.Run("multi_ns_agreement", func(t *testing.T) {
		got := m.ProviderForNSSet([]string{"a.awsdns.com", "b.awsdns.com"})
		if got == nil || *got != 2 {
			t.Errorf("agreeing NS set = %v, want 2", got)
		}
	})

	t.Run("multi_ns_disagreement_longest_wins", func(t *testing.T) {
		got := m.ProviderForNSSet([]string{"a.awsdns.com", "gina.ns.cloudflare.com"})
		if got == nil || *got != 3 {
			t.Errorf("disagreeing NS set = %v, want 3 (longest match across all)", got)
		}
	})
}

// TestHostingTag covers 06-ingest §6.10's hosting/CDN derivation (P1.14).
func TestHostingTag(t *testing.T) {
	t.Run("cname_cdn", func(t *testing.T) {
		got := NormalizeHosting(true, []string{"www.example.com", "dualstack.x.cloudfront.net."}, 0)
		if got == nil || *got != "cloudfront" {
			t.Errorf("CDN chain = %v, want cloudfront", got)
		}
	})
	t.Run("asn_fallback", func(t *testing.T) {
		got := NormalizeHosting(false, nil, 24940)
		if got == nil || *got != "hetzner" {
			t.Errorf("ASN 24940 = %v, want hetzner", got)
		}
	})
	t.Run("cdn_beats_asn", func(t *testing.T) {
		got := NormalizeHosting(true, []string{"x.fastly.net"}, 24940)
		if got == nil || *got != "fastly" {
			t.Errorf("CDN+ASN = %v, want fastly (CNAME wins)", got)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		if got := NormalizeHosting(false, nil, 64512); got != nil {
			t.Errorf("unknown ASN = %v, want nil", got)
		}
		if got := NormalizeHosting(true, []string{"cdn.example-cdn.io"}, 0); got != nil {
			t.Errorf("unmatched CDN chain = %v, want nil", got)
		}
	})
}
