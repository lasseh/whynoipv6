package checker

import (
	"net/url"
	"testing"
)

// TestAddHostSchemes pins the scheme allowlist in hostSet.add. Only http and
// https can carry a resource; every other scheme (data:, javascript:,
// vbscript: and the rest) resolves to no host worth probing. Protocol-relative
// and root-relative refs inherit the base scheme, so they must still pass.
func TestAddHostSchemes(t *testing.T) {
	base, err := url.Parse("https://example.com/page")
	if err != nil {
		t.Fatalf("unparseable base: %v", err)
	}

	tests := []struct {
		name string
		raw  string
		want string // "" means the ref is discarded
	}{
		{"absolute_https", "https://cdn.example.net/app.js", "cdn.example.net"},
		{"absolute_http", "http://cdn.example.net/app.js", "cdn.example.net"},
		{"protocol_relative", "//cdn.example.net/app.js", "cdn.example.net"},
		{"uppercase_host_lowered", "https://CDN.Example.NET/app.js", "cdn.example.net"},
		{"leading_whitespace", "  https://cdn.example.net/app.js  ", "cdn.example.net"},

		{"data_uri_rejected", "data:text/css;base64,Zm9v", ""},
		{"javascript_uri_rejected", "javascript:alert(1)", ""},
		{"vbscript_uri_rejected", "vbscript:msgbox(1)", ""},
		{"mailto_rejected", "mailto:hi@example.net", ""},
		{"mixed_case_scheme_rejected", "JavaScript:alert(1)", ""},
		{"ftp_rejected", "ftp://cdn.example.net/app.js", ""},
		{"empty_rejected", "   ", ""},

		{"root_relative_is_same_domain", "/static/app.js", ""},
		{"subdomain_is_same_domain", "https://cdn.example.com/app.js", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			set := newHostSet()
			set.add(tc.raw, base, "example.com")
			hosts := set.hosts

			if tc.want == "" {
				if len(hosts) != 0 {
					t.Fatalf("add(%q) = %v, want no hosts", tc.raw, hosts)
				}
				return
			}
			if len(hosts) != 1 || hosts[0] != tc.want {
				t.Fatalf("add(%q) = %v, want [%s]", tc.raw, hosts, tc.want)
			}
		})
	}
}
