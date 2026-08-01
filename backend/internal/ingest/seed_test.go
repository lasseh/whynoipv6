package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// seedPath is the checked-in dns_provider reference data loaded by
// `v6ctl provider seed` (06-ingest.md §6.11).
const seedRelPath = "db/seed/dns_provider.yaml"

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test dir")
		}
		dir = parent
	}
}

func loadSeed(t *testing.T) []providerSeedEntry {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), seedRelPath))
	if err != nil {
		t.Fatal(err)
	}
	var entries []providerSeedEntry
	if err := yaml.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("seed file does not parse as the provider-add shape: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("seed file is empty")
	}
	return entries
}

// TestProviderSeedShape guards the checked-in seed against the normalization
// ProviderAdd would otherwise have to repair: suffixes are stored lowercase,
// dot-free at both ends, and each one belongs to exactly one provider (the
// table is keyed by suffix, so a duplicate silently reassigns it).
func TestProviderSeedShape(t *testing.T) {
	entries := loadSeed(t)

	names := map[string]bool{}
	owner := map[string]string{}
	for _, e := range entries {
		if e.Name == "" {
			t.Error("entry with an empty name")
		}
		if names[e.Name] {
			t.Errorf("duplicate provider name %q", e.Name)
		}
		names[e.Name] = true
		if len(e.Suffixes) == 0 {
			t.Errorf("provider %q has no suffixes", e.Name)
		}
		for _, s := range e.Suffixes {
			switch {
			case s != strings.ToLower(s):
				t.Errorf("%s: suffix %q is not lowercase", e.Name, s)
			case strings.HasPrefix(s, "."), strings.HasSuffix(s, "."):
				t.Errorf("%s: suffix %q has a leading/trailing dot", e.Name, s)
			case !strings.Contains(s, "."):
				t.Errorf("%s: suffix %q is a bare label", e.Name, s)
			}
			if prev, dup := owner[s]; dup {
				t.Errorf("suffix %q claimed by both %q and %q", s, prev, e.Name)
			}
			owner[s] = e.Name
		}
	}
}

// TestProviderSeedResolves is the end-to-end check that the seed and the
// matcher agree: real nameserver hosts, in the wire form the NS check stores
// them (trailing root dot), must resolve to the intended provider.
func TestProviderSeedResolves(t *testing.T) {
	entries := loadSeed(t)

	m := &ProviderMapping{suffixes: map[string]int64{}}
	nameByID := map[int64]string{}
	for i, e := range entries {
		id := int64(i + 1)
		nameByID[id] = e.Name
		for _, s := range e.Suffixes {
			m.suffixes[s] = id
		}
	}

	cases := []struct {
		nsSet []string
		want  string // "" = must not resolve
	}{
		{[]string{"anna.ns.cloudflare.com.", "bob.ns.cloudflare.com."}, "Cloudflare"},
		{[]string{"ns-1234.awsdns-56.org.", "ns-2048.awsdns-07.co.uk."}, "Amazon Route 53"},
		{[]string{"ns-cloud-a1.googledomains.com."}, "Google Cloud DNS"},
		{[]string{"ns1-01.azure-dns.com.", "ns4-01.azure-dns.info."}, "Microsoft Azure DNS"},
		{[]string{"dns1.p01.nsone.net."}, "NS1"},
		{[]string{"hydrogen.ns.hetzner.com."}, "Hetzner"},
		{[]string{"ns01.domaincontrol.com."}, "GoDaddy"},
		{[]string{"ns1.hyp.net."}, "Domeneshop"},
		{[]string{"a1-64.akam.net."}, "Akamai Edge DNS"},
		// Self-hosted / unlisted operators stay unattributed rather than
		// falling back to a near-miss.
		{[]string{"ns1.example.org.", "ns2.example.org."}, ""},
		{[]string{"ns1.notcloudflare.com."}, ""},
	}
	for _, tc := range cases {
		got := m.ProviderForNSSet(tc.nsSet)
		if tc.want == "" {
			if got != nil {
				t.Errorf("%v resolved to %q, want no match", tc.nsSet, nameByID[*got])
			}
			continue
		}
		if got == nil {
			t.Errorf("%v did not resolve, want %q", tc.nsSet, tc.want)
			continue
		}
		if name := nameByID[*got]; name != tc.want {
			t.Errorf("%v resolved to %q, want %q", tc.nsSet, name, tc.want)
		}
	}
}
