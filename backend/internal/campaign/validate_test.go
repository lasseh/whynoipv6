package campaign

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo builds a throwaway campaign checkout with a main branch.
type gitRepo struct {
	t   *testing.T
	dir string
}

func newGitRepo(t *testing.T) *gitRepo {
	t.Helper()
	r := &gitRepo{t: t, dir: t.TempDir()}
	r.run("init", "-b", "main")
	r.run("config", "user.email", "test@example.invalid")
	r.run("config", "user.name", "test")
	return r
}

func (r *gitRepo) run(args ...string) {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (r *gitRepo) write(name, content string) {
	r.t.Helper()
	path := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *gitRepo) rm(name string) {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.dir, name)); err != nil {
		r.t.Fatal(err)
	}
}

func (r *gitRepo) commitAll(msg string) {
	r.t.Helper()
	r.run("add", "-A")
	r.run("commit", "-m", msg)
}

const baseCampaign = `title: Nordic Banks
description: Banks in the north
uuid: 11111111-2222-3333-4444-555555555555
domains:
  - bank.no
  - sparebank.no
`

const otherCampaign = `title: Telcos
description: Norwegian telcos
domains:
  - telenor.no
  - bank.no
`

// TestCampaignValidate (P7.1 / 06 §4.2): every blocking check on fixture
// PRs; cross-file duplicates never block.
func TestCampaignValidate(t *testing.T) {
	ctx := context.Background()

	setup := func() *gitRepo {
		r := newGitRepo(t)
		r.write("nordic-banks.yml", baseCampaign)
		r.write("telcos.yml", otherCampaign)
		r.commitAll("seed")
		r.run("checkout", "-b", "pr")
		return r
	}

	t.Run("added_file_with_uuid_fails", func(t *testing.T) {
		r := setup()
		r.write("new.yml", "title: New\ndescription: d\nuuid: 99999999-8888-7777-6666-555555555555\ndomains:\n  - example.no\n")
		r.commitAll("add with uuid")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		if res.OK() || !strings.Contains(strings.Join(res.Failures, "\n"), "assigned by the import bot") {
			t.Errorf("failures = %v", res.Failures)
		}
	})

	t.Run("modified_uuid_fails", func(t *testing.T) {
		r := setup()
		r.write("nordic-banks.yml", strings.Replace(baseCampaign,
			"11111111-2222-3333-4444-555555555555", "99999999-8888-7777-6666-555555555555", 1))
		r.commitAll("mutate uuid")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		if res.OK() {
			t.Error("mutated uuid must fail")
		}
	})

	t.Run("rename_preserving_uuid_passes", func(t *testing.T) {
		r := setup()
		r.rm("nordic-banks.yml")
		r.write("norse-banks.yml", baseCampaign)
		r.commitAll("rename")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		if !res.OK() {
			t.Errorf("rename must pass, failures = %v", res.Failures)
		}
		if !strings.Contains(res.Comment, "rename detected") ||
			!strings.Contains(res.Comment, "nordic-banks.yml") {
			t.Errorf("comment must announce the rename:\n%s", res.Comment)
		}
	})

	t.Run("within_file_duplicate_fails_with_lines", func(t *testing.T) {
		r := setup()
		r.write("dup.yml", "title: Dup\ndescription: d\ndomains:\n  - bank.se\n  - other.se\n  - BANK.se\n")
		r.commitAll("dup")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(res.Failures, "\n")
		if res.OK() || !strings.Contains(joined, "dup.yml:6") || !strings.Contains(joined, "first at line 4") {
			t.Errorf("failures = %v", res.Failures)
		}
	})

	t.Run("oversize_file_fails", func(t *testing.T) {
		r := setup()
		var b strings.Builder
		b.WriteString("title: Big\ndescription: d\ndomains:\n")
		for i := 0; i < 1001; i++ {
			fmt.Fprintf(&b, "  - host%d.no\n", i)
		}
		r.write("big.yml", b.String())
		r.commitAll("big")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		if res.OK() || !strings.Contains(strings.Join(res.Failures, "\n"), "cap is 1000") {
			t.Errorf("failures = %v", res.Failures)
		}
	})

	t.Run("bad_hostname_and_public_suffix_fail", func(t *testing.T) {
		r := setup()
		r.write("bad.yml", "title: Bad\ndescription: d\ndomains:\n  - ok.no\n  - not_a_host!\n  - co.uk\n")
		r.commitAll("bad hosts")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(res.Failures, "\n")
		if res.OK() || !strings.Contains(joined, "bad.yml:5") || !strings.Contains(joined, "bad.yml:6") {
			t.Errorf("failures = %v", res.Failures)
		}
	})

	t.Run("cross_file_duplicate_never_blocks", func(t *testing.T) {
		r := setup()
		r.write("more.yml", "title: More\ndescription: d\ndomains:\n  - bank.no\n")
		r.commitAll("cross dup")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		if !res.OK() {
			t.Fatalf("cross-file duplicates must never block: %v", res.Failures)
		}
		if !strings.Contains(res.Comment, "also in") {
			t.Errorf("comment must carry the informational membership line:\n%s", res.Comment)
		}
	})

	t.Run("unknown_key_fails", func(t *testing.T) {
		r := setup()
		r.write("extra.yml", "title: X\ndescription: d\nowner: someone\ndomains:\n  - x.no\n")
		r.commitAll("extra key")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		if res.OK() {
			t.Error("unknown keys must fail the schema check")
		}
	})

	t.Run("local_mode_skips_uuid_rule", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "a.yml"),
			[]byte("title: A\ndescription: d\nuuid: 11111111-2222-3333-4444-555555555555\ndomains:\n  - a.no\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := Validate(context.Background(), dir, "", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		if !res.OK() {
			t.Errorf("local mode must not apply the uuid diff rule: %v", res.Failures)
		}
	})
}
