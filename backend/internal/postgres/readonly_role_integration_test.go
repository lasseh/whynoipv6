//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// TestReadonlyRoleReachesChunks is review issue 67's second half: 000007
// grants SELECT on all tables in `public` and sets default privileges FOR
// ROLE current_user, but hypertable chunks live in _timescaledb_internal and
// are created by the TimescaleDB background worker — not by the migrating
// role — so they fall outside that default-privileges grant by construction.
// If TimescaleDB does not propagate the hypertable's ACL to its chunks,
// Grafana reads work until the first chunk is created and then stop.
//
// The database is migrated, then rows are inserted — which creates chunks
// AFTER 000007 ran, which is the case in question. `SET ROLE` exercises the
// grants without needing a password on the NOLOGIN role.
func TestReadonlyRoleReachesChunks(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	// A domain to hang changelog rows off, then a row in each hypertable so
	// each has at least one chunk created after the grant.
	var domainID int32
	if err := pool.QueryRow(ctx,
		`INSERT INTO domain (host, kind, created_by, asn_id, country_id, tld)
		 VALUES ('chunk-probe.no', 'apex', 'tranco', (SELECT id FROM asn WHERE number = 0),
		         (SELECT id FROM country WHERE code = 'NO'), 'no') RETURNING id`).
		Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO changelog (domain_id, ts, field, old_value, new_value)
		 VALUES ($1, now(), 'base', 'unsupported', 'supported')`, domainID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO scan (domain_id, ts, base, www, ns, mx, conn, resources, classification)
		 VALUES ($1, now(), 'supported', 'supported', 'supported', 'supported',
		         'supported', 'supported', 'hero')`, domainID); err != nil {
		t.Fatal(err)
	}

	var chunks int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM timescaledb_information.chunks`).Scan(&chunks); err != nil {
		t.Fatal(err)
	}
	if chunks == 0 {
		t.Fatal("no chunks created; the test proves nothing")
	}
	t.Logf("%d chunks exist", chunks)

	// Everything a Grafana dashboard would read.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET ROLE whynoipv6_ro`); err != nil {
		t.Fatalf("SET ROLE whynoipv6_ro: %v", err)
	}
	for _, q := range []struct{ what, sql string }{
		{"changelog hypertable", `SELECT count(*) FROM changelog`},
		{"scan hypertable", `SELECT count(*) FROM scan`},
		{"changelog_daily cagg", `SELECT count(*) FROM changelog_daily`},
		{"stats_global_daily", `SELECT count(*) FROM stats_global_daily`},
		{"domain", `SELECT count(*) FROM domain`},
	} {
		var n int
		if err := conn.QueryRow(ctx, q.sql).Scan(&n); err != nil {
			t.Errorf("whynoipv6_ro cannot read %s: %v", q.what, err)
		}
	}

	// And it must still be unable to write.
	if _, err := conn.Exec(ctx, `INSERT INTO country (name, code, tld) VALUES ('X', 'XX', '.XX')`); err == nil {
		t.Error("whynoipv6_ro wrote to country; the role is not read-only")
	}
}
