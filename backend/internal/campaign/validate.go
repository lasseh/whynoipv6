package campaign

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lasseh/whynoipv6/internal/domain"
)

// ValidateResult is the `v6ctl campaign validate` outcome (06 §4.3): the
// blocking failures (file:line: message) and the bot-comment Markdown.
type ValidateResult struct {
	Failures []string
	Comment  string
}

// OK reports whether every blocking check passed.
func (r *ValidateResult) OK() bool { return len(r.Failures) == 0 }

// hostEntry is one domains[] list item with its source line.
type hostEntry struct {
	Raw       string
	Canonical string // "" when canonicalization failed
	Line      int
}

// changedFile is one PR-changed campaign file (git name-status).
type changedFile struct {
	name   string // basename
	status byte   // A, M, D
}

// Validate runs the §4.2 checks. base != "" is CI mode: only the files
// changed vs the merge base are evaluated and the UUID diff rule applies;
// base == "" is local mode: every root-level YAML, no git required.
// The verb never touches the DB or the network.
func Validate(ctx context.Context, repo, base string, maxDomains int) (*ValidateResult, error) {
	res := &ValidateResult{}
	var comment strings.Builder
	comment.WriteString("## Campaign validation\n\n")

	var files []changedFile
	if base != "" {
		out, err := git(ctx, repo, "diff", "--name-status", "--no-renames", base+"...HEAD")
		if err != nil {
			return nil, fmt.Errorf("git diff: %w", err)
		}
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) != 2 || strings.Contains(parts[1], "/") {
				continue // root-level files only
			}
			if ext := filepath.Ext(parts[1]); ext != ".yml" && ext != ".yaml" {
				continue
			}
			files = append(files, changedFile{name: parts[1], status: parts[0][0]})
		}
	} else {
		list, err := ListYAMLFiles(repo)
		if err != nil {
			return nil, err
		}
		for _, p := range list {
			files = append(files, changedFile{name: filepath.Base(p), status: 'M'})
		}
	}
	if len(files) == 0 {
		comment.WriteString("No campaign files changed.\n")
		res.Comment = comment.String()
		return res, nil
	}

	// Deleted files' base uuids feed the rename rule.
	deletedUUIDs := map[string][]string{} // uuid → deleted file names
	if base != "" {
		for _, f := range files {
			if f.status != 'D' {
				continue
			}
			if u := gitFileUUID(ctx, repo, base, f.name); u != "" {
				deletedUUIDs[u] = append(deletedUUIDs[u], f.name)
			}
		}
	}

	// Head-state hosts of every root-level file (cross-file informational).
	headHosts := map[string]map[string]string{} // file → host → title
	if all, err := ListYAMLFiles(repo); err == nil {
		for _, p := range all {
			if f, err := ParseFile(p, maxDomains); err == nil {
				m := map[string]string{}
				for _, h := range f.Hosts {
					m[h] = f.Title
				}
				headHosts[filepath.Base(p)] = m
			}
		}
	}

	for _, ch := range files {
		if ch.status == 'D' {
			continue // retiring a campaign is always allowed
		}
		path := filepath.Join(repo, ch.name)
		fail := func(line int, format string, args ...any) {
			res.Failures = append(res.Failures, fmt.Sprintf("%s:%d: %s", ch.name, line, fmt.Sprintf(format, args...)))
		}

		parsed, parseErr := ParseFile(path, maxDomains)
		if parseErr != nil {
			fail(1, "%v", parseErr)
		}

		entries, entriesErr := domainEntries(path)
		if entriesErr != nil && parseErr == nil {
			fail(1, "%v", entriesErr)
		}

		// Hostname validation + within-file duplicates, with line numbers.
		subdomains := 0
		firstSeen := map[string]int{}
		for i := range entries {
			e := &entries[i]
			host, err := domain.Canonicalize(e.Raw)
			if err != nil {
				fail(e.Line, "%q: %v", e.Raw, err)
				continue
			}
			registrable, _, err := PSLParse(host)
			if err != nil {
				fail(e.Line, "%q: %v", e.Raw, err)
				continue
			}
			if host != registrable {
				subdomains++
			}
			e.Canonical = host
			if prev, dup := firstSeen[host]; dup {
				fail(e.Line, "duplicate entry %q (first at line %d)", host, prev)
				continue
			}
			firstSeen[host] = e.Line
		}

		// UUID trust (CI mode only).
		if base != "" {
			// Byte-identical comparison (06 §4.2): read the raw uuid text on
			// both sides — never the parser-normalized form.
			headUUID := rawFileUUID(path)
			baseUUID := gitFileUUID(ctx, repo, base, ch.name)
			switch ch.status {
			case 'A':
				if headUUID != "" {
					if from := deletedUUIDs[headUUID]; len(from) == 1 {
						fmt.Fprintf(&comment, "**rename detected:** `%s` → `%s` (uuid preserved)\n\n", from[0], ch.name)
					} else {
						fail(1, "uuid values are assigned by the import bot; remove the uuid field")
					}
				}
			case 'M':
				if headUUID != baseUUID {
					fail(1, "uuid values are assigned by the import bot; remove the uuid field")
				}
			}
		}

		// Per-file comment summary + cross-file informational lines.
		title := ch.name
		if parsed != nil {
			title = parsed.Title
		}
		fmt.Fprintf(&comment, "### `%s` — %s\n\n", ch.name, title)
		if parsed != nil {
			fmt.Fprintf(&comment, "%d domains, %d subdomains → parents auto-linked\n\n",
				len(parsed.Hosts), subdomains)
			added := addedHosts(ctx, repo, base, ch, parsed)
			var lines []string
			for _, h := range added {
				for other, hosts := range headHosts {
					if other == ch.name {
						continue
					}
					if otherTitle, ok := hosts[h]; ok {
						lines = append(lines, fmt.Sprintf("- `%s` is also in `%s`", h, otherTitle))
					}
				}
			}
			sort.Strings(lines)
			if len(lines) > 0 {
				comment.WriteString("Cross-file memberships (informational, never blocking):\n")
				comment.WriteString(strings.Join(lines, "\n") + "\n\n")
			}
		}
	}

	if res.OK() {
		comment.WriteString("✅ all blocking checks passed\n")
	} else {
		fmt.Fprintf(&comment, "❌ %d blocking failure(s):\n\n", len(res.Failures))
		for _, f := range res.Failures {
			fmt.Fprintf(&comment, "- `%s`\n", f)
		}
	}
	res.Comment = comment.String()
	return res, nil
}

