package campaign

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseSubdomainFile covers the file-naming rules and every entry rule:
// relative labels only, no bare www, normalization, dedupe, cap.
func TestParseSubdomainFile(t *testing.T) {
	tests := []struct {
		name      string
		file      string // filename inside the temp subdomains dir
		body      string
		wantHosts []string
		wantErr   string // substring; "" means the file must parse
	}{
		{
			name:      "labels join to the apex",
			file:      "nrk.no.yml",
			body:      "subdomains:\n  - tv\n  - radio\n  - api\n",
			wantHosts: []string{"tv.nrk.no", "radio.nrk.no", "api.nrk.no"},
		},
		{
			name:      "multi-level labels are allowed",
			file:      "nrk.no.yml",
			body:      "subdomains:\n  - secure.login\n",
			wantHosts: []string{"secure.login.nrk.no"},
		},
		{
			name:      "entries are canonicalized",
			file:      "nrk.no.yml",
			body:      "subdomains:\n  - TV\n  -  radio \n",
			wantHosts: []string{"tv.nrk.no", "radio.nrk.no"},
		},
		{
			name:      "unicode labels become punycode",
			file:      "nrk.no.yml",
			body:      "subdomains:\n  - café\n",
			wantHosts: []string{"xn--caf-dma.nrk.no"},
		},
		{
			name:      "yaml extension is accepted",
			file:      "nrk.no.yaml",
			body:      "subdomains:\n  - tv\n",
			wantHosts: []string{"tv.nrk.no"},
		},
		{
			name: "an empty list is valid",
			file: "nrk.no.yml",
			body: "subdomains: []\n",
		},
		{
			name: "a comments-only file is an empty list",
			file: "nrk.no.yml",
			body: "# nothing listed yet\n",
		},
		{
			name:      "www.something is not the bare www",
			file:      "nrk.no.yml",
			body:      "subdomains:\n  - www.tv\n",
			wantHosts: []string{"www.tv.nrk.no"},
		},
		{
			name:    "bare www is redundant",
			file:    "nrk.no.yml",
			body:    "subdomains:\n  - www\n",
			wantErr: "redundant",
		},
		{
			name:    "uppercase www is redundant too",
			file:    "nrk.no.yml",
			body:    "subdomains:\n  - WWW\n",
			wantErr: "redundant",
		},
		{
			name:    "a full host is not a relative label",
			file:    "nrk.no.yml",
			body:    "subdomains:\n  - api.nrk.no\n",
			wantErr: "write the label relative",
		},
		{
			name:    "the apex itself is not a relative label",
			file:    "nrk.no.yml",
			body:    "subdomains:\n  - nrk.no\n",
			wantErr: "write the label relative",
		},
		{
			name:    "duplicates after normalization",
			file:    "nrk.no.yml",
			body:    "subdomains:\n  - tv\n  - TV\n",
			wantErr: "duplicate entry",
		},
		{
			name:    "underscores are not valid labels",
			file:    "nrk.no.yml",
			body:    "subdomains:\n  - api_v2\n",
			wantErr: "api_v2",
		},
		{
			name:    "empty entries are rejected",
			file:    "nrk.no.yml",
			body:    "subdomains:\n  - \"\"\n",
			wantErr: "empty entry",
		},
		{
			name:    "over the cap",
			file:    "nrk.no.yml",
			body:    "subdomains:\n  - a\n  - b\n  - c\n  - d\n",
			wantErr: "cap is 3",
		},
		{
			name:    "unknown keys are rejected",
			file:    "nrk.no.yml",
			body:    "subdomains:\n  - tv\nnote: hello\n",
			wantErr: "field note not found",
		},
		{
			name:    "the filename must be an apex, not a subdomain",
			file:    "api.nrk.no.yml",
			body:    "subdomains:\n  - tv\n",
			wantErr: "must name the registrable apex",
		},
		{
			name:    "the filename must be lowercase punycode",
			file:    "NRK.no.yml",
			body:    "subdomains:\n  - tv\n",
			wantErr: "must be lowercase punycode",
		},
		{
			name:    "the filename must not be a public suffix",
			file:    "co.uk.yml",
			body:    "subdomains:\n  - tv\n",
			wantErr: "is a suffix",
		},
		{
			name:    "an unknown TLD has no registrable apex",
			file:    "example.invalidtld999.yml",
			body:    "subdomains:\n  - tv\n",
			wantErr: "invalidtld999",
		},
	}

	const cap = 3
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tt.file)
			if err := os.WriteFile(path, []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}
			f, err := ParseSubdomainFile(path, cap)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parsed %+v, want error containing %q", f, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := strings.Join(f.Hosts, ","); got != strings.Join(tt.wantHosts, ",") {
				t.Errorf("hosts = %v, want %v", f.Hosts, tt.wantHosts)
			}
			if f.Path != filepath.Join(SubdomainsDir, tt.file) {
				t.Errorf("path = %q, want it repo-relative", f.Path)
			}
		})
	}
}

