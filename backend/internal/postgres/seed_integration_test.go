//go:build integration

package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestSeedMigrationsAreReRunnable is review issue 67. The seed INSERTs had no
// ON CONFLICT, so re-running them raised a unique violation. Nothing could be
// corrupted by that — the constraints held — but it broke the documented
// recovery path: `migrate force N-1 && migrate up` died on the seed and left
// schema_migrations dirty again, with hand-editing the version row as the
// only way forward.
//
// The test executes each seed file's own SQL a second time against an
// already-migrated database, which is exactly what that recovery does.
//
// Note this cannot go through pgtest's template cache to prove itself: the
// template is keyed on the highest migration NUMBER, not on file content
// (pgtest.EmbeddedVersion), so editing an applied migration in place does not
// rebuild it. Running the file's SQL directly sidesteps that.
func TestSeedMigrationsAreReRunnable(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	for _, name := range []string{
		"000003_seed.up.sql",
		"000010_hosting_provider.up.sql",
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
			if err != nil {
				t.Fatal(err)
			}
			// 000010 also carries DDL, which is not re-runnable and is not
			// what this test is about. Take only the seed INSERTs.
			for _, stmt := range strings.Split(stripSQLComments(string(raw)), ";") {
				if !strings.Contains(stmt, "INSERT INTO") {
					continue
				}
				if _, err := pool.Exec(ctx, stmt+";"); err != nil {
					t.Errorf("re-running a seed INSERT failed: %v", err)
				}
			}
		})
	}

	// The re-run must not have duplicated anything either.
	for _, c := range []struct {
		what, query string
		want        int
	}{
		{"asn sentinel", `SELECT count(*) FROM asn WHERE number = 0`, 1},
		{"country UN sentinel", `SELECT count(*) FROM country WHERE code = 'UN'`, 1},
		{"country rows", `SELECT count(*) FROM country`, 251},
		{"hosting providers", `SELECT count(*) FROM hosting_provider`, 13},
	} {
		var n int
		if err := pool.QueryRow(ctx, c.query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != c.want {
			t.Errorf("%s: %d rows after a re-run, want %d", c.what, n, c.want)
		}
	}
}

// stripSQLComments drops -- line comments so the naive split on ";" below
// does not cut a statement in half at a semicolon inside prose. The seed
// files carry no string literal containing "--".
func stripSQLComments(sql string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
