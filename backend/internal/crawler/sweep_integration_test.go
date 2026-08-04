//go:build integration

package crawler

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

func sweepCfg() SweepConfig {
	return SweepConfig{
		LiveCheckLinkage: 168 * time.Hour,
		DelistGrace:      720 * time.Hour,
		SlowLaneEvery:    720 * time.Hour,
	}
}

// mkDomain inserts a rank-NULL entity and returns its id.
func mkDomain(t *testing.T, pool *pgxpool.Pool, host, createdBy string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO domain (host, kind, created_by, asn_id, country_id, tld)
		VALUES ($1, 'apex', $2,
		        (SELECT id FROM asn WHERE number=0), (SELECT id FROM country WHERE code='UN'), 'example')
		RETURNING id`, host, createdBy).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// mkSubdomain inserts a rank-NULL child of parentID and returns its id.
func mkSubdomain(t *testing.T, pool *pgxpool.Pool, host string, parentID int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO domain (host, kind, parent_id, created_by, asn_id, country_id, tld)
		VALUES ($1, 'subdomain', $2, 'curated',
		        (SELECT id FROM asn WHERE number=0), (SELECT id FROM country WHERE code='UN'), 'example')
		RETURNING id`, host, parentID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func mkCampaign(t *testing.T, pool *pgxpool.Pool, name string, memberID int64) int32 {
	t.Helper()
	var cid int32
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO campaign (uuid, name) VALUES (gen_random_uuid(), $1) RETURNING id`, name).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO campaign_domain (campaign_id, domain_id) VALUES ($1, $2)", cid, memberID); err != nil {
		t.Fatal(err)
	}
	return cid
}

// TestSweep (04 §17.5): S1–S5 in isolation and as a sequence.
func TestSweep(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	// Fixture population:
	// a: campaign-linked, previously orphan-stamped → S1 clears the mark.
	a := mkDomain(t, pool, "a.example", "campaign")
	mkCampaign(t, pool, "camp-a", a)
	mustExec(t, pool, "UPDATE domain SET orphaned_at = now() - interval '10 days' WHERE id=$1", a)

	// b: delisted but member of a re-enabled campaign → S2 re-enables.
	b := mkDomain(t, pool, "b.example", "campaign")
	mkCampaign(t, pool, "camp-b", b)
	mustExec(t, pool, `UPDATE domain SET disabled=true, disabled_reason='delisted',
		disabled_at=now(), next_check_at=now()+interval '400 hours' WHERE id=$1`, b)

	// c: live_check row with expired linkage → S3 delists immediately (no grace).
	c := mkDomain(t, pool, "c.example", "live_check")
	mustExec(t, pool, "UPDATE domain SET last_requested_at = now() - interval '8 days' WHERE id=$1", c)

	// d: unlinked campaign-created row, fresh orphan → S4 stamps.
	d := mkDomain(t, pool, "d.example", "campaign")

	// e: unlinked with expired grace → S5 delists.
	e := mkDomain(t, pool, "e.example", "campaign")
	mustExec(t, pool, "UPDATE domain SET orphaned_at = now() - interval '31 days' WHERE id=$1", e)

	// f: member of a DISABLED campaign → not pinned: gets stamped like d.
	f := mkDomain(t, pool, "f.example", "campaign")
	fc := mkCampaign(t, pool, "camp-f", f)
	mustExec(t, pool, "UPDATE campaign SET disabled = true WHERE id=$1", fc)

	res, err := Sweep(ctx, pool, sweepCfg())
	if err != nil {
		t.Fatal(err)
	}
	if res.OrphansCleared != 1 || res.Reenabled != 1 || res.LiveCheckDelisted != 1 ||
		res.OrphansStamped != 2 || res.Delisted != 1 {
		t.Fatalf("sweep counts = %+v, want 1/1/1/2/1", res)
	}

	type row struct {
		disabled bool
		reason   *string
		orphaned *time.Time
		dueNow   bool
	}
	get := func(id int64) row {
		t.Helper()
		var r row
		if err := pool.QueryRow(ctx,
			"SELECT disabled, disabled_reason::text, orphaned_at, next_check_at <= now() FROM domain WHERE id=$1", id).
			Scan(&r.disabled, &r.reason, &r.orphaned, &r.dueNow); err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := get(a); r.orphaned != nil {
		t.Error("S1: linked row keeps its orphan mark")
	}
	if r := get(b); r.disabled || !r.dueNow {
		t.Errorf("S2: delisted linked row not re-enabled: %+v", r)
	}
	if r := get(c); !r.disabled || *r.reason != "delisted" {
		t.Errorf("S3: expired live_check row not delisted: %+v", r)
	}
	if r := get(d); r.orphaned == nil || r.disabled {
		t.Errorf("S4: unlinked row not grace-stamped: %+v", r)
	}
	if r := get(e); !r.disabled || *r.reason != "delisted" {
		t.Errorf("S5: expired-grace row not delisted: %+v", r)
	}
	if r := get(f); r.orphaned == nil {
		t.Error("disabled campaign must not pin its member (f should be stamped)")
	}

	// Monotonic grace: d's stamp must not move on a second run.
	dStamp := *get(d).orphaned
	res2, err := Sweep(ctx, pool, sweepCfg())
	if err != nil {
		t.Fatal(err)
	}
	if res2.OrphansCleared != 0 || res2.Reenabled != 0 || res2.LiveCheckDelisted != 0 ||
		res2.OrphansStamped != 0 || res2.Delisted != 0 {
		t.Fatalf("same-day second run changed rows: %+v", res2)
	}
	if got := *get(d).orphaned; !got.Equal(dStamp) {
		t.Errorf("grace stamp moved: %v → %v", dStamp, got)
	}
}

// TestSweepCuratedLinkage: curated_subdomain membership is sweep linkage — a
// rank-NULL curated child keeps the frontier while its subdomains/<apex>.yml
// file lists it, and enters the normal grace → delist path once dropped.
func TestSweepCuratedLinkage(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	parent := mkDomain(t, pool, "curated.example", "tranco")
	mustExec(t, pool, "UPDATE domain SET rank = 42 WHERE id=$1", parent)
	child := mkSubdomain(t, pool, "api.curated.example", parent)
	mustExec(t, pool, "INSERT INTO curated_subdomain (domain_id) VALUES ($1)", child)

	state := func(id int64) (disabled bool, reason *string, orphaned *time.Time) {
		t.Helper()
		if err := pool.QueryRow(ctx,
			"SELECT disabled, disabled_reason::text, orphaned_at FROM domain WHERE id=$1", id).
			Scan(&disabled, &reason, &orphaned); err != nil {
			t.Fatal(err)
		}
		return
	}
	sweep := func() SweepResult {
		t.Helper()
		res, err := Sweep(ctx, pool, sweepCfg())
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	// Listed: no campaign, no children, never live-checked — linked anyway.
	if res := sweep(); res.OrphansStamped != 0 {
		t.Fatalf("listed curated child stamped: %+v", res)
	}
	if _, _, orphaned := state(child); orphaned != nil {
		t.Error("listed curated child carries an orphan mark")
	}

	// Dropped from the file: S4 stamps it, grace starts.
	mustExec(t, pool, "DELETE FROM curated_subdomain WHERE domain_id=$1", child)
	if res := sweep(); res.OrphansStamped != 1 {
		t.Fatalf("unlisted curated child not stamped: %+v", res)
	}

	// Grace expired: S5 delists.
	mustExec(t, pool, "UPDATE domain SET orphaned_at = now() - interval '31 days' WHERE id=$1", child)
	if res := sweep(); res.Delisted != 1 {
		t.Fatalf("expired-grace curated child not delisted: %+v", res)
	}
	if disabled, reason, _ := state(child); !disabled || *reason != "delisted" {
		t.Errorf("curated child state = disabled=%t reason=%v, want true/delisted", disabled, reason)
	}

	// Re-listed: S2 re-enables symmetrically (04 §7).
	mustExec(t, pool, "INSERT INTO curated_subdomain (domain_id) VALUES ($1)", child)
	if res := sweep(); res.Reenabled != 1 {
		t.Fatalf("re-listed curated child not re-enabled: %+v", res)
	}
	if disabled, _, orphaned := state(child); disabled || orphaned != nil {
		t.Errorf("re-listed curated child = disabled=%t orphaned=%v, want false/nil", disabled, orphaned)
	}

	// The ranked parent is never touched by any of this.
	if disabled, _, orphaned := state(parent); disabled || orphaned != nil {
		t.Errorf("ranked parent = disabled=%t orphaned=%v, want false/nil", disabled, orphaned)
	}
}

// TestStatsRollup: the shared tick-step-2/3 rollup lands the snapshot rows
// and counters idempotently; generated_at refreshes on re-run.
func TestStatsRollup(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	// Two ranked confirmed domains: one hero-ish, one sinner.
	mustExec(t, pool, `INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, tld,
			base_status, www_status, ns_status, mx_status, conn_status, classification)
		VALUES ('hero.example', 'apex', 1, 'tranco',
		        (SELECT id FROM asn WHERE number=0), (SELECT id FROM country WHERE code='NO'), 'example',
		        'supported', 'supported', 'supported', 'supported', 'supported', 'hero')`)
	mustExec(t, pool, `INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, tld,
			base_status, classification)
		VALUES ('sinner.example', 'apex', 2, 'tranco',
		        (SELECT id FROM asn WHERE number=0), (SELECT id FROM country WHERE code='NO'), 'example',
		        'unsupported', 'sinner')`)

	if err := RunStatsRollup(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var domains, heroes, sinners int
	var gen1 time.Time
	if err := pool.QueryRow(ctx, `SELECT domains, heroes, sinners, generated_at
		FROM stats_global_daily WHERE day = CURRENT_DATE`).Scan(&domains, &heroes, &sinners, &gen1); err != nil {
		t.Fatal(err)
	}
	if domains != 2 || heroes != 1 || sinners != 1 {
		t.Errorf("global snapshot = %d/%d/%d, want 2/1/1", domains, heroes, sinners)
	}

	var sites, v6sites int
	var pct string
	if err := pool.QueryRow(ctx, `SELECT sites, v6sites, percent::text FROM country WHERE code='NO'`).
		Scan(&sites, &v6sites, &pct); err != nil {
		t.Fatal(err)
	}
	if sites != 2 || v6sites != 1 || pct != "50.00" {
		t.Errorf("NO counters = %d/%d/%s, want 2/1/50.00", sites, v6sites, pct)
	}

	// Idempotent same-day re-run: values identical, generated_at refreshed.
	time.Sleep(20 * time.Millisecond)
	if err := RunStatsRollup(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var domains2 int
	var gen2 time.Time
	if err := pool.QueryRow(ctx, `SELECT domains, generated_at FROM stats_global_daily
		WHERE day = CURRENT_DATE`).Scan(&domains2, &gen2); err != nil {
		t.Fatal(err)
	}
	if domains2 != domains {
		t.Errorf("re-run changed counters: %d → %d", domains, domains2)
	}
	if !gen2.After(gen1) {
		t.Errorf("generated_at not refreshed: %v → %v", gen1, gen2)
	}
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
