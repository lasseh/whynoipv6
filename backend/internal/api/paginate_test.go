package api

import (
	"errors"
	"net/url"
	"testing"
)

// TestCursor (P4.13; 10-testing keyset vectors): round-trips all three
// orderings, rejects fingerprint mismatches, re-anchors stale generations,
// and keeps the null-flag-first tail.
func TestCursor(t *testing.T) {
	const g = int32(20260707)
	fp := FilterFingerprint(url.Values{"class": {"hero"}})

	t.Run("rank_round_trip", func(t *testing.T) {
		tok := EncodeCursor(g, SortRank, fp, []any{int32(10023), int64(88142)})
		c, err := DecodeCursor(tok, SortRank, fp, g)
		if err != nil {
			t.Fatal(err)
		}
		s, err := c.SeekTuple()
		if err != nil {
			t.Fatal(err)
		}
		if s.Rank == nil || *s.Rank != 10023 || s.ID != 88142 || c.ReAnchored {
			t.Errorf("seek = %+v reanchored=%t", s, c.ReAnchored)
		}
	})

	t.Run("host_round_trip", func(t *testing.T) {
		tok := EncodeCursor(g, SortHost, fp, []any{"m.example"})
		c, err := DecodeCursor(tok, SortHost, fp, g)
		if err != nil {
			t.Fatal(err)
		}
		s, err := c.SeekTuple()
		if err != nil || s.Host != "m.example" {
			t.Errorf("seek = %+v err=%v", s, err)
		}
	})

	t.Run("dependents_round_trip", func(t *testing.T) {
		// Ranked row.
		tok := EncodeCursor(g, SortDependents, fp, []any{false, int32(500), int64(7)})
		c, _ := DecodeCursor(tok, SortDependents, fp, g)
		s, err := c.SeekTuple()
		if err != nil || s.RankNull || s.Rank == nil || *s.Rank != 500 || s.ID != 7 {
			t.Errorf("ranked seek = %+v err=%v", s, err)
		}
		// The rank-NULL tail is never dropped: the null flag carries the walk.
		tok = EncodeCursor(g, SortDependents, fp, []any{true, nil, int64(9001)})
		c, _ = DecodeCursor(tok, SortDependents, fp, g)
		s, err = c.SeekTuple()
		if err != nil || !s.RankNull || s.Rank != nil || s.ID != 9001 {
			t.Errorf("null-tail seek = %+v err=%v", s, err)
		}
	})

	t.Run("fingerprint_mismatch_rejected", func(t *testing.T) {
		tok := EncodeCursor(g, SortRank, fp, []any{int32(1), int64(1)})
		other := FilterFingerprint(url.Values{"class": {"sinner"}})
		if _, err := DecodeCursor(tok, SortRank, other, g); !errors.Is(err, ErrCursorInvalid) {
			t.Errorf("mismatched fingerprint: err = %v, want ErrCursorInvalid (400)", err)
		}
	})

	t.Run("sort_mismatch_rejected", func(t *testing.T) {
		tok := EncodeCursor(g, SortRank, fp, []any{int32(1), int64(1)})
		if _, err := DecodeCursor(tok, SortHost, fp, g); !errors.Is(err, ErrCursorInvalid) {
			t.Errorf("mismatched sort: err = %v", err)
		}
	})

	t.Run("stale_generation_reanchors", func(t *testing.T) {
		tok := EncodeCursor(20260706, SortRank, fp, []any{int32(4200), int64(555)})
		c, err := DecodeCursor(tok, SortRank, fp, g)
		if err != nil {
			t.Fatal(err)
		}
		if !c.ReAnchored {
			t.Fatal("stale cursor not marked re-anchored")
		}
		s, _ := c.SeekTuple()
		if s.Rank == nil || *s.Rank != 4200 || s.ID != 0 {
			t.Errorf("re-anchored seek = %+v, want rank 4200 with zeroed id", s)
		}
	})

	t.Run("garbage_rejected", func(t *testing.T) {
		if _, err := DecodeCursor("not-base64!!", SortRank, fp, g); !errors.Is(err, ErrCursorInvalid) {
			t.Errorf("garbage: %v", err)
		}
	})
}

// TestScopeGuardrail (07 §3.3): bare residuals and stacked residuals return
// scope-required; a scoped single residual passes.
func TestScopeGuardrail(t *testing.T) {
	cases := []struct {
		name string
		q    url.Values
		ok   bool
	}{
		{"no_residual", url.Values{"class": {"hero"}}, true},
		{"bare_flag", url.Values{"flag": {"broken_v6"}}, false},
		{"bare_mx", url.Values{"mx": {"unsupported"}}, false},
		{"bare_tld", url.Values{"tld": {"no"}}, false},
		{"scoped_flag", url.Values{"class": {"hero"}, "flag": {"broken_v6"}}, true},
		{"scoped_mx_country", url.Values{"country": {"no"}, "mx": {"unsupported"}}, true},
		{"scoped_provider_asn", url.Values{"asn": {"2119"}, "provider": {"3"}}, true},
		{"stacked_residuals", url.Values{"class": {"hero"}, "tld": {"com"}, "mx": {"unsupported"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateResiduals(tc.q, false)
			if tc.ok && err != nil {
				t.Errorf("unexpected: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrScopeRequired) {
				t.Errorf("want ErrScopeRequired (422), got %v", err)
			}
		})
	}
}

// TestAfterRank: available on rank orderings only (07 §3.2).
func TestAfterRank(t *testing.T) {
	q := url.Values{"after_rank": {"500000"}}
	rank, err := ParseAfterRank(q, SortRank)
	if err != nil || rank == nil || *rank != 500000 {
		t.Errorf("after_rank on rank sort = %v err=%v", rank, err)
	}
	if _, err := ParseAfterRank(q, SortHost); !errors.Is(err, ErrCursorInvalid) {
		t.Errorf("after_rank on host sort must be rejected: %v", err)
	}
}

// TestParseLimit: default 50, cap 200.
func TestParseLimit(t *testing.T) {
	if n, _ := ParseLimit(url.Values{}); n != DefaultLimit {
		t.Errorf("default = %d", n)
	}
	if n, _ := ParseLimit(url.Values{"limit": {"5000"}}); n != MaxLimit {
		t.Errorf("cap = %d", n)
	}
	if _, err := ParseLimit(url.Values{"limit": {"-3"}}); err == nil {
		t.Error("negative limit accepted")
	}
}