// TestListSubdomainFiles: a repo without the directory is not an error.
func TestListSubdomainFiles(t *testing.T) {
	dir := t.TempDir()
	files, err := ListSubdomainFiles(dir)
	if err != nil || files != nil {
		t.Fatalf("missing dir = %v/%v, want nil/nil", files, err)
	}

	if err := os.Mkdir(filepath.Join(dir, SubdomainsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"b.no.yml", "a.no.yaml", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, SubdomainsDir, n), []byte("subdomains: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err = ListSubdomainFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	if strings.Join(names, ",") != "a.no.yaml,b.no.yml" {
		t.Errorf("files = %v, want the sorted YAML pair", names)
	}
}

// TestValidateSubdomains: the PR validator reports subdomain failures in the
// same file:line form as the campaign checks, and passes clean lists.
func TestValidateSubdomains(t *testing.T) {
	ctx := context.Background()

	setup := func(t *testing.T) *gitRepo {
		t.Helper()
		r := newGitRepo(t)
		r.write("nordic-banks.yml", baseCampaign)
		r.commitAll("seed")
		r.run("checkout", "-b", "pr")
		return r
	}

	t.Run("clean list passes", func(t *testing.T) {
		r := setup(t)
		r.write("subdomains/bank.no.yml", "subdomains:\n  - login\n  - api\n")
		r.commitAll("add list")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		if !res.OK() {
			t.Fatalf("failures = %v", res.Failures)
		}
		if !strings.Contains(res.Comment, "2 subdomain(s) of `bank.no`") {
			t.Errorf("comment missing the list summary:\n%s", res.Comment)
		}
	})

	t.Run("bad entry names its line", func(t *testing.T) {
		r := setup(t)
		r.write("subdomains/bank.no.yml", "subdomains:\n  - login\n  - www\n")
		r.commitAll("add list")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(res.Failures, "\n")
		if res.OK() || !strings.Contains(joined, "subdomains/bank.no.yml:3:") {
			t.Errorf("failures = %v, want one at line 3", res.Failures)
		}
	})

	// The canonical-filename rule stops two spellings of one apex, but not
	// two extensions, so the validator compares against the whole repo head.
	t.Run("two files for one domain fail", func(t *testing.T) {
		r := setup(t)
		r.write("subdomains/bank.no.yml", "subdomains:\n  - login\n")
		r.commitAll("first list")
		r.run("checkout", "-b", "pr2")
		r.write("subdomains/bank.no.yaml", "subdomains:\n  - api\n")
		r.commitAll("second list for the same domain")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(res.Failures, "\n")
		if res.OK() || !strings.Contains(joined, "one file per domain") {
			t.Errorf("failures = %v, want a duplicate-domain failure", res.Failures)
		}
	})

	// The other direction: the existing list is the one that sorts LAST, so
	// the new spelling is the lexicographically first claimant. An
	// order-dependent check missed exactly this case.
	t.Run("two files for one domain fail whichever sorts first", func(t *testing.T) {
		r := setup(t)
		r.write("subdomains/bank.no.yml", "subdomains:\n  - login\n")
		r.commitAll("first list")
		r.run("checkout", "-b", "pr3")
		r.write("subdomains/bank.no.yaml", "subdomains:\n  - api\n") // ".yaml" < ".yml"
		r.commitAll("second list, sorting before the first")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		if res.OK() || !strings.Contains(strings.Join(res.Failures, "\n"), "one file per domain") {
			t.Errorf("failures = %v, want a duplicate-domain failure", res.Failures)
		}
	})

	t.Run("deleting a list is allowed", func(t *testing.T) {
		r := setup(t)
		r.write("subdomains/bank.no.yml", "subdomains:\n  - login\n")
		r.commitAll("add list")
		r.run("checkout", "-b", "pr2")
		if err := os.Remove(filepath.Join(r.dir, "subdomains", "bank.no.yml")); err != nil {
			t.Fatal(err)
		}
		r.commitAll("drop list")
		res, err := Validate(ctx, r.dir, "main", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		if !res.OK() {
			t.Errorf("failures = %v", res.Failures)
		}
	})

	t.Run("local mode walks every list", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, SubdomainsDir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, SubdomainsDir, "bank.no.yml"),
			[]byte("subdomains:\n  - api.bank.no\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := Validate(ctx, dir, "", 1000, 20)
		if err != nil {
			t.Fatal(err)
		}
		if res.OK() || !strings.Contains(strings.Join(res.Failures, "\n"), "write the label relative") {
			t.Errorf("failures = %v", res.Failures)
		}
	})
}
