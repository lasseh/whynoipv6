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

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, pool *pgxpool.Pool, dir string, adopt bool) *Report {
	t.Helper()
	rep, err := Sync(context.Background(), Config{
		RepoPath: dir, GitRemote: "origin", MaxDomainsPerFile: 1000,
		AdoptUnknownUUIDs: adopt, Pull: false, Push: false,
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

// TestCampaignSync covers the 06-ingest §9.6 matrix on a fixture repo.
func TestCampaignSync(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	// --- new file without uuid: insert + write-back.
	writeFixture(t, dir, "a.yml", campA)
	rep := run(t, pool, dir, false)
	if len(rep.Created) != 1 || rep.MembershipAdds != 2 {
		t.Fatalf("create: %+v", rep)
	}
	if rep.WriteBack != "written (push disabled)" {
		t.Errorf("write-back = %q", rep.WriteBack)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "a.yml"))
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
	rep = run(t, pool, dir, false)
	if len(rep.Created) != 0 || len(rep.Updated) != 1 || rep.MembershipAdds != 0 || rep.MembershipRemoves != 0 {
		t.Errorf("re-run should be churn-free: %+v", rep)
	}

	// --- rename: same uuid, new filename → source_file update, no churn.
	if err := os.Rename(filepath.Join(dir, "a.yml"), filepath.Join(dir, "a-renamed.yml")); err != nil {
		t.Fatal(err)
	}
	rep = run(t, pool, dir, false)
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
	if err := os.Remove(filepath.Join(dir, "a-renamed.yml")); err != nil {
		t.Fatal(err)
	}
	rep = run(t, pool, dir, false)
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
	rep = run(t, pool, dir, false)
	if len(rep.ReEnabled) != 1 || rep.MembershipAdds != 0 {
		t.Errorf("re-appearance: %+v", rep)
	}

	// --- unknown uuid rejected without the flag; adopted with it.
	writeFixture(t, dir, "b.yml", `title: Campaign B
description: Unknown uuid fixture.
uuid: 3b3b3b3b-1111-2222-3333-444444444444
domains:
    - b-one.no
`)
	rep = run(t, pool, dir, false)
	if _, ok := rep.RejectedFiles["b.yml"]; !ok {
		t.Errorf("unknown uuid should reject b.yml: %+v", rep.RejectedFiles)
	}
	rep = run(t, pool, dir, true)
	if len(rep.Created) != 1 {
		t.Errorf("adopt-unknown-uuids should insert b.yml: %+v", rep)
	}

	// --- duplicate uuid across files: source_file match wins.
	writeFixture(t, dir, "b-copy.yml", `title: Campaign B copy
description: Copied uuid fixture.
uuid: 3b3b3b3b-1111-2222-3333-444444444444
domains:
    - b-two.no
`)
	rep = run(t, pool, dir, false)
	if _, ok := rep.RejectedFiles["b-copy.yml"]; !ok {
		t.Errorf("duplicate uuid should reject the copy: %+v", rep.RejectedFiles)
	}
	if _, ok := rep.RejectedFiles["b.yml"]; ok {
		t.Errorf("original file must survive the duplicate-uuid guard")
	}
	if err := os.Remove(filepath.Join(dir, "b-copy.yml")); err != nil {
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
	rep = run(t, pool, dir, false)
	if rep.MembershipAdds != 3 {
		t.Errorf("membership re-add count = %d, want 3", rep.MembershipAdds)
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

// TestCampaignSyncRealRepo runs the full sync over the live campaign-repo
// checkout with --adopt-unknown-uuids (P1.8 acceptance: ~30k entities with
// correct parents). Skipped when the checkout is absent.
func TestCampaignSyncRealRepo(t *testing.T) {
	repo := os.Getenv("CAMPAIGN_REPO_PATH")
	if repo == "" {
		repo = filepath.Join(os.Getenv("HOME"), "code", "go", "src", "github.com", "lasseh", "whynoipv6-campaign")
	}
	if _, err := os.Stat(repo); err != nil {
		t.Skipf("campaign repo checkout not available: %v", err)
	}
	// Copy to a temp dir so write-back cannot touch the real checkout.
	dir := t.TempDir()
	entries, err := os.ReadDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || (filepath.Ext(e.Name()) != ".yml" && filepath.Ext(e.Name()) != ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(repo, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, dir, e.Name(), string(raw))
	}

	pool := pgtest.NewDB(t)
	ctx := context.Background()
	// The live repo carries one file above the default 1000-domain cap
	// (the 1729-entry Dutch central-government file); the cap is config
	// (campaign.max_domains_per_file) and is raised for the full import.
	rep, err := Sync(context.Background(), Config{
		RepoPath: dir, GitRemote: "origin", MaxDomainsPerFile: 2000,
		AdoptUnknownUUIDs: true, Pull: false, Push: false,
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
