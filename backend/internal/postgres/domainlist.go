// Package postgres holds the hand-written adapters over the sqlc-generated
// db/ subpackage. domainlist.go is the ONE builder-built slice (05-schema.md
// §10.2 carve-out): the /domains list family, whose filter grammar would
// need 150–300 sqlc texts and whose partial indexes demand literal
// predicates. Every other query stays sqlc.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DomainListFilter is the validated §3.3 filter set. Closed-set values
// (class, status dims, flag, gold) are emitted as SQL literals — required
// for the planner's partial-index predicate-implication check; free-text
// values (tld, hosting, q) ride bind parameters.
type DomainListFilter struct {
	Class     string // validated classification literal; "" = none
	Gold      bool
	CountryID *int32 // resolved from the ISO code by the caller
	ASNID     *int32 // resolved from the AS number
	Provider  *int64 // dns_provider.id
	TLD       string // free text (bound)
	Hosting   string // free text (bound)
	Flag      string // validated class_flags literal
	StatusDim string // validated dimension name (base/www/ns/mx/conn/resources)
	StatusVal string // validated ipv6_status literal
	RankMin   *int32
	RankMax   *int32
	Query     string // ?q= substring (bound; forces the host ordering)

	// Scope presets for sub-collections (campaign members etc.) are built
	// by their own methods, not through this filter.
	IncludeRankNULL bool // dependents ordering only
}

// DomainSeek is the decoded cursor seek (mirrors api.Seek without the
// import cycle).
type DomainSeek struct {
	Rank     *int32
	ID       int64
	Host     string
	RankNull bool
}

// DomainRow is the §4.2 summary row scanned via RowToStructByName.
type DomainRow struct {
	ID             int64      `db:"id"`
	Host           string     `db:"host"`
	Rank           *int32     `db:"rank"`
	Kind           string     `db:"kind"`
	Parent         *string    `db:"parent"`
	Classification string     `db:"classification"`
	ClassFlags     []string   `db:"class_flags"`
	Gold           bool       `db:"gold"`
	BaseStatus     *string    `db:"base_status"`
	BaseSince      *time.Time `db:"base_since"`
	WWWStatus      *string    `db:"www_status"`
	WWWSince       *time.Time `db:"www_since"`
	NSStatus       *string    `db:"ns_status"`
	NSSince        *time.Time `db:"ns_since"`
	MXStatus       *string    `db:"mx_status"`
	MXSince        *time.Time `db:"mx_since"`
	ConnStatus     *string    `db:"conn_status"`
	ConnSince      *time.Time `db:"conn_since"`
	ResStatus      *string    `db:"resources_status"`
	ResSince       *time.Time `db:"resources_since"`
	TLD            *string    `db:"tld"`
	CountryCode    string     `db:"country_code"`
	CountryName    string     `db:"country_name"`
	ASNNumber      int64      `db:"asn_number"`
	ASNName        string     `db:"asn_name"`
	ProviderID     *int64     `db:"provider_id"`
	ProviderName   *string    `db:"provider_name"`
	Hosting        *string    `db:"hosting_provider"`
	LastCheckedAt  *time.Time `db:"last_checked_at"`
}

const domainRowColumns = `d.id, d.host, d.rank, d.kind::text AS kind, p.host AS parent,
d.classification::text AS classification, d.class_flags, d.gold,
d.base_status::text AS base_status, d.base_since,
d.www_status::text AS www_status, d.www_since,
d.ns_status::text AS ns_status, d.ns_since,
d.mx_status::text AS mx_status, d.mx_since,
d.conn_status::text AS conn_status, d.conn_since,
d.resources_status::text AS resources_status, d.resources_since,
d.tld, c.code::text AS country_code, c.name AS country_name,
a.number AS asn_number, a.name AS asn_name,
dp.id AS provider_id, dp.name AS provider_name,
d.hosting_provider, d.last_checked_at`

// ListSort names the three orderings (07 §3.2).
type ListSort string

const (
	ListSortRank     ListSort = "rank"
	ListSortRankDesc ListSort = "-rank"
	ListSortHost     ListSort = "host"
)

