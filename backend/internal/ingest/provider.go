package ingest

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// ProviderMapping is the in-memory ns_host → provider snapshot, loaded once
// per run and refreshed on dns_provider.refresh_interval (06-ingest.md §6.10).
type ProviderMapping struct {
	mu       sync.RWMutex
	suffixes map[string]int64 // suffix (lowercase, no leading dot) -> provider id
}

// LoadProviderMapping builds the snapshot from the dns_provider table.
func LoadProviderMapping(ctx context.Context, q *db.Queries) (*ProviderMapping, error) {
	m := &ProviderMapping{}
	if err := m.Refresh(ctx, q); err != nil {
		return nil, err
	}
	return m, nil
}

// Refresh rebuilds the snapshot in place from the dns_provider table
// (dns_provider.refresh_interval — provider curation lands without a
// crawler restart).
func (m *ProviderMapping) Refresh(ctx context.Context, q *db.Queries) error {
	rows, err := q.ProviderList(ctx)
	if err != nil {
		return fmt.Errorf("provider mapping: %w", err)
	}
	next := map[string]int64{}
	for _, r := range rows {
		for _, s := range r.NsSuffixes {
			next[s] = r.ID
		}
	}
	m.mu.Lock()
	m.suffixes = next
	m.mu.Unlock()
	return nil
}

// ProviderForNSHost resolves one nameserver host by the longest matching
// suffix on a label boundary (ns == suffix or ns ends with "."+suffix).
// NS hosts arrive as wire-form FQDNs (trailing root dot, case as served), so
// they are folded to the stored suffix form first — same step as
// NormalizeHosting's CNAME chain.
func (m *ProviderMapping) ProviderForNSHost(ns string) (id int64, matchLen int, ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ns = strings.ToLower(strings.TrimSuffix(ns, "."))
	for suffix, pid := range m.suffixes {
		if ns == suffix || strings.HasSuffix(ns, "."+suffix) {
			if len(suffix) > matchLen {
				id, matchLen, ok = pid, len(suffix), true
			}
		}
	}
	return id, matchLen, ok
}

// ProviderForNSSet resolves a domain's NS host set: the longest matching
// suffix across all providers and all NS hosts wins; empty set or no match
// → nil (06-ingest.md §6.10).
func (m *ProviderMapping) ProviderForNSSet(nsHosts []string) *int64 {
	var best *int64
	bestLen := 0
	for _, ns := range nsHosts {
		if id, l, ok := m.ProviderForNSHost(ns); ok && l > bestLen {
			v := id
			best, bestLen = &v, l
		}
	}
	return best
}

// StampDNSProvider is the read-only attribution writer: it derives the
// provider from the scan's observed NS hosts and stamps ONLY
// domain.dns_provider_id — never scan/changelog/confirmed-status columns.
func StampDNSProvider(ctx context.Context, q *db.Queries, m *ProviderMapping, domainID int64, nsHosts []string) error {
	return q.DomainStampDNSProvider(ctx, db.DomainStampDNSProviderParams{
		ID: domainID, DnsProviderID: m.ProviderForNSSet(nsHosts),
	})
}

// providerSeedEntry mirrors the `v6ctl provider add` arguments
// (dns_provider.seed_path YAML — 06-ingest.md §6.11).
type providerSeedEntry struct {
	Name     string   `yaml:"name"`
	Suffixes []string `yaml:"suffixes"`
}

// SeedProviders upserts every provider in a curated seed document — the same
// operation as `v6ctl provider add`, once per entry, so it is idempotent.
func SeedProviders(ctx context.Context, pool *pgxpool.Pool, raw []byte) (int, error) {
	var entries []providerSeedEntry
	if err := yaml.Unmarshal(raw, &entries); err != nil {
		return 0, fmt.Errorf("provider seed: %w", err)
	}
	q := db.New(pool)
	for _, e := range entries {
		if err := ProviderAdd(ctx, q, e.Name, e.Suffixes); err != nil {
			return 0, err
		}
	}
	return len(entries), nil
}

// ProviderAdd upserts a provider by name and appends the suffixes (deduped;
// stored lowercase, no leading dot) — the table's single write path.
func ProviderAdd(ctx context.Context, q *db.Queries, name string, suffixes []string) error {
	norm := make([]string, 0, len(suffixes))
	for _, s := range suffixes {
		s = strings.TrimPrefix(strings.TrimSpace(s), ".")
		if s == "" {
			continue
		}
		norm = append(norm, strings.ToLower(s))
	}
	if name == "" || len(norm) == 0 {
		return fmt.Errorf("provider add: name and at least one suffix required")
	}
	row, err := q.ProviderByName(ctx, name)
	if err != nil {
		if _, err := q.ProviderInsert(ctx, db.ProviderInsertParams{Name: name, NsSuffixes: norm}); err != nil {
			return fmt.Errorf("provider add: %w", err)
		}
		return nil
	}
	return q.ProviderAppendSuffixes(ctx, db.ProviderAppendSuffixesParams{ID: row.ID, Suffixes: norm})
}
