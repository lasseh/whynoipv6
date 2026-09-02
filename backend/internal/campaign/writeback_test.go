package campaign

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInsertUUIDLine pins the write-back splice (§3.3 step 6): the uuid
// lands after the description whatever shape the description takes, an
// empty `uuid:` placeholder is filled in place, an assigned uuid is left
// alone, and a splice that would not read back is refused.
func TestInsertUUIDLine(t *testing.T) {
	const id = "3b3b3b3b-4444-4444-4444-444444444444"
	cases := []struct {
		name string
		in   string
		want bool // written?
	}{
		{"plain description", "title: A\ndescription: one line\ndomains:\n  - example.no\n", true},
		{"block scalar with a blank line", "title: A\ndescription: |\n  para one\n\n  para two\ndomains:\n  - example.no\n", true},
		{"folded scalar with trailing blank lines", "title: A\ndescription: >\n  folded\n  text\n\n\ndomains:\n  - example.no\n", true},
		{"empty uuid placeholder is filled", "title: A\nuuid:\ndescription: x\ndomains:\n  - example.no\n", true},
		{"assigned uuid is left alone", "title: A\nuuid: 11111111-2222-3333-4444-555555555555\ndescription: x\ndomains:\n  - example.no\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "c.yml")
			if err := os.WriteFile(path, []byte(tc.in), 0o644); err != nil {
				t.Fatal(err)
			}
			ok, err := insertUUIDLine(path, id)
			if err != nil {
				t.Fatalf("insertUUIDLine: %v", err)
			}
			if ok != tc.want {
				t.Fatalf("written = %t, want %t", ok, tc.want)
			}
			f, err := ParseFile(path, 100)
			if err != nil {
				t.Fatalf("file no longer parses: %v", err)
			}
			if tc.want && f.UUID != id {
				t.Errorf("uuid after splice = %q, want %q", f.UUID, id)
			}
			if !tc.want && f.UUID == id {
				t.Error("an assigned uuid was overwritten")
			}
		})
	}

	t.Run("no description line is an error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "c.yml")
		if err := os.WriteFile(path, []byte("title: A\ndomains:\n  - example.no\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := insertUUIDLine(path, id); err == nil {
			t.Error("want an error for a file without description:")
		}
	})
}

// TestGitRemoteShape: the remote reaches git as an argv position.
func TestGitRemoteShape(t *testing.T) {
	for _, ok := range []string{"origin", "upstream", "github/main", "deploy.key-1"} {
		if !gitRemoteRe.MatchString(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "-origin", "--upload-pack=x", "https://x-access-token:T@github.com/o/r.git", "git@github.com:o/r.git"} {
		if gitRemoteRe.MatchString(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

// gitRepoPair builds a bare "remote" and a clone of it holding one campaign
// file with no uuid, and returns the clone's path plus a function that
// breaks the remote.
func gitRepoPair(t *testing.T) (work string, breakRemote func()) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work = filepath.Join(root, "work")

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run(root, "init", "--bare", "--initial-branch=main", remote)
	run(root, "clone", remote, work)
	run(work, "config", "user.email", "bot@example.invalid")
	run(work, "config", "user.name", "bot")

	if err := os.WriteFile(filepath.Join(work, "c.yml"),
		[]byte("title: A\ndescription: x\ndomains:\n  - example.no\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(work, "add", "c.yml")
	run(work, "commit", "-m", "seed")
	run(work, "push", "-u", "origin", "main")

	return work, func() {
		if err := os.RemoveAll(remote); err != nil {
			t.Fatal(err)
		}
	}
}

// TestWriteBackRollsBackOnPushFailure (issue 28): once insertUUIDLine has
// run, §3.3 step 6's "step 4's reuse rule recovers on the next run" no
// longer holds — the file carries its uuid, so the next run finds nothing
// pending and never retries, while the stranded local commit makes every
// later `pull --ff-only` abort the whole sync. A failed push must therefore
// leave the checkout level with the remote.
func TestWriteBackRollsBackOnPushFailure(t *testing.T) {
	const id = "3b3b3b3b-4444-4444-4444-444444444444"
	work, breakRemote := gitRepoPair(t)
	breakRemote()

	cfg := Config{RepoPath: work, GitRemote: "origin", Push: true}
	got := writeBackUUIDs(context.Background(), cfg, map[string]string{
		filepath.Join(work, "c.yml"): id,
	})
	if !strings.Contains(got, "failed: push") {
		t.Fatalf("write-back = %q, want a push failure", got)
	}
	if strings.Contains(got, "rollback failed") {
		t.Fatalf("write-back = %q, want a clean rollback", got)
	}

	// The file is uuid-less again, so the next run mints the same uuid from
	// the database and retries the write-back.
	f, err := ParseFile(filepath.Join(work, "c.yml"), 100)
	if err != nil {
		t.Fatal(err)
	}
	if f.UUID != "" {
		t.Errorf("uuid %q survived the rollback; the next run will find nothing pending", f.UUID)
	}

	gitOut := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if ahead := gitOut("rev-list", "@{u}..HEAD"); ahead != "" {
		t.Errorf("checkout is ahead of the remote by %q; the next pull --ff-only aborts", ahead)
	}
	if dirty := gitOut("status", "--porcelain"); dirty != "" {
		t.Errorf("checkout left dirty: %q", dirty)
	}
	if rebaseInProgress(work) {
		t.Error("a rebase was left in progress")
	}
}