// buildDomainList assembles the leaderboard query. The publicly-ranked
// predicate is spelled verbatim as a literal (05-schema §1.7).
func buildDomainList(f *DomainListFilter, sortKey ListSort, seek *DomainSeek, afterRank *int32, limit int) (sqlText string, args []any, err error) {
	q := sq.Select(strings.Split(domainRowColumns, ",\n")...).
		From("domain d").
		Join("country c ON c.id = d.country_id").
		Join("asn a ON a.id = d.asn_id").
		LeftJoin("dns_provider dp ON dp.id = d.dns_provider_id").
		LeftJoin("domain p ON p.id = d.parent_id").
		PlaceholderFormat(sq.Dollar)

	// The literal public scope — verbatim for partial-index implication.
	q = q.Where(sq.Expr("d.rank IS NOT NULL AND NOT d.disabled"))

	if f.Class != "" {
		q = q.Where(sq.Expr(fmt.Sprintf("d.classification = '%s'", f.Class))) // validated closed set
	}
	if f.Gold {
		q = q.Where(sq.Expr("d.gold"))
	}
	if f.CountryID != nil {
		q = q.Where(sq.Expr(fmt.Sprintf("d.country_id = %d", *f.CountryID)))
	}
	if f.ASNID != nil {
		q = q.Where(sq.Expr(fmt.Sprintf("d.asn_id = %d", *f.ASNID)))
	}
	if f.Provider != nil {
		q = q.Where(sq.Expr(fmt.Sprintf("d.dns_provider_id = %d", *f.Provider)))
	}
	if f.TLD != "" {
		q = q.Where(sq.Expr("d.tld = ?", f.TLD))
	}
	if f.Hosting != "" {
		q = q.Where(sq.Expr("d.hosting_provider = ?", f.Hosting))
	}
	if f.Flag != "" {
		q = q.Where(sq.Expr(fmt.Sprintf("'%s' = ANY(d.class_flags)", f.Flag))) // validated closed set
	}
	if f.StatusDim != "" && f.StatusVal != "" {
		q = q.Where(sq.Expr(fmt.Sprintf("d.%s_status = '%s'", f.StatusDim, f.StatusVal))) // both validated
	}
	if f.RankMin != nil {
		q = q.Where(sq.Expr(fmt.Sprintf("d.rank >= %d", *f.RankMin)))
	}
	if f.RankMax != nil {
		q = q.Where(sq.Expr(fmt.Sprintf("d.rank <= %d", *f.RankMax)))
	}
	if f.Query != "" {
		q = q.Where(sq.Expr("d.host LIKE '%' || ? || '%'", f.Query)) // trigram-backed
	}

	switch sortKey {
	case ListSortHost:
		if seek != nil && seek.Host != "" {
			q = q.Where(sq.Expr("d.host > ?", seek.Host))
		}
		q = q.OrderBy("d.host ASC")
	case ListSortRankDesc:
		if seek != nil && seek.Rank != nil {
			q = q.Where(sq.Expr(fmt.Sprintf("(d.rank, d.id) < (%d, %d)", *seek.Rank, seek.ID)))
		}
		q = q.OrderBy("d.rank DESC", "d.id DESC")
	default: // rank
		if afterRank != nil {
			q = q.Where(sq.Expr(fmt.Sprintf("d.rank > %d", *afterRank)))
		} else if seek != nil && seek.Rank != nil {
			q = q.Where(sq.Expr(fmt.Sprintf("(d.rank, d.id) > (%d, %d)", *seek.Rank, seek.ID)))
		}
		q = q.OrderBy("d.rank ASC", "d.id ASC")
	}

	return q.Limit(uint64(limit + 1)).ToSql() // N+1 fetch
}

// ListDomains runs the built query and returns limit+1 rows at most.
func ListDomains(ctx context.Context, pool *pgxpool.Pool, f *DomainListFilter,
	sortKey ListSort, seek *DomainSeek, afterRank *int32, limit int,
) ([]DomainRow, error) {
	sqlText, args, err := buildDomainList(f, sortKey, seek, afterRank, limit)
	if err != nil {
		return nil, fmt.Errorf("domain list build: %w", err)
	}
	rows, err := pool.Query(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("domain list: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[DomainRow])
	if err != nil {
		return nil, fmt.Errorf("domain list scan: %w", err)
	}
	return out, nil
}

// EstimateDomainListCount returns the plan-row estimate for the filtered
// list (07 §3.4 — never an exact count on the hot path).
func EstimateDomainListCount(ctx context.Context, pool *pgxpool.Pool, f *DomainListFilter) (int64, error) {
	sqlText, args, err := buildDomainList(f, ListSortRank, nil, nil, 0)
	if err != nil {
		return 0, err
	}
	// Strip the LIMIT so the estimate covers the whole scope.
	sqlText = sqlText[:strings.LastIndex(sqlText, " LIMIT ")]
	var raw []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+sqlText, args...).Scan(&raw); err != nil {
		return 0, fmt.Errorf("count estimate: %w", err)
	}
	var plans []struct {
		Plan struct {
			PlanRows float64 `json:"Plan Rows"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) == 0 {
		return 0, fmt.Errorf("count estimate parse: %w", err)
	}
	return int64(plans[0].Plan.PlanRows), nil
}

// MaxRank is the O(1) global-list count estimate (07 §3.4).
func MaxRank(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var n *int64
	if err := pool.QueryRow(ctx, "SELECT max(rank)::bigint FROM domain").Scan(&n); err != nil {
		return 0, err
	}
	if n == nil {
		return 0, nil
	}
	return *n, nil
}
