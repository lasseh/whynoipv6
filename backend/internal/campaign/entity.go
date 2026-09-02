package campaign

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/lasseh/whynoipv6/internal/domain"
	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// entityEnsurer implements the §3.4 ensure-entity rule against one tx.
type entityEnsurer struct {
	q               *db.Queries
	sentinelASN     int32
	sentinelCountry int32
}

func newEntityEnsurer(ctx context.Context, q *db.Queries) (*entityEnsurer, error) {
	asn, err := q.ASNSentinelID(ctx)
	if err != nil {
		return nil, fmt.Errorf("sentinel asn: %w", err)
	}
	country, err := q.CountrySentinelID(ctx)
	if err != nil {
		return nil, fmt.Errorf("sentinel country: %w", err)
	}
	return &entityEnsurer{q: q, sentinelASN: asn, sentinelCountry: country}, nil
}

// countryID applies the §6.5 insert-time country rule: final DNS label,
// upper-cased and dot-prefixed, probed against country.tld; miss → sentinel.
func (e *entityEnsurer) countryID(ctx context.Context, host string) int32 {
	label := host[strings.LastIndexByte(host, '.')+1:]
	probe := "." + strings.ToUpper(label)
	id, err := e.q.CountryIDByTLD(ctx, &probe)
	if err != nil {
		return e.sentinelCountry
	}
	return id
}

// ensure returns the domain id for a canonicalized host, creating the entity
// (and, for subdomains, the auto-created parent apex) when absent.
// existed reports whether the host row pre-existed.
func (e *entityEnsurer) ensure(ctx context.Context, host string) (id int64, existed bool, err error) {
	registrable, tld, err := domain.PSLParse(host)
	if err != nil {
		return 0, false, err
	}

	if host == registrable {
		return e.ensureRow(ctx, host, "apex", nil, "campaign", tld)
	}

	// Subdomain: parent apex first (created_by='parent_link' when absent).
	parentID, _, err := e.ensureRow(ctx, registrable, "apex", nil, "parent_link", tld)
	if err != nil {
		return 0, false, fmt.Errorf("ensure parent %s: %w", registrable, err)
	}
	return e.ensureRow(ctx, host, "subdomain", &parentID, "campaign", tld)
}

func (e *entityEnsurer) ensureRow(ctx context.Context, host, kind string, parentID *int64, createdBy, tld string) (id int64, existed bool, err error) {
	existing, err := e.q.DomainByHost(ctx, host)
	switch {
	case err == nil:
		if kind == "subdomain" && existing.Kind == db.DomainKindApex {
			// Defensive: impossible via these ingresses (06 §3.4 step 3b).
			slog.Warn("subdomain entry already exists as apex; leaving kind/parent unchanged", "domain", host)
		}
		return existing.ID, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return 0, false, fmt.Errorf("lookup %s: %w", host, err)
	}

	id, err = e.q.DomainInsertEntity(ctx, db.DomainInsertEntityParams{
		Host:      host,
		Kind:      db.DomainKind(kind),
		ParentID:  parentID,
		CreatedBy: db.CreatedBy(createdBy),
		AsnID:     e.sentinelASN,
		CountryID: e.countryID(ctx, host),
		Tld:       &tld,
	})
	if err == nil {
		return id, false, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// ON CONFLICT DO NOTHING raced another ingress: re-read.
		existing, err := e.q.DomainByHost(ctx, host)
		if err != nil {
			return 0, false, fmt.Errorf("re-read %s: %w", host, err)
		}
		return existing.ID, true, nil
	}
	return 0, false, fmt.Errorf("insert %s: %w", host, err)
}
