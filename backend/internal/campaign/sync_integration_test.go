//go:build integration

package campaign

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

// writeFixture writes one campaign file into the checkout's campaigns/ dir.
func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	writeFileIn(t, filepath.Join(dir, CampaignsDir), name, content)
}

func writeFileIn(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustExecTest(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func writeSubdomainFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	writeFileIn(t, filepath.Join(dir, SubdomainsDir), name, content)
}

func run(t *testing.T, pool *pgxpool.Pool, dir string) *Report {
	t.Helper()
	rep, err := Sync(context.Background(), Config{
		RepoPath: dir, GitRemote: "origin", MaxDomainsPerFile: 1000,
		MaxSubdomainsPerDomain: 20, Pull: false, Push: false,
	}, pool)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return rep
}

const campA = `title: Campaign A
description: First fixture campaign.
domains:
    - a-one.no
    - api.a-two.no
`

// campKeeper exists so the checkout is never empty: an empty checkout is a
// broken clone, not a repo where every campaign was deleted (review issue
// 29), and Sync refuses it.
const campKeeper = `title: Keeper
description: Keeps the checkout non-empty.
domains:
    - keeper.no
`

// TestCampaignSync covers the 06-ingest §9.6 matrix on a fixture repo.
func TestCampaignSync(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	// --- new file without uuid: insert + write-back.
	writeFixture(t, dir, "a.yml", campA)
	rep := run(t, pool, dir)
	if len(rep.Created) != 1 || rep.MembershipAdds != 2 {
		t.Fatalf("create: %+v", rep)
	}
	if rep.WriteBack != "written (push disabled)" {
		t.Errorf("write-back = %q", rep.WriteBack)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, CampaignsDir, "a.yml"))
	if !strings.Contains(string(raw), "uuid: ") {
		t.Fatalf("uuid not written back:\n%s", raw)
	}
	uuidLine := ""
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(l, "uuid: ") {
			uuidLine = strings.TrimPrefix(l, "uuid: ")
		}
	}

	// Subdomain entry: parent auto-created with created_by='parent_link'.
	var createdBy, kind string
	var parentID *int64
	if err := pool.QueryRow(ctx,
		"SELECT created_by::text, kind::text FROM domain WHERE host='a-two.no'").
		Scan(&createdBy, &kind); err != nil {
		t.Fatalf("parent row: %v", err)
	}
	if createdBy != "parent_link" || kind != "apex" {
		t.Errorf("auto-created parent = %s/%s, want parent_link/apex", createdBy, kind)
	}
	if err := pool.QueryRow(ctx,
		"SELECT kind::text, parent_id FROM domain WHERE host='api.a-two.no'").
		Scan(&kind, &parentID); err != nil {
		t.Fatalf("subdomain row: %v", err)
	}
	if kind != "subdomain" || parentID == nil {
		t.Errorf("subdomain = %s parent=%v, want subdomain with parent_id", kind, parentID)
	}

	// --- idempotent re-run: no churn.
	rep = run(t, pool, dir)
	if len(rep.Created) != 0 || len(rep.Updated) != 1 || rep.MembershipAdds != 0 || rep.MembershipRemoves != 0 {
		t.Errorf("re-run should be churn-free: %+v", rep)
	}

	// --- rename: same uuid, new filename → source_file update, no churn.
	if err := os.Rename(filepath.Join(dir, CampaignsDir, "a.yml"), filepath.Join(dir, CampaignsDir, "a-renamed.yml")); err != nil {
		t.Fatal(err)
	}
	rep = run(t, pool, dir)
	if len(rep.Renamed) != 1 || rep.MembershipAdds != 0 {
		t.Errorf("rename: %+v", rep)
	}
	var srcFile string
	if err := pool.QueryRow(ctx, "SELECT source_file FROM campaign WHERE uuid=$1", uuidLine).Scan(&srcFile); err != nil {
		t.Fatal(err)
	}
	if srcFile != "a-renamed.yml" {
		t.Errorf("source_file = %q, want a-renamed.yml", srcFile)
	}

	// --- deletion: file removed → soft-disable, memberships kept.
	//
	// A second campaign has to be present for this to be a deletion at all:
	// removing the last file leaves nothing parsed, which since review
	// issue 29 is treated as a broken checkout and refused. That is the
	// distinction the guard draws — a file gone from a healthy repo still
	// disables its campaign.
	writeFixture(t, dir, "keeper.yml", campKeeper)
	if err := os.Remove(filepath.Join(dir, CampaignsDir, "a-renamed.yml")); err != nil {
		t.Fatal(err)
	}
	rep = run(t, pool, dir)
	if len(rep.Disabled) != 1 {
		t.Fatalf("disable: %+v", rep)
	}
	var disabled bool
	var members int
	if err := pool.QueryRow(ctx,
		"SELECT c.disabled, (SELECT count(*) FROM campaign_domain cd WHERE cd.campaign_id=c.id) FROM campaign c WHERE uuid=$1",
		uuidLine).Scan(&disabled, &members); err != nil {
		t.Fatal(err)
	}
	if !disabled || members != 2 {
		t.Errorf("soft delete = disabled=%t members=%d, want true/2", disabled, members)
	}

	// --- re-appearance (same uuid): re-enable, no membership churn.
	writeFixture(t, dir, "a-renamed.yml", campA[:len(campA)-1]+"\nuuid: "+uuidLine+"\n")
	rep = run(t, pool, dir)
	if len(rep.ReEnabled) != 1 || rep.MembershipAdds != 0 {
		t.Errorf("re-appearance: %+v", rep)
	}

	// --- unknown uuid is always adopted; editing one in place forks the
	// campaign (old row soft-disabled by the uuid-set diff, new one created).
	writeFixture(t, dir, "b.yml", `title: Campaign B
description: Unknown uuid fixture.
uuid: 3b3b3b3b-1111-2222-3333-444444444444
domains:
    - b-one.no
`)
	rep = run(t, pool, dir)
	if len(rep.Created) != 1 {
		t.Errorf("unknown uuid on a new file should insert b.yml: %+v", rep)
	}
	writeFixture(t, dir, "b.yml", `title: Campaign B
description: Unknown uuid fixture.
uuid: 3b3b3b3b-1111-2222-3333-999999999999
domains:
    - b-one.no
`)
	rep = run(t, pool, dir)
	if len(rep.Created) != 1 || len(rep.Disabled) != 1 {
		t.Errorf("a uuid edited in place should fork: created=%v disabled=%v", rep.Created, rep.Disabled)
	}
	if _, ok := rep.RejectedFiles["b.yml"]; ok {
		t.Errorf("a uuid edited in place must not be rejected: %+v", rep.RejectedFiles)
	}
	writeFixture(t, dir, "b.yml", `title: Campaign B
description: Unknown uuid fixture.
uuid: 3b3b3b3b-1111-2222-3333-444444444444
domains:
    - b-one.no
`)
	if rep = run(t, pool, dir); len(rep.ReEnabled) != 1 {
		t.Errorf("restoring the uuid should re-enable b.yml: %+v", rep)
	}

	// --- duplicate uuid across files: source_file match wins.
	writeFixture(t, dir, "b-copy.yml", `title: Campaign B copy
description: Copied uuid fixture.
uuid: 3b3b3b3b-1111-2222-3333-444444444444
domains:
    - b-two.no
`)
	rep = run(t, pool, dir)
	if _, ok := rep.RejectedFiles["b-copy.yml"]; !ok {
		t.Errorf("duplicate uuid should reject the copy: %+v", rep.RejectedFiles)
	}
	if _, ok := rep.RejectedFiles["b.yml"]; ok {
		t.Errorf("original file must survive the duplicate-uuid guard")
	}
	if err := os.Remove(filepath.Join(dir, CampaignsDir, "b-copy.yml")); err != nil {
		t.Fatal(err)
	}

	// --- membership re-entry: delisted member re-enables on re-add.
	if _, err := pool.Exec(ctx, `UPDATE domain SET disabled=true, disabled_reason='delisted',
		disabled_at=now(), orphaned_at=now(), next_check_at=now()+interval '9 hours'
		WHERE host='a-one.no'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM campaign_domain"); err != nil {
		t.Fatal(err)
	}
	rep = run(t, pool, dir)
	// Three from campaign A plus keeper.yml's one, which the checkout has
	// carried since the deletion case above.
	if rep.MembershipAdds != 4 {
		t.Errorf("membership re-add count = %d, want 4", rep.MembershipAdds)
	}
	row := pool.QueryRow(ctx,
		"SELECT disabled, next_check_at <= now() FROM domain WHERE host='a-one.no'")
	var dueNow bool
	if err := row.Scan(&disabled, &dueNow); err != nil {
		t.Fatal(err)
	}
	if disabled || !dueNow {
		t.Errorf("delisted member re-entry = disabled=%t dueNow=%t, want false/true", disabled, dueNow)
	}
}

// TestSubdomainSync covers the curated-list ingress: creation, membership,
// the apex precondition, re-entry, and the parse-failure removal guard.
func TestSubdomainSync(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	// campA seeds a-one.no (created_by='campaign'), the apex the lists hang off.
	writeFixture(t, dir, "a.yml", campA)
	run(t, pool, dir)

	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	listed := func(host string) int {
		t.Helper()
		return count(`SELECT count(*) FROM curated_subdomain cs
			JOIN domain d ON d.id = cs.domain_id WHERE d.host = $1`, host)
	}

	// --- a list whose apex is not tracked: reported, and no rows created.
	writeSubdomainFixture(t, dir, "unknown-apex.no.yml", "subdomains:\n  - api\n")
	rep := run(t, pool, dir)
	if reason, ok := rep.RejectedFiles["subdomains/unknown-apex.no.yml"]; !ok ||
		!strings.Contains(reason, "not tracked") {
		t.Errorf("unknown apex should be reported: %v", rep.RejectedFiles)
	}
	if n := count("SELECT count(*) FROM domain WHERE host='api.unknown-apex.no'"); n != 0 {
		t.Errorf("unknown apex created %d rows, want 0", n)
	}
	if err := os.Remove(filepath.Join(dir, SubdomainsDir, "unknown-apex.no.yml")); err != nil {
		t.Fatal(err)
	}

	// --- a tracked apex: children created and listed, parent untouched.
	writeSubdomainFixture(t, dir, "a-one.no.yml", "subdomains:\n  - login\n  - api\n")
	rep = run(t, pool, dir)
	if rep.CuratedAdds != 2 || rep.CuratedRemoves != 0 {
		t.Fatalf("first apply = +%d/-%d, want +2/-0", rep.CuratedAdds, rep.CuratedRemoves)
	}
	var kind, createdBy string
	var parentID *int64
	if err := pool.QueryRow(ctx,
		"SELECT kind::text, created_by::text, parent_id FROM domain WHERE host='login.a-one.no'").
		Scan(&kind, &createdBy, &parentID); err != nil {
		t.Fatalf("child row: %v", err)
	}
	if kind != "subdomain" || createdBy != "curated" || parentID == nil {
		t.Errorf("child = %s/%s parent=%v, want subdomain/curated with a parent", kind, createdBy, parentID)
	}
	if err := pool.QueryRow(ctx, "SELECT created_by::text FROM domain WHERE host='a-one.no'").
		Scan(&createdBy); err != nil {
		t.Fatal(err)
	}
	if createdBy != "campaign" {
		t.Errorf("parent created_by = %s, want the original campaign origin", createdBy)
	}

	// --- idempotent re-run.
	if rep = run(t, pool, dir); rep.CuratedAdds != 0 || rep.CuratedRemoves != 0 {
		t.Errorf("re-run should be churn-free: +%d/-%d", rep.CuratedAdds, rep.CuratedRemoves)
	}

	// --- a host already known from another ingress keeps its origin.
	if _, err := pool.Exec(ctx, `INSERT INTO domain (host, kind, created_by, asn_id, country_id, tld)
		VALUES ('shop.a-one.no', 'subdomain', 'live_check',
		        (SELECT id FROM asn WHERE number=0), (SELECT id FROM country WHERE code='UN'), 'no')`); err != nil {
		t.Fatal(err)
	}
	writeSubdomainFixture(t, dir, "a-one.no.yml", "subdomains:\n  - login\n  - api\n  - shop\n")
	rep = run(t, pool, dir)
	if rep.CuratedAdds != 1 {
		t.Errorf("adding a known host = +%d, want +1", rep.CuratedAdds)
	}
	if err := pool.QueryRow(ctx, "SELECT created_by::text FROM domain WHERE host='shop.a-one.no'").
		Scan(&createdBy); err != nil {
		t.Fatal(err)
	}
	if createdBy != "live_check" {
		t.Errorf("created_by = %s, want live_check kept (membership is not provenance)", createdBy)
	}
	// That row was created parentless (its apex was unknown at the time). The
	// ingress must link it, or it stays off its parent's page and out of
	// subdomain_count — both of which key on parent_id.
	if err := pool.QueryRow(ctx,
		"SELECT kind::text, parent_id FROM domain WHERE host='shop.a-one.no'").
		Scan(&kind, &parentID); err != nil {
		t.Fatal(err)
	}
	if kind != "subdomain" || parentID == nil {
		t.Errorf("adopted host = %s parent=%v, want it linked to its apex", kind, parentID)
	}
	if n := count(`SELECT subdomain_count FROM (
		SELECT (SELECT count(*) FROM domain ch WHERE ch.parent_id = d.id AND NOT ch.disabled)
		AS subdomain_count FROM domain d WHERE d.host='a-one.no') s`); n != 3 {
		t.Errorf("subdomain_count = %d, want all 3 children visible", n)
	}

	// --- dropping an entry drops membership only; the row stays for the sweep.
	writeSubdomainFixture(t, dir, "a-one.no.yml", "subdomains:\n  - login\n  - api\n")
	rep = run(t, pool, dir)
	if rep.CuratedRemoves != 1 {
		t.Errorf("dropping an entry = -%d, want -1", rep.CuratedRemoves)
	}
	if listed("shop.a-one.no") != 0 {
		t.Error("dropped entry still listed")
	}
	if n := count("SELECT count(*) FROM domain WHERE host='shop.a-one.no' AND NOT disabled"); n != 1 {
		t.Error("sync disabled a row itself; that belongs to the lifecycle sweep")
	}

	// --- re-listing a delisted host re-enables it and makes it due.
	if _, err := pool.Exec(ctx, `UPDATE domain SET disabled=true, disabled_reason='delisted',
		disabled_at=now(), orphaned_at=now(), next_check_at=now()+interval '9 hours'
		WHERE host='shop.a-one.no'`); err != nil {
		t.Fatal(err)
	}
	writeSubdomainFixture(t, dir, "a-one.no.yml", "subdomains:\n  - login\n  - api\n  - shop\n")
	run(t, pool, dir)
	var disabled, dueNow bool
	if err := pool.QueryRow(ctx,
		"SELECT disabled, next_check_at <= now() FROM domain WHERE host='shop.a-one.no'").
		Scan(&disabled, &dueNow); err != nil {
		t.Fatal(err)
	}
	if disabled || !dueNow {
		t.Errorf("re-listed host = disabled=%t dueNow=%t, want false/true", disabled, dueNow)
	}

	// --- a disabled apex: the list is skipped, but its hosts keep membership.
	// Disabling a parent must not silently start its children's delist grace.
	mustExecTest(t, pool, "UPDATE domain SET disabled=true, disabled_reason='manual' WHERE host='a-one.no'")
	rep = run(t, pool, dir)
	if reason, ok := rep.RejectedFiles["subdomains/a-one.no.yml"]; !ok ||
		!strings.Contains(reason, "disabled") {
		t.Errorf("disabled apex should be reported: %v", rep.RejectedFiles)
	}
	if rep.CuratedRemoves != 0 || listed("login.a-one.no") != 1 || listed("shop.a-one.no") != 1 {
		t.Errorf("disabled apex unlisted its children: -%d", rep.CuratedRemoves)
	}
	mustExecTest(t, pool, `UPDATE domain SET disabled=false, disabled_reason=NULL WHERE host='a-one.no'`)

	// --- two files claiming one apex: neither is applied, and removals are
	// suspended, so whichever file sorts first cannot supersede the other.
	writeSubdomainFixture(t, dir, "a-one.no.yaml", "subdomains:\n  - extra\n")
	rep = run(t, pool, dir)
	for _, name := range []string{"subdomains/a-one.no.yml", "subdomains/a-one.no.yaml"} {
		if reason, ok := rep.RejectedFiles[name]; !ok || !strings.Contains(reason, "more than one file") {
			t.Errorf("both claimants should be rejected, %s was not: %v", name, rep.RejectedFiles)
		}
	}
	if rep.CuratedRemoves != 0 {
		t.Errorf("removals ran with a duplicate apex present: -%d", rep.CuratedRemoves)
	}
	if !rep.CuratedFrozen {
		t.Error("a suspended removal diff must be visible in the report")
	}
	if listed("login.a-one.no") != 1 {
		t.Error("a duplicate-apex rejection unlisted the established file's hosts")
	}
	if n := count("SELECT count(*) FROM domain WHERE host='extra.a-one.no'"); n != 0 {
		t.Error("a rejected duplicate list still created rows")
	}
	if err := os.Remove(filepath.Join(dir, SubdomainsDir, "a-one.no.yaml")); err != nil {
		t.Fatal(err)
	}

	// --- a file that fails to parse must not unlist anything.
	writeSubdomainFixture(t, dir, "a-two.no.yml", "subdomains:\n  - www\n")
	rep = run(t, pool, dir)
	if _, ok := rep.RejectedFiles["subdomains/a-two.no.yml"]; !ok {
		t.Errorf("broken list should be reported: %v", rep.RejectedFiles)
	}
	if rep.CuratedRemoves != 0 {
		t.Errorf("removals ran with a broken list present: -%d", rep.CuratedRemoves)
	}
	if listed("login.a-one.no") != 1 || listed("shop.a-one.no") != 1 {
		t.Error("a broken list unlisted a healthy list's hosts")
	}

	// --- fixing the repo lets removals resume.
	if err := os.Remove(filepath.Join(dir, SubdomainsDir, "a-two.no.yml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, SubdomainsDir, "a-one.no.yml")); err != nil {
		t.Fatal(err)
	}
	rep = run(t, pool, dir)
	if rep.CuratedRemoves != 3 {
		t.Errorf("emptied directory = -%d, want -3", rep.CuratedRemoves)
	}
	if n := count("SELECT count(*) FROM curated_subdomain"); n != 0 {
		t.Errorf("%d membership rows survived an emptied directory", n)
	}
}

// TestCampaignSyncRealRepo runs the full sync over the live campaign-repo
// checkout (P1.8 acceptance: ~30k entities with correct parents; the files
// carry production uuids, which the sync adopts). Skipped when the checkout
// is absent.
func TestCampaignSyncRealRepo(t *testing.T) {
	repo := os.Getenv("CAMPAIGN_REPO_PATH")
	if repo == "" {
		repo = filepath.Join(os.Getenv("HOME"), "code", "go", "src", "github.com", "lasseh", "whynoipv6-campaign")
	}
	campaigns := filepath.Join(repo, CampaignsDir)
	entries, err := os.ReadDir(campaigns)
	if err != nil {
		t.Skipf("campaign repo checkout not available: %v", err)
	}
	// Copy to a temp dir so write-back cannot touch the real checkout.
	dir := t.TempDir()
	for _, e := range entries {
		if e.IsDir() || (filepath.Ext(e.Name()) != ".yml" && filepath.Ext(e.Name()) != ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(campaigns, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, dir, e.Name(), string(raw))
	}

	pool := pgtest.NewDB(t)
	ctx := context.Background()
	// The live repo carries one large file (the 1729-entry Dutch
	// central-government file); the cap is config
	// (campaign.max_domains_per_file, default 5000) and is set explicitly here.
	rep, err := Sync(context.Background(), Config{
		RepoPath: dir, GitRemote: "origin", MaxDomainsPerFile: 5000,
		Pull: false, Push: false,
	}, pool)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if len(rep.RejectedFiles) > 0 {
		t.Errorf("rejected files on the real repo: %v", rep.RejectedFiles)
	}
	var entities, memberships, subdomains, badParents int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM domain),
		(SELECT count(*) FROM campaign_domain),
		(SELECT count(*) FROM domain WHERE kind='subdomain'),
		(SELECT count(*) FROM domain WHERE kind='subdomain' AND parent_id IS NULL)`).
		Scan(&entities, &memberships, &subdomains, &badParents); err != nil {
		t.Fatal(err)
	}
	t.Logf("real repo: %d entities, %d memberships, %d subdomains, %d rejected hosts",
		entities, memberships, subdomains, len(rep.RejectedHosts))
	// The spec's "~30k entities" figure predates repo cleanups; the live
	// repo measures ~4.5k domain entries. Assert order-of-magnitude sanity.
	if entities < 2000 {
		t.Errorf("expected thousands of entities, got %d", entities)
	}
	if badParents != 0 {
		t.Errorf("%d subdomains without parent_id", badParents)
	}
	var badCreated int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM domain WHERE created_by NOT IN ('parent_link','campaign')").Scan(&badCreated); err != nil {
		t.Fatal(err)
	}
	if badCreated != 0 {
		t.Errorf("%d entities with unexpected created_by", badCreated)
	}
}
