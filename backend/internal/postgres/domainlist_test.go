package postgres

import (
	"strings"
	"testing"
)

// TestValidateLiterals pins the literal-interpolation guard: buildDomainList
// emits Class/StatusDim/StatusVal as raw SQL literals, so the builder must
// reject anything outside the closed sets regardless of caller validation.
func TestValidateLiterals(t *testing.T) {
	tests := []struct {
		name    string
		f       DomainListFilter
		wantErr bool
	}{
		{"empty", DomainListFilter{}, false},
		{"valid class", DomainListFilter{Class: "hero"}, false},
		{"valid flag", DomainListFilter{Flag: "broken_v6"}, false},
		{"valid status", DomainListFilter{StatusDim: "conn", StatusVal: "not_applicable"}, false},
		{"class injection", DomainListFilter{Class: "hero' OR '1'='1"}, true},
		{"flag unknown", DomainListFilter{Flag: "x') OR TRUE --"}, true},
		{"dim injection", DomainListFilter{StatusDim: "base_status = 'x' OR 1=1; --", StatusVal: "supported"}, true},
		{"status injection", DomainListFilter{StatusDim: "www", StatusVal: "supported' --"}, true},
		{"dim without value", DomainListFilter{StatusDim: "mx"}, true},
		{"value without dim", DomainListFilter{StatusVal: "supported"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.f.validateLiterals()
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateLiterals() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestNullFlagWalk pins the null-flag-first keyset walk (07 §3.2) to the
// expression list idx_domain_dependents_order carries,
// ((rank IS NULL), COALESCE(rank, 0), id). The ?q= search sort and
// ListDependents are one shape with two cursor keys and share this one
// spelling; a change to either half the index does not carry degrades both
// walks from an index scan to a sort.
func TestNullFlagWalk(t *testing.T) {
	rank := int32(42)
	tests := []struct {
		name      string
		rankNull  bool
		rank      *int32
		backward  bool
		wantSeek  string
		wantOrder string
	}{
		{
			name: "forward ranked", rank: &rank,
			wantSeek:  "((d.rank IS NULL), COALESCE(d.rank, 0), d.id) > (false, 42, 7)",
			wantOrder: "(d.rank IS NULL), COALESCE(d.rank, 0), d.id ASC",
		},
		{
			name: "forward null rank", rankNull: true,
			wantSeek:  "((d.rank IS NULL), COALESCE(d.rank, 0), d.id) > (true, 0, 7)",
			wantOrder: "(d.rank IS NULL), COALESCE(d.rank, 0), d.id ASC",
		},
		{
			name: "backward ranked", rank: &rank, backward: true,
			wantSeek:  "((d.rank IS NULL), COALESCE(d.rank, 0), d.id) < (false, 42, 7)",
			wantOrder: "(d.rank IS NULL) DESC, COALESCE(d.rank, 0) DESC, d.id DESC",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := nullFlagSeekExpr(tt.rankNull, tt.rank, 7, tt.backward).ToSql()
			if err != nil {
				t.Fatalf("seek expr: %v", err)
			}
			if got != tt.wantSeek {
				t.Errorf("seek = %q, want %q", got, tt.wantSeek)
			}
			if order := strings.Join(nullFlagOrder(tt.backward), ", "); order != tt.wantOrder {
				t.Errorf("order = %q, want %q", order, tt.wantOrder)
			}
		})
	}

	// The search arm must reach the walk through the same helpers, not a
	// second copy of the predicate.
	t.Run("search sort walks the shared key", func(t *testing.T) {
		sql, _, err := buildDomainList(&DomainListFilter{Query: "ex"}, ListSortSearch,
			&DomainSeek{Rank: &rank, ID: 7}, nil, false).ToSql()
		if err != nil {
			t.Fatalf("buildDomainList: %v", err)
		}
		if !strings.Contains(sql, tests[0].wantSeek) {
			t.Errorf("search sort seek missing from %q", sql)
		}
		if !strings.Contains(sql, "ORDER BY "+tests[0].wantOrder) {
			t.Errorf("search sort order missing from %q", sql)
		}
	})
}
