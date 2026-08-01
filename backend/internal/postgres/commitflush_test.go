package postgres

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// TestCommitStatementBinding pins the invariant queueParams relies on:
// struct field declaration order IS placeholder order. It verifies the full
// $n→column correspondence against each params struct's json tags (sqlc
// emits them as column/param names), so a transposed pair of same-typed
// fields — which a count check would pass — fails here.
func TestCommitStatementBinding(t *testing.T) {
	// Params whose sqlc name differs from the column they bind:
	// WHERE id/claimed_at → domain_id/lease, SET last_checked_at ← ts.
	alias := map[string]string{"id": "domain_id", "claimed_at": "lease", "last_checked_at": "ts"}

	// tagAt returns field i's json tag (the sqlc param name).
	tagAt := func(params any, i int) string {
		tag := reflect.TypeOf(params).Field(i).Tag.Get("json")
		return strings.Split(tag, ",")[0]
	}

	t.Run("CommitDomain", func(t *testing.T) {
		// UPDATE shape: every `col = $n` pair, in SET and WHERE, plus the
		// conditional pivot shape `col = CASE WHEN $a::boolean THEN $b ELSE
		// col END`, whose flag param is named stamp_<col> by convention.
		caseRe := regexp.MustCompile(`(\w+)\s*=\s*CASE WHEN \$(\d+)::boolean THEN \$(\d+) ELSE \w+ END`)
		byPlaceholder := map[int]string{}
		for _, m := range caseRe.FindAllStringSubmatch(db.CommitDomain, -1) {
			flag, _ := strconv.Atoi(m[2])
			val, _ := strconv.Atoi(m[3])
			byPlaceholder[flag] = "stamp_" + m[1]
			byPlaceholder[val] = m[1]
		}
		re := regexp.MustCompile(`(\w+)\s*=\s*\$(\d+)`)
		for _, m := range re.FindAllStringSubmatch(db.CommitDomain, -1) {
			n, _ := strconv.Atoi(m[2])
			if _, claimed := byPlaceholder[n]; claimed {
				continue // a CASE-bound pivot param
			}
			col := m[1]
			if a, ok := alias[col]; ok {
				col = a
			}
			byPlaceholder[n] = col
		}
		typ := reflect.TypeOf(db.CommitDomainParams{})
		if len(byPlaceholder) != typ.NumField() {
			t.Fatalf("SQL binds %d placeholders, params struct has %d fields",
				len(byPlaceholder), typ.NumField())
		}
		for i := 0; i < typ.NumField(); i++ {
			if want := byPlaceholder[i+1]; tagAt(db.CommitDomainParams{}, i) != want {
				t.Errorf("field %d (%s) feeds $%d, which the SQL binds to %s",
					i, tagAt(db.CommitDomainParams{}, i), i+1, want)
			}
		}
	})

	// INSERT shape: the column list maps to $1..$n in order.
	inserts := []struct {
		name   string
		sql    string
		params any
	}{
		{"InsertChangelog", db.InsertChangelog, db.InsertChangelogParams{}},
		{"InsertScan", db.InsertScan, db.InsertScanParams{}},
		{"InsertScanDetail", db.InsertScanDetail, db.InsertScanDetailParams{}},
	}
	colsRe := regexp.MustCompile(`(?s)INSERT INTO \w+ \(([^)]+)\)`)
	phRe := regexp.MustCompile(`\$(\d+)`)
	for _, tc := range inserts {
		t.Run(tc.name, func(t *testing.T) {
			m := colsRe.FindStringSubmatch(tc.sql)
			if m == nil {
				t.Fatal("no INSERT column list found")
			}
			var cols []string
			for _, c := range strings.Split(m[1], ",") {
				cols = append(cols, strings.TrimSpace(c))
			}
			typ := reflect.TypeOf(tc.params)
			if len(cols) != typ.NumField() {
				t.Fatalf("SQL lists %d columns, params struct has %d fields", len(cols), typ.NumField())
			}
			// Placeholders must be exactly $1..$n (each once — VALUES order).
			seen := map[int]bool{}
			for _, pm := range phRe.FindAllStringSubmatch(tc.sql, -1) {
				n, _ := strconv.Atoi(pm[1])
				if seen[n] {
					t.Errorf("$%d bound twice", n)
				}
				seen[n] = true
			}
			if len(seen) != len(cols) {
				t.Fatalf("%d distinct placeholders for %d columns", len(seen), len(cols))
			}
			for i, col := range cols {
				if got := tagAt(tc.params, i); got != col {
					t.Errorf("field %d (%s) feeds $%d, which the SQL binds to column %s", i, got, i+1, col)
				}
			}
		})
	}
}
