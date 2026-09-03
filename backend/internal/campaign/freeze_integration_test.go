//go:build integration

package campaign

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// runForce is run() with the empty-checkout guard lifted.
func runForce(t *testing.T, pool *pgxpool.Pool, dir string) (*Report, error) {
	t.Helper()
	return Sync(context.Background(), Config{
		RepoPath: dir, GitRemote: "origin", MaxDomainsPerFile: 1000,
		MaxSubdomainsPerDomain: 20, Force: true,
	}, pool)
}

// TestCampaignRejectedFileFreezesDisable is review issue 29's first rule: a
// campaign whose file is rejected is unreadable, not absent. Before this,
// one bad edit to a YAML file — a typo'd key, an oversize list — dropped the
// file from the parsed set, so step 5's uuid-set soft delete disabled the
// campaign it described, and the frontend lost it until someone fixed the
// typo.
func TestCampaignRejectedFileFreezesDisable(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	writeFixture(t, dir, "a.yml", campA)
	writeFixture(t, dir, "keeper.yml", campKeeper)
	run(t, pool, dir)

	raw, err := os.ReadFile(filepath.Join(dir, "a.yml"))
	if err != nil {
		t.Fatal(err)
	}
	id := ""
	for _, l := range strings.Split(string(raw), "\n") {
		if after, ok := strings.CutPrefix(l, "uuid: "); ok {
			id = after
		}
	}
	if id == "" {
		t.Fatal("no uuid written back")
	}

	// Break the file in a way ParseFile refuses, keeping the uuid line
	// readable — which is the case rawFileUUID exists for.
	writeFixture(t, dir, "a.yml", "uuid: "+id+"\ntitle: Campaign A\nnot_a_key: x\ndomains:\n    - a-one.no\n")
	rep := run(t, pool, dir)

	if len(rep.RejectedFiles) != 1 {
		t.Fatalf("rejected files = %v, want the broken one", rep.RejectedFiles)
	}
	if len(rep.Disabled) != 0 {
		t.Errorf("disabled %v: a rejected file is not a deleted one", rep.Disabled)
	}
	if !slices.Contains(rep.DisableFrozen, "Campaign A") {
		t.Errorf("DisableFrozen = %v, want Campaign A", rep.DisableFrozen)
	}
	var disabled bool
	if err := pool.QueryRow(ctx, "SELECT disabled FROM campaign WHERE uuid=$1", id).Scan(&disabled); err != nil {
		t.Fatal(err)
	}
	if disabled {
		t.Error("the campaign was disabled over a rejected file")
	}
}

// TestCampaignMalformedUUIDFreezesDisable is the same rule with the uuid line
// itself broken — a truncated uuid, which is the likeliest hand-edit error in
// a campaign file and a reason ParseFile rejects one. rawFileUUID's regex is
// deliberately looser than ParseFile's, so it reads the bad value straight
// back out; parsing it as a uuid there panics the sync, and with it the
// crawler, since nothing recovers around the tick. The freeze has to fall
// through to source_file instead.
func TestCampaignMalformedUUIDFreezesDisable(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	writeFixture(t, dir, "a.yml", campA)
	writeFixture(t, dir, "keeper.yml", campKeeper)
	run(t, pool, dir)

	writeFixture(t, dir, "a.yml", "uuid: 8f14e45f-ceea-467a-9dfb\n"+campA)
	rep := run(t, pool, dir)

	if _, ok := rep.RejectedFiles["a.yml"]; !ok {
		t.Fatalf("rejected files = %v, want the truncated uuid", rep.RejectedFiles)
	}
	if !slices.Contains(rep.DisableFrozen, "Campaign A") {
		t.Errorf("DisableFrozen = %v, want Campaign A", rep.DisableFrozen)
	}
	var disabled bool
	if err := pool.QueryRow(ctx,
		"SELECT disabled FROM campaign WHERE source_file='a.yml'").Scan(&disabled); err != nil {
		t.Fatal(err)
	}
	if disabled {
		t.Error("the campaign was disabled over a file whose uuid line is malformed")
	}
}

// TestCampaignAllHostsRejectedFreezesRemoval is the second rule: a file that
// listed hosts and produced none is a bad edit, not an emptied campaign.
// Removing every member over it would unlist the whole campaign.
func TestCampaignAllHostsRejectedFreezesRemoval(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	writeFixture(t, dir, "a.yml", campA)
	writeFixture(t, dir, "keeper.yml", campKeeper)
	run(t, pool, dir)

	var before int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM campaign_domain").Scan(&before); err != nil {
		t.Fatal(err)
	}

	// Every entry fails canonicalization: no public suffix.
	writeFixture(t, dir, "a.yml", `title: Campaign A
description: First fixture campaign.
domains:
    - not a host
    - also/not/a/host
`)
	rep := run(t, pool, dir)

	if rep.MembershipRemoves != 0 {
		t.Errorf("removed %d members over a file whose hosts all failed validation", rep.MembershipRemoves)
	}
	if len(rep.DisableFrozen) == 0 {
		t.Error("an all-rejected file must report the freeze")
	}
	var after int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM campaign_domain").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("memberships %d → %d: a bad edit unlisted the campaign", before, after)
	}
}

// TestCampaignEmptyCheckoutAborts is the third rule: nothing parsed while
// the database has enabled campaigns is a broken clone or a wrong
// repo_path, and step 5 would soft-delete every campaign. --force is the
// operator's way past it.
func TestCampaignEmptyCheckoutAborts(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	writeFixture(t, dir, "a.yml", campA)
	writeFixture(t, dir, "keeper.yml", campKeeper)
	run(t, pool, dir)

	for _, name := range []string{"a.yml", "keeper.yml"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}

	_, err := Sync(ctx, Config{
		RepoPath: dir, GitRemote: "origin", MaxDomainsPerFile: 1000, MaxSubdomainsPerDomain: 20,
	}, pool)
	if err == nil {
		t.Fatal("an empty checkout was accepted")
	}
	if !strings.Contains(err.Error(), "checkout is empty") {
		t.Errorf("err = %v, want the empty-checkout refusal", err)
	}
	var enabled int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM campaign WHERE NOT disabled").Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 2 {
		t.Errorf("enabled campaigns = %d, want both still enabled", enabled)
	}

	// --force is the deliberate path, and it does disable them.
	if _, err := runForce(t, pool, dir); err != nil {
		t.Fatalf("--force: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM campaign WHERE NOT disabled").Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Errorf("enabled campaigns after --force = %d, want 0", enabled)
	}
}

// An empty checkout with nothing to lose is not an error: a first run
// against a fresh database has no campaigns to disable.
func TestCampaignEmptyCheckoutOnEmptyDB(t *testing.T) {
	pool := pgtest.NewDB(t)
	if _, err := Sync(context.Background(), Config{
		RepoPath: t.TempDir(), GitRemote: "origin",
		MaxDomainsPerFile: 1000, MaxSubdomainsPerDomain: 20,
	}, pool); err != nil {
		t.Errorf("empty checkout against an empty DB: %v", err)
	}
}
