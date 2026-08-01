package api

import "testing"

// TestCSVSanitize pins the OWASP formula-injection neutralization: cells
// starting with =, +, -, @, tab or CR are prefixed with a single quote.
func TestCSVSanitize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "example.com", "example.com"},
		{"formula", `=HYPERLINK("http://evil")`, `'=HYPERLINK("http://evil")`},
		{"plus", "+1+1", "'+1+1"},
		{"minus", "-2+3", "'-2+3"},
		{"at", "@SUM(A1)", "'@SUM(A1)"},
		{"tab", "\t=1", "'\t=1"},
		{"cr", "\r=1", "'\r=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := csvSanitize(tt.in); got != tt.want {
				t.Errorf("csvSanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