// domainEntries extracts the domains[] items with their source lines via a
// yaml.Node walk (ParseFile is line-blind).
func domainEntries(path string) ([]hostEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(raw)).Decode(&root); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("yaml: not a mapping")
	}
	m := root.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != "domains" || m.Content[i+1].Kind != yaml.SequenceNode {
			continue
		}
		var out []hostEntry
		for _, item := range m.Content[i+1].Content {
			out = append(out, hostEntry{Raw: item.Value, Line: item.Line})
		}
		return out, nil
	}
	return nil, nil
}

// uuidLineRe extracts the top-level uuid value from raw YAML text.
var uuidLineRe = regexp.MustCompile(`(?m)^uuid:\s*["']?([0-9a-fA-F-]+)["']?\s*$`)

func rawFileUUID(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := uuidLineRe.FindSubmatch(raw); m != nil {
		return string(m[1])
	}
	return ""
}

// gitFileUUID reads the file's uuid at the base ref ("" when absent or the
// file does not exist there).
func gitFileUUID(ctx context.Context, repo, base, name string) string {
	out, err := git(ctx, repo, "show", base+":"+name)
	if err != nil {
		return ""
	}
	if m := uuidLineRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// addedHosts computes the PR's newly-added hosts for one file: head minus
// base (CI mode with an existing base file); everything otherwise.
func addedHosts(ctx context.Context, repo, base string, ch changedFile, parsed *File) []string {
	if base == "" || ch.status == 'A' {
		return parsed.Hosts
	}
	out, err := git(ctx, repo, "show", base+":"+ch.name)
	if err != nil {
		return parsed.Hosts
	}
	baseSet := map[string]bool{}
	var yf yamlFile
	if yaml.Unmarshal([]byte(out), &yf) == nil {
		for _, raw := range yf.Domains {
			if h, err := domain.Canonicalize(raw); err == nil {
				baseSet[h] = true
			}
		}
	}
	var added []string
	for _, h := range parsed.Hosts {
		if !baseSet[h] {
			added = append(added, h)
		}
	}
	return added
}
