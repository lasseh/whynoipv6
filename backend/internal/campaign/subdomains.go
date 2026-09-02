package campaign

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lasseh/whynoipv6/internal/domain"
)

// SubdomainsDir is the campaign-repo subdirectory holding the curated lists,
// one file per parent: subdomains/<apex>.yml.
const SubdomainsDir = "subdomains"

// subdomainYAML is the normative one-key schema; unknown keys are an error
// (KnownFields), as in the campaign files.
type subdomainYAML struct {
	Subdomains []string `yaml:"subdomains"`
}

// SubdomainFile is one parsed, validated curated list. Entries are written in
// the file as labels relative to the apex — that is what keeps a list from
// reaching outside its own parent — and Hosts holds them joined and
// canonicalized.
type SubdomainFile struct {
	Path  string   // repo-relative ("subdomains/nrk.no.yml"), the report key
	Apex  string   // registrable host, read from the filename
	Hosts []string // canonical FQDNs, deduped; nil for an empty list
}

// EntryError is a failure attributable to one subdomains[] item, carrying the
// item's index so the PR validator can name a line.
type EntryError struct {
	Index int
	Err   error
}

func (e *EntryError) Error() string { return e.Err.Error() }
func (e *EntryError) Unwrap() error { return e.Err }

// ParseSubdomainFile parses and validates one subdomains/<apex>.yml.
// maxSubdomains is campaign.max_subdomains_per_domain. One bad entry rejects
// the whole file: the lists are small, CI validates them before merge, and
// partial application would silently unlist the entries that did parse.
func ParseSubdomainFile(path string, maxSubdomains int) (*SubdomainFile, error) {
	apex, err := apexFromFilename(path)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var sy subdomainYAML
	// EOF is a comments-only or empty file: an empty list, equivalent to
	// deleting the file (all membership drops, lifecycle handles the rest).
	if err := dec.Decode(&sy); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	if len(sy.Subdomains) > maxSubdomains {
		return nil, fmt.Errorf("subdomains list has %d entries, cap is %d", len(sy.Subdomains), maxSubdomains)
	}

	f := &SubdomainFile{
		Path: filepath.Join(SubdomainsDir, filepath.Base(path)),
		Apex: apex,
	}
	seen := map[string]bool{}
	for i, entry := range sy.Subdomains {
		host, err := f.hostFor(entry)
		if err != nil {
			return nil, &EntryError{Index: i, Err: err}
		}
		if seen[host] {
			return nil, &EntryError{Index: i, Err: fmt.Errorf("duplicate entry %q", entry)}
		}
		seen[host] = true
		f.Hosts = append(f.Hosts, host)
	}
	return f, nil
}

// hostFor joins one relative label sequence to the apex and canonicalizes the
// result, which is where label syntax, IDN mapping and the 253-octet limit are
// enforced (domain.Canonicalize is the single sanctioned site for all three).
func (f *SubdomainFile) hostFor(entry string) (string, error) {
	label := strings.TrimSpace(entry)
	if label == "" {
		return "", errors.New("empty entry")
	}
	host, err := domain.Canonicalize(label + "." + f.Apex)
	if err != nil {
		return "", fmt.Errorf("%q: %w", entry, err)
	}
	// Everything below reads the canonical form, which is why the entry's own
	// case never needs handling here: Canonicalize owns hostname lowercasing
	// (06 §9.1 grep gate).
	rel := strings.TrimSuffix(host, "."+f.Apex)
	if rel == f.Apex || strings.HasSuffix(rel, "."+f.Apex) {
		return "", fmt.Errorf("%q is a full host: write the label relative to %s", entry, f.Apex)
	}
	if rel == "www" {
		return "", fmt.Errorf("%q is redundant: the apex's own www dimension already covers it", entry)
	}
	return host, nil
}

