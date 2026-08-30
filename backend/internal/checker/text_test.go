package checker

import "testing"

// TestSanitizeText pins the property the commit path depends on: no C0
// control byte survives into a Detail field. The NUL cases are the reason
// this function exists — jsonb rejects the escape json.Marshal writes for
// one (SQLSTATE 22P05) and fails the whole commit batch with it.
func TestSanitizeText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"clean ascii", "220 mx1.hero.no ESMTP", "220 mx1.hero.no ESMTP"},
		{"clean unicode", "220 café ✓", "220 café ✓"},
		{"empty", "", ""},
		// One banner mail.katia.sh served on 2026-08-30, the one that took
		// adsb.lol's commit down. The message text rotates per connection;
		// the NUL at offset 4 does not.
		{
			"adsb.lol banner",
			"220 \x00Rmail.katia.sh ESMTP i\\'ll watch the inbox 🐾",
			"220 Rmail.katia.sh ESMTP i\\'ll watch the inbox 🐾",
		},
		{"leading nul", "\x00220 mx.example", "220 mx.example"},
		{"trailing nul", "nginx\x00", "nginx"},
		{"only nuls", "\x00\x00", ""},
		{"del", "ngi\x7fnx", "nginx"},
		{"bell and escape", "\x07ngi\x1bnx", "nginx"},
		{"tab and newline go too", "a\tb\nc", "abc"},
		{"space survives", "text/html; charset=utf-8", "text/html; charset=utf-8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeText(tc.in); got != tc.want {
				t.Errorf("sanitizeText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSanitizeTexts checks the slice form copies rather than cleaning in
// place: callers hand it slices owned by parsed certificates and DNS
// messages.
func TestSanitizeTexts(t *testing.T) {
	if got := sanitizeTexts(nil); got != nil {
		t.Errorf("sanitizeTexts(nil) = %v, want nil", got)
	}

	in := []string{"hero.no", "www.hero.no\x00", "\x00evil.no"}
	got := sanitizeTexts(in)

	want := []string{"hero.no", "www.hero.no", "evil.no"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sanitizeTexts()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if in[1] != "www.hero.no\x00" {
		t.Errorf("input mutated: in[1] = %q", in[1])
	}
}
