package campaign

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lasseh/whynoipv6/internal/domain"
)

// yamlFile is the normative five-key schema (06-ingest.md §3.2); unknown keys
// are a validation error (KnownFields).
type yamlFile struct {
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	UUID        string   `yaml:"uuid"`
	Domains     []string `yaml:"domains"`
	Tags        []string `yaml:"tags"`
}

// File is one parsed, validated campaign YAML.
type File struct {
	Path        string // basename, the source_file value
	Title       string
	Description string
	UUID        string   // "" when absent
	Hosts       []string // canonicalized, deduped within the file (first wins)
	Tags        []string // normalized per §3.2, nil when absent
	// RejectedHosts maps raw entry -> reason for entries that failed
	// Canonicalize or PSL evaluation.
	RejectedHosts map[string]string
}

var tagRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ParseFile parses and validates one campaign YAML (06-ingest.md §3.2, §4.2
// schema checks). maxDomains is campaign.max_domains_per_file.
//
//nolint:goconst // validation reasons are messages, not constants
func ParseFile(path string, maxDomains int) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var yf yamlFile
	if err := dec.Decode(&yf); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	if strings.TrimSpace(yf.Title) == "" {
		return nil, fmt.Errorf("title is required and non-empty")
	}
	if strings.TrimSpace(yf.Description) == "" {
		return nil, fmt.Errorf("description is required and non-empty")
	}
	if len(yf.Domains) == 0 {
		return nil, fmt.Errorf("domains is required and non-empty")
	}
	if len(yf.Domains) > maxDomains {
		return nil, fmt.Errorf("domains list has %d entries, cap is %d", len(yf.Domains), maxDomains)
	}
	if yf.UUID != "" && !uuidRe.MatchString(yf.UUID) {
		return nil, fmt.Errorf("uuid %q is not a valid UUID", yf.UUID)
	}

	f := &File{
		Path:          filepath.Base(path),
		Title:         yf.Title,
		Description:   yf.Description,
		UUID:          strings.ToLower(yf.UUID),
		RejectedHosts: map[string]string{},
	}

	// Tags: lowercase-normalized kebab-case, ≤16, deduped (§3.2 OPEN-12).
	if yf.Tags != nil {
		if len(yf.Tags) > 16 {
			return nil, fmt.Errorf("tags list has %d entries, cap is 16", len(yf.Tags))
		}
		seen := map[string]bool{}
		for _, t := range yf.Tags {
			norm := strings.ToLower(strings.TrimSpace(t))
			if norm == "" {
				return nil, fmt.Errorf("empty/whitespace tag")
			}
			if !tagRe.MatchString(norm) {
				return nil, fmt.Errorf("tag %q is not kebab-case (^[a-z0-9][a-z0-9-]{0,31}$)", t)
			}
			if !seen[norm] {
				seen[norm] = true
				f.Tags = append(f.Tags, norm)
			}
		}
	}

	// Hosts: Canonicalize, then dedupe within the file (first wins).
	seen := map[string]bool{}
	for _, raw := range yf.Domains {
		host, err := domain.Canonicalize(raw)
		if err != nil {
			f.RejectedHosts[raw] = err.Error()
			continue
		}
		if !seen[host] {
			seen[host] = true
			f.Hosts = append(f.Hosts, host)
		}
	}
	if len(f.Hosts) == 0 {
		return nil, fmt.Errorf("no valid domains after canonicalization")
	}
	return f, nil
}

// ListYAMLFiles returns the root-level *.yml/*.yaml files of the checkout,
// sorted (06-ingest.md §3.2 — root only, no subdirectories).
func ListYAMLFiles(repoPath string) ([]string, error) {
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := filepath.Ext(e.Name()); ext == ".yml" || ext == ".yaml" {
			files = append(files, filepath.Join(repoPath, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}
