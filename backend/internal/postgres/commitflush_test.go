package postgres

import (
	"reflect"
	"regexp"
	"strconv"
	"testing"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// TestCommitStatementBinding pins the invariant queueParams relies on: every
// sqlc params struct has exactly one field per SQL placeholder, in $1..$n
// order. A regenerated statement whose placeholder count drifts from its
// params struct fails here instead of at runtime.
func TestCommitStatementBinding(t *testing.T) {
	cases := []struct {
		name   string
		sql    string
		params any
	}{
		{"CommitDomain", db.CommitDomain, db.CommitDomainParams{}},
		{"InsertChangelog", db.InsertChangelog, db.InsertChangelogParams{}},
		{"InsertScan", db.InsertScan, db.InsertScanParams{}},
		{"InsertScanDetail", db.InsertScanDetail, db.InsertScanDetailParams{}},
	}
	re := regexp.MustCompile(`\$(\d+)`)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			maxPh := 0
			for _, m := range re.FindAllStringSubmatch(tc.sql, -1) {
				n, err := strconv.Atoi(m[1])
				if err != nil {
					t.Fatalf("placeholder %q: %v", m[0], err)
				}
				maxPh = max(maxPh, n)
			}
			if fields := reflect.TypeOf(tc.params).NumField(); fields != maxPh {
				t.Errorf("%s: params struct has %d fields, SQL has %d placeholders",
					tc.name, fields, maxPh)
			}
		})
	}
}
