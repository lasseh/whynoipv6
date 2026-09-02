package campaign

import (
	"os"
	"path/filepath"
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