// apexFromFilename derives the parent from the filename and requires it to be
// canonical: two spellings that normalize to the same apex would otherwise be
// two files for one parent, with the last sync silently winning.
func apexFromFilename(path string) (string, error) {
	base := filepath.Base(path)
	name := strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
	apex, err := domain.Canonicalize(name)
	if err != nil {
		return "", fmt.Errorf("filename %q: %w", base, err)
	}
	if apex != name {
		return "", fmt.Errorf("filename %q must be lowercase punycode: rename it to %s%s", base, apex, filepath.Ext(base))
	}
	registrable, _, err := domain.PSLParse(apex)
	if err != nil {
		return "", fmt.Errorf("filename %q: %w", base, err)
	}
	if apex != registrable {
		return "", fmt.Errorf("filename %q must name the registrable apex (%s), not a subdomain", base, registrable)
	}
	return apex, nil
}

// ListSubdomainFiles returns the sorted subdomains/*.yml|*.yaml files of the
// checkout. A repo without the directory is not an error: the lists are
// optional, and campaign repos predate them.
func ListSubdomainFiles(repoPath string) ([]string, error) {
	files, err := listYAMLIn(filepath.Join(repoPath, SubdomainsDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return files, err
}

// subdomainReportKey is a list's repo-relative path, the key it is reported
// under. Parsing yields the same value as SubdomainFile.Path; this derives it
// from the path alone, for files that failed to parse.
func subdomainReportKey(path string) string {
	return filepath.Join(SubdomainsDir, filepath.Base(path))
}

// validateSubdomainFiles runs the subdomain half of the PR validation, sharing
// the campaign checks' file:line failure format and bot-comment section style.
func validateSubdomainFiles(repo string, files []changedFile, maxSubdomains int, res *ValidateResult, comment *strings.Builder) {
	// Every list in the repo head, so a PR adding nrk.no.yaml next to an
	// existing nrk.no.yml is caught even though only one of them is in the
	// diff. One parent, one file (06 §3.7).
	claimants := map[string][]string{}
	if all, err := ListSubdomainFiles(repo); err == nil {
		for _, p := range all {
			if f, err := ParseSubdomainFile(p, maxSubdomains); err == nil {
				claimants[f.Apex] = append(claimants[f.Apex], f.Path)
			}
		}
	}

	for _, ch := range files {
		if ch.status == 'D' {
			continue // dropping a list is always allowed
		}
		path := filepath.Join(repo, ch.name) // ch.name is repo-relative here

		parsed, err := ParseSubdomainFile(path, maxSubdomains)
		if err != nil {
			line := 1
			if ee, ok := errors.AsType[*EntryError](err); ok {
				if entries, eErr := sequenceEntries(path, "subdomains"); eErr == nil && ee.Index < len(entries) {
					line = entries[ee.Index].Line
				}
			}
			res.Failures = append(res.Failures,
				fmt.Sprintf("%s:%d: %s", ch.name, line, err))
			continue
		}

		// Any other claimant fails the changed file, whichever spelling sorts
		// first — the sync rejects both claimants and freezes curated removals.
		var others []string
		for _, p := range claimants[parsed.Apex] {
			if p != parsed.Path {
				others = append(others, p)
			}
		}
		if len(others) > 0 {
			res.Failures = append(res.Failures, fmt.Sprintf(
				"%s:1: `%s` is already listed by `%s` — one file per domain, merge them",
				ch.name, parsed.Apex, strings.Join(others, "`, `")))
			continue
		}

		fmt.Fprintf(comment, "### `%s` — %d subdomain(s) of `%s`\n\n", ch.name, len(parsed.Hosts), parsed.Apex)
		if len(parsed.Hosts) > 0 {
			fmt.Fprintf(comment, "%s\n\n", strings.Join(parsed.Hosts, ", "))
		}
		fmt.Fprintf(comment,
			"`%s` must already be tracked on the site for these to be picked up — the sync skips lists whose apex is unknown (informational, never blocking).\n\n",
			parsed.Apex)
	}
}
