package postgres

import "testing"

// TestLikeSubstring: the substring term is wrapped in %…% with its own
// LIKE metacharacters escaped, so ?q=a_b cannot match "acb".
func TestLikeSubstring(t *testing.T) {
	cases := []struct{ in, want string }{
		{"example", "%example%"},
		{"a_b", `%a\_b%`},
		{"100%", `%100\%%`},
		{`back\slash`, `%back\\slash%`},
		{"", "%%"},
	}
	for _, tc := range cases {
		if got := likeSubstring(tc.in); got != tc.want {
			t.Errorf("likeSubstring(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
