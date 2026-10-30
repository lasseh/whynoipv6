//go:build integration

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/postgres"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// linkFixture seeds one domain_resource row per entry, and resource_host's
// dependent_count from the rows it actually got.
func linkFixture(t *testing.T, pool *pgxpool.Pool, links []pruneLink) {
	t.Helper()
	ctx := context.Background()
	for _, l := range links {
		if _, err := pool.Exec(ctx,
			`INSERT INTO domain (host, kind, created_by, asn_id, country_id, tld)
			 VALUES ($1, 'apex', 'tranco', (SELECT id FROM asn WHERE number = 0),
			         (SELECT id FROM country WHERE code = 'NO'), 'no')
			 ON CONFLICT (host) DO NOTHING`, l.domain); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO resource_host (host) VALUES ($1) ON CONFLICT (host) DO NOTHING`, l.host); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO domain_resource (domain_id, resource_host_id, source, first_seen, last_seen)
			 SELECT d.id, rh.id, $3::resource_source, $4, $4
			 FROM domain d, resource_host rh WHERE d.host = $1 AND rh.host = $2`,
			l.domain, l.host, l.source, l.lastSeen); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx,
		`UPDATE resource_host rh SET dependent_count =
		   (SELECT count(*) FROM domain_resource dr WHERE dr.resource_host_id = rh.id)`); err != nil {
		t.Fatal(err)
	}
}

type pruneLink struct {
	domain, host, source string
	lastSeen             time.Time
}

// linkCounts is host → real link count, for every host with a registry row.
func linkCounts(t *testing.T, pool *pgxpool.Pool) map[string]int {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT rh.host, count(dr.domain_id)::int, rh.dependent_count
		 FROM resource_host rh LEFT JOIN domain_resource dr ON dr.resource_host_id = rh.id
		 GROUP BY rh.host, rh.dependent_count`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var host string
		var real, counter int
		if err := rows.Scan(&host, &real, &counter); err != nil {
			t.Fatal(err)
		}
		// The invariant every writer of this column owes (06 §5.1).
		if real != counter {
			t.Errorf("%s: dependent_count = %d, real links = %d", host, counter, real)
		}
		got[host] = real
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

// TestResourcePrune pins saint-blockers 02. The fixture's point is gmpg.org:
// one host losing links from several domains at once, which the crawler's own
// per-domain prune never does and a `dependent_count - 1` would get wrong.
func TestResourcePrune(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	t.Setenv("DATABASE_URL", pool.Config().ConnString())

	now := time.Now().UTC()
	stale, fresh := now.Add(-2*time.Hour), now
	cutoff := now.Add(-time.Hour)

	linkFixture(t, pool, []pruneLink{
		{"a.no", "gmpg.org", "discovered", stale},
		{"b.no", "gmpg.org", "discovered", stale},
		{"c.no", "gmpg.org", "discovered", stale},
		// A manual pin on a host that is being pruned: the case the delete's
		// own source filter exists for.
		{"d.no", "gmpg.org", "manual", stale},
		{"a.no", "s.w.org", "discovered", stale},
		{"b.no", "s.w.org", "discovered", fresh},
		{"a.no", "fonts.example", "discovered", fresh},
		{"a.no", "manual.example", "manual", stale},
	})
	before := linkCounts(t, pool)

	t.Run("a dry run reports the same set and writes nothing", func(t *testing.T) {
		var out bytes.Buffer
		if err := resourcePrune(ctx, pool, &out, cutoff, true); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"gmpg.org", "s.w.org", "4 links across 2 hosts", "dry run"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("report lacks %q:\n%s", want, out.String())
			}
		}
		for _, host := range []string{"fonts.example", "manual.example"} {
			if strings.Contains(out.String(), host) {
				t.Errorf("report names %s, which has nothing to prune:\n%s", host, out.String())
			}
		}
		after := linkCounts(t, pool)
		for host, n := range before {
			if after[host] != n {
				t.Errorf("%s: %d links after a dry run, want %d", host, after[host], n)
			}
		}
	})

	t.Run("the real run deletes only stale discovered links", func(t *testing.T) {
		if err := runV6ctl(t, ctx, "resource", "prune", "--before", cutoff.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		got := linkCounts(t, pool)
		for host, want := range map[string]int{
			"gmpg.org":       1, // the manual pin stays, the three discovered go
			"s.w.org":        1, // the fresh link stays
			"fonts.example":  1,
			"manual.example": 1, // manual links are never pruned, however old
		} {
			if got[host] != want {
				t.Errorf("%s: %d links, want %d", host, got[host], want)
			}
		}
	})

	t.Run("a second run finds nothing", func(t *testing.T) {
		var out bytes.Buffer
		if err := resourcePrune(ctx, pool, &out, cutoff, false); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "nothing to prune") {
			t.Errorf("second run:\n%s", out.String())
		}
		linkCounts(t, pool)
	})
}

// TestResourcePruneKeepsARescannedLink is the commit race in sequence: a link
// refreshed after the candidate list was read must survive, because the delete
// re-tests last_seen rather than trusting the list.
func TestResourcePruneKeepsARescannedLink(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	linkFixture(t, pool, []pruneLink{{"a.no", "gmpg.org", "discovered", now.Add(-2 * time.Hour)}})

	if _, err := pool.Exec(ctx, `UPDATE domain_resource SET last_seen = now()`); err != nil {
		t.Fatal(err)
	}
	q := db.New(pool)
	id, err := q.ResourceHostIDByHost(ctx, "gmpg.org")
	if err != nil {
		t.Fatal(err)
	}
	links, err := q.ResourcePruneHost(ctx, db.ResourcePruneHostParams{
		ResourceHostID: id, Cutoff: postgres.TS(now.Add(-time.Hour)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if links != 0 {
		t.Errorf("deleted %d links refreshed after the cutoff", links)
	}
	if got := linkCounts(t, pool); got["gmpg.org"] != 1 {
		t.Errorf("gmpg.org has %d links, want 1", got["gmpg.org"])
	}
}
