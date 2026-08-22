//go:build integration

package ingest

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

// fakeSource serves a scripted list ID + CSV zip.
type fakeSource struct {
	id  string
	csv string
}

func (f *fakeSource) ListID(context.Context) (string, error) { return f.id, nil }
func (f *fakeSource) List(context.Context, string) (*TrancoArchive, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("top-1m.csv")
	if err != nil {
		return nil, err
	}
	if _, err := w.Write([]byte(f.csv)); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return &TrancoArchive{Zip: buf.Bytes(), ETag: `"e1"`, LastModified: time.Now().UTC()}, nil
}

func crlf(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }

func importList(t *testing.T, pool *pgxpool.Pool, id, csv string, force bool) *TrancoReport {
	t.Helper()
	imp := NewTrancoImporter(pool, &fakeSource{id: id, csv: csv}, TrancoConfig{MinRows: 3, MaxDelistPct: 2.0})
	rep, err := imp.Import(context.Background(), force)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return rep
}

// TestTrancoImport covers 06-ingest §9.2: CRLF, junk rejection, mixed-case
// duplicate fold (lowest rank wins), IDN conversion, counters, 24h spread,
// populated tld.
func TestTrancoImport(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	csv := crlf(
		"1,Example.COM",
		"2,_wildcard_.ph", // rejected: LDH underscore
		"3,dnb.no",
		"4,DNB.NO",  // folds into dnb.no; rank 3 (lowest) wins
		"5,møre.no", // IDN -> xn--mre-0na.no
		"6,bbc.co.uk",
		"notanumber,junk.example", // rejected: rank parse
	)
	rep := importList(t, pool, "L001", csv, false)

	if rep.Outcome != TrancoImported {
		t.Fatalf("outcome = %s (%s), want imported", rep.Outcome, rep.Note)
	}
	if rep.LineCount != 7 || rep.RejectedCount != 2 || rep.DuplicateCount != 1 {
		t.Errorf("counters line/rejected/dup = %d/%d/%d, want 7/2/1",
			rep.LineCount, rep.RejectedCount, rep.DuplicateCount)
	}
	if rep.ImportedCount != 4 || rep.Delisted != 0 {
		t.Errorf("imported/delisted = %d/%d, want 4/0", rep.ImportedCount, rep.Delisted)
	}

	var rank int
	if err := pool.QueryRow(ctx, "SELECT rank FROM domain WHERE host = 'dnb.no'").Scan(&rank); err != nil {
		t.Fatalf("dnb.no: %v", err)
	}
	if rank != 3 {
		t.Errorf("dnb.no rank = %d, want 3 (lowest-rank-wins fold)", rank)
	}

	var idn int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM domain WHERE host = 'xn--mre-0na.no'").Scan(&idn); err != nil || idn != 1 {
		t.Errorf("IDN row xn--mre-0na.no missing (n=%d, err=%v)", idn, err)
	}

	// 24h spread + tld populated + attribution on every inserted apex.
	var bad int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM domain WHERE
		next_check_at <= now() OR next_check_at > now() + interval '24 hours'
		OR tld IS NULL OR tld = ''`).Scan(&bad); err != nil {
		t.Fatal(err)
	}
	if bad != 0 {
		t.Errorf("%d rows outside the 24h next_check_at spread or with NULL tld", bad)
	}
	var tld string
	if err := pool.QueryRow(ctx, "SELECT tld FROM domain WHERE host = 'bbc.co.uk'").Scan(&tld); err != nil || tld != "co.uk" {
		t.Errorf("bbc.co.uk tld = %q err=%v, want co.uk", tld, err)
	}
	// ccTLD insert-time attribution: dnb.no -> country NO, sentinel ASN.
	var cc string
	if err := pool.QueryRow(ctx, `SELECT c.code FROM domain d JOIN country c ON c.id = d.country_id
		WHERE d.host = 'dnb.no'`).Scan(&cc); err != nil || cc != "NO" {
		t.Errorf("dnb.no country = %q err=%v, want NO", cc, err)
	}
	if err := pool.QueryRow(ctx, `SELECT c.code FROM domain d JOIN country c ON c.id = d.country_id
		WHERE d.host = 'example.com'`).Scan(&cc); err != nil || cc != "UN" {
		t.Errorf("example.com country = %q err=%v, want UN sentinel", cc, err)
	}
}

// TestTrancoIdempotency covers §9.3: same list ID is a no-op; aborted lists
// are not auto-retried; --force imports them.
func TestTrancoIdempotency(t *testing.T) {
	pool := pgtest.NewDB(t)

	csv := crlf("1,example.com", "2,dnb.no", "3,bbc.co.uk")
	rep := importList(t, pool, "L010", csv, false)
	if rep.Outcome != TrancoImported {
		t.Fatalf("first import: %s", rep.Outcome)
	}
	rep = importList(t, pool, "L010", csv, false)
	if rep.Outcome != TrancoNoNewList {
		t.Errorf("re-import same list = %s, want no_new_list", rep.Outcome)
	}

	// A list that aborts (min_rows) is never auto-retried; --force imports.
	tiny := crlf("1,example.org")
	rep = importList(t, pool, "L011", tiny, false)
	if rep.Outcome != TrancoAborted {
		t.Fatalf("undersized list = %s, want aborted", rep.Outcome)
	}
	rep = importList(t, pool, "L011", tiny, false)
	if rep.Outcome != TrancoAbortedPreviously {
		t.Errorf("aborted list retry = %s, want aborted_previously", rep.Outcome)
	}
	rep = importList(t, pool, "L011", tiny, true)
	if rep.Outcome != TrancoImported {
		t.Errorf("forced import of aborted list = %s (%s), want imported", rep.Outcome, rep.Note)
	}
}

// TestTrancoSanityGuard covers §9.4: a >2% delist fixture aborts with ranks
// unchanged; --force applies it.
func TestTrancoSanityGuard(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	// 100 ranked rows.
	lines := make([]string, 0, 100)
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("%d,d%d.example", i, i))
	}
	rep := importList(t, pool, "L020", crlf(lines...), false)
	if rep.Outcome != TrancoImported || rep.ImportedCount != 100 {
		t.Fatalf("seed import: %s imported=%d", rep.Outcome, rep.ImportedCount)
	}

	// Next list drops 5 of 100 ranked rows (5% > 2%).
	rep = importList(t, pool, "L021", crlf(lines[:95]...), false)
	if rep.Outcome != TrancoAborted {
		t.Fatalf("delist-heavy list = %s, want aborted", rep.Outcome)
	}
	if !strings.Contains(rep.Note, "delist") {
		t.Errorf("abort note %q does not mention delist", rep.Note)
	}
	var ranked int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM domain WHERE rank IS NOT NULL").Scan(&ranked); err != nil {
		t.Fatal(err)
	}
	if ranked != 100 {
		t.Errorf("ranks after abort = %d, want 100 unchanged", ranked)
	}

	rep = importList(t, pool, "L021", crlf(lines[:95]...), true)
	if rep.Outcome != TrancoImported || rep.Delisted != 5 {
		t.Errorf("forced apply = %s delisted=%d, want imported/5", rep.Outcome, rep.Delisted)
	}
}

// TestTrancoStaleBudget covers §9.4: the delist budget scales with how many
// days of churn the import has to absorb, so a run that aborted once does
// not stay wedged forever against an ever-staler DB.
func TestTrancoStaleBudget(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	lines := make([]string, 0, 100)
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("%d,d%d.example", i, i))
	}
	if rep := importList(t, pool, "L030", crlf(lines...), false); rep.Outcome != TrancoImported {
		t.Fatalf("seed import: %s", rep.Outcome)
	}

	// Backdate the success: 5 days stale buys min(2.0*5, 10) = 10%.
	if _, err := pool.Exec(ctx,
		"UPDATE tranco_import SET imported_at = now() - interval '5 days' WHERE NOT aborted"); err != nil {
		t.Fatal(err)
	}

	// The same 5-of-100 drop that aborts on a fresh DB now fits the budget.
	rep := importList(t, pool, "L031", crlf(lines[:95]...), false)
	if rep.Outcome != TrancoImported || rep.Delisted != 5 {
		t.Fatalf("stale budget = %s delisted=%d (%s), want imported/5", rep.Outcome, rep.Delisted, rep.Note)
	}

	// The ceiling binds: 10 days would buy 20% unscaled, but 15% still
	// aborts because the budget is capped at 10%.
	if _, err := pool.Exec(ctx,
		"UPDATE tranco_import SET imported_at = now() - interval '10 days' WHERE NOT aborted"); err != nil {
		t.Fatal(err)
	}
	rep = importList(t, pool, "L032", crlf(lines[:85]...), false)
	if rep.Outcome != TrancoAborted {
		t.Errorf("15%% delist at a 10%% ceiling = %s, want aborted", rep.Outcome)
	}
}

// TestTrancoReEntry covers §9.5: delisted rows re-enable with an immediate
// rescan; dead rows stay disabled but rescan now; service/manual rows get
// only the rank update.
func TestTrancoReEntry(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	csv := crlf("1,a.example", "2,b.example", "3,c.example", "4,d.example")
	if rep := importList(t, pool, "L030", csv, false); rep.Outcome != TrancoImported {
		t.Fatalf("seed: %s", rep.Outcome)
	}
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	future := "next_check_at = now() + interval '10 hours'"
	mustExec("UPDATE domain SET disabled=true, disabled_reason='delisted', disabled_at=now(), orphaned_at=now(), " + future + " WHERE host='a.example'")
	mustExec("UPDATE domain SET disabled=true, disabled_reason='dead', disabled_at=now(), " + future + " WHERE host='b.example'")
	mustExec("UPDATE domain SET disabled=true, disabled_reason='service', disabled_at=now(), " + future + " WHERE host='c.example'")
	mustExec("UPDATE domain SET disabled=true, disabled_reason='manual', disabled_at=now(), " + future + " WHERE host='d.example'")

	// New list: same hosts, shifted ranks (so the guarded upsert touches all).
	csv2 := crlf("11,a.example", "12,b.example", "13,c.example", "14,d.example")
	if rep := importList(t, pool, "L031", csv2, false); rep.Outcome != TrancoImported {
		t.Fatalf("re-entry import: %s", rep.Outcome)
	}

	type row struct {
		disabled bool
		reason   *string
		dueNow   bool
		rank     int
	}
	get := func(host string) row {
		t.Helper()
		var r row
		if err := pool.QueryRow(ctx,
			"SELECT disabled, disabled_reason, next_check_at <= now(), rank FROM domain WHERE host=$1", host).
			Scan(&r.disabled, &r.reason, &r.dueNow, &r.rank); err != nil {
			t.Fatal(err)
		}
		return r
	}

	a := get("a.example")
	if a.disabled || a.reason != nil || !a.dueNow || a.rank != 11 {
		t.Errorf("delisted re-entry = %+v, want enabled/nil/due-now/rank 11", a)
	}
	b := get("b.example")
	if !b.disabled || b.reason == nil || *b.reason != "dead" || !b.dueNow || b.rank != 12 {
		t.Errorf("dead re-entry = %+v, want disabled/dead/due-now/rank 12", b)
	}
	c := get("c.example")
	if !c.disabled || *c.reason != "service" || c.dueNow || c.rank != 13 {
		t.Errorf("service re-entry = %+v, want disabled/service/not-due/rank 13", c)
	}
	d := get("d.example")
	if !d.disabled || *d.reason != "manual" || d.dueNow || d.rank != 14 {
		t.Errorf("manual re-entry = %+v, want disabled/manual/not-due/rank 14", d)
	}
}
