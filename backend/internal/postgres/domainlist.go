package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// DomainListFilter is the validated §3.3 filter set. Closed-set values
// (class, status dims, flag, saint) are emitted as SQL literals — required
// for the planner's partial-index predicate-implication check; free-text
// values (tld, hosting, q) ride bind parameters.
type DomainListFilter struct {
	Class      string // validated classification literal; "" = none
	Saint      bool
	AlmostHero bool   // hero in every dimension except the apex AAAA (07 §4.4)
	CountryID  *int32 // resolved from the ISO code by the caller
	ASNID      *int32 // resolved from the AS number
	Provider   *int64 // dns_provider.id
	TLD        string // free text (bound)
	Hosting    string // free text (bound)
	Flag       string // validated class_flags literal
	StatusDim  string // validated dimension name (base/www/ns/mx/conn/resources)
	StatusVal  string // validated ipv6_status literal
	RankMin    *int32
	RankMax    *int32
	Query      string // ?q= substring (bound; forces the host ordering)

	// Sub-collection scopes. Either drops the public rank-IS-NOT-NULL half
	// of the visibility predicate: campaign members and subdomains are
	// typically rank-NULL and must resolve on their sub-collections
	// (07 §2.2/§4.7); NOT disabled always applies.
	CampaignID *int32 // campaign_domain membership (internal campaign.id)
	ParentID   *int64 // subdomains of one apex
}

// The closed sets behind the literal interpolations in buildDomainList.
// api.parseDomainFilter validates the same sets at the boundary, but the
// builder re-checks them itself (validateLiterals) so no future caller can
// reach the fmt.Sprintf literals with unvalidated input.
var (
	literalClasses = map[string]bool{
		string(db.ClassificationUnknown): true, string(db.ClassificationInactive): true,
		string(db.ClassificationSinner): true, string(db.ClassificationPartial): true,
		string(db.ClassificationHero): true,
	}
	literalDims   = map[string]bool{"base": true, "www": true, "ns": true, "mx": true, "conn": true, "resources": true}
	literalStatus = map[string]bool{
		string(db.Ipv6StatusSupported): true, string(db.Ipv6StatusUnsupported): true,
		string(db.Ipv6StatusNoRecord): true, string(db.Ipv6StatusNotApplicable): true,
	}
	literalFlags = map[string]bool{"broken_v6": true, "www_missing": true, "ns_missing": true, "mail_missing": true, "resources_v4only": true}
)

// validateLiterals rejects any literal-emitted field outside its closed set —
// the in-package guard behind the cross-package validation convention.
func (f *DomainListFilter) validateLiterals() error {
	if f.Class != "" && !literalClasses[f.Class] {
		return fmt.Errorf("domain list filter: class %q outside the closed set", f.Class)
	}
	if f.Flag != "" && !literalFlags[f.Flag] {
		return fmt.Errorf("domain list filter: flag %q outside the closed set", f.Flag)
	}
	if (f.StatusDim != "" || f.StatusVal != "") &&
		(!literalDims[f.StatusDim] || !literalStatus[f.StatusVal]) {
		return fmt.Errorf("domain list filter: status %q=%q outside the closed set", f.StatusDim, f.StatusVal)
	}
	return nil
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
	Saint          bool       `db:"saint"`
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

// Confirmed returns the six confirmed (status, since) pairs in canonical
// dimension order: base, www, ns, mx, conn, resources.
func (r *DomainRow) Confirmed() ([6]*string, [6]*time.Time) {
	return [6]*string{r.BaseStatus, r.WWWStatus, r.NSStatus, r.MXStatus, r.ConnStatus, r.ResStatus},
		[6]*time.Time{r.BaseSince, r.WWWSince, r.NSSince, r.MXSince, r.ConnSince, r.ResSince}
}

const domainRowColumns = `d.id, d.host, d.rank, d.kind::text AS kind, p.host AS parent,
d.classification::text AS classification, d.class_flags, d.saint,
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

// ListSort names the four orderings (07 §3.2).
type ListSort string

const (
	ListSortRank     ListSort = "rank"
	ListSortRankDesc ListSort = "-rank"
	ListSortHost     ListSort = "host"
	ListSortSearch   ListSort = "search"
)

// ORDER BY fragments shared by the forward/backward walks.
const (
	orderIDDesc = "d.id DESC"
	orderIDAsc  = "d.id ASC"
)

// baseDomainSelect is the shared §4.2-row select over the five-way join —
// the one base both the leaderboard and the dependents list build on.
func baseDomainSelect(extraColumns ...string) sq.SelectBuilder {
	return sq.Select(append(strings.Split(domainRowColumns, ",\n"), extraColumns...)...).
		From("domain d").
		Join("country c ON c.id = d.country_id").
		Join("asn a ON a.id = d.asn_id").
		LeftJoin("dns_provider dp ON dp.id = d.dns_provider_id").
		LeftJoin("domain p ON p.id = d.parent_id").
		PlaceholderFormat(sq.Dollar)
}

// buildDomainList assembles the leaderboard query. The /domains list family
// is the ONE builder-built slice (05-schema.md §10.2 carve-out): its filter
// grammar would need 150–300 sqlc texts and its partial indexes demand
// literal predicates. The publicly-ranked predicate is spelled verbatim as a
// literal (05-schema §1.7).
func buildDomainList(f *DomainListFilter, sortKey ListSort, seek *DomainSeek, afterRank *int32, backward bool) sq.SelectBuilder {
	q := baseDomainSelect()

	// The literal public scope — verbatim for partial-index implication.
	// Sub-collection scopes keep rank-NULL members visible (07 §2.2).
	switch {
	case f.CampaignID != nil:
		q = q.Join(fmt.Sprintf("campaign_domain cd ON cd.domain_id = d.id AND cd.campaign_id = %d", *f.CampaignID)).
			Where(sq.Expr("NOT d.disabled"))
	case f.ParentID != nil:
		q = q.Where(sq.Expr(fmt.Sprintf("d.parent_id = %d AND NOT d.disabled", *f.ParentID)))
	case f.Query != "":
		// ?q= search spans all non-disabled rows including rank-NULL
		// (campaign-only hosts, subdomains, live-check origins) — the one
		// read that surfaces rank-NULL rows outside their sub-collections
		// (07 §3.1/§3.3, Decision 2026-07-11). Only rank IS NOT NULL is
		// dropped; NOT disabled stays. Those rows sort last under the
		// ListSortSearch null-flag-first ordering below.
		q = q.Where(sq.Expr("NOT d.disabled"))
	default:
		q = q.Where(sq.Expr("d.rank IS NOT NULL AND NOT d.disabled"))
	}

	if f.Class != "" {
		q = q.Where(sq.Expr(fmt.Sprintf("d.classification = '%s'", f.Class))) // validated closed set
	}
	if f.Saint {
		q = q.Where(sq.Expr("d.saint"))
	}
	if f.AlmostHero {
		// One DNS record from hero: everything passes except the apex AAAA.
		// conn is excluded — it cannot pass without the apex record.
		q = q.Where(sq.Expr("d.www_status = 'supported' AND d.ns_status = 'supported'" +
			" AND d.mx_status IN ('supported','not_applicable')" +
			" AND d.base_status IN ('unsupported','no_record')"))
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
		// Bound, not literal: no partial index predicates class_flags, so
		// the planner gains nothing from a literal here.
		q = q.Where(sq.Expr("? = ANY(d.class_flags)", f.Flag))
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

	// backward flips the seek comparison and the ORDER BY (the §3.2
	// prev_cursor walk); the caller re-reverses the rows for display.
	switch sortKey {
	case ListSortSearch:
		// ?q= orders by rank NULLS LAST on the null-flag-first key, the
		// same shape ListDependents walks: the rank-NULL rows the search
		// scope pulls in cannot anchor a plain (rank, id) seek, and a
		// literal (rank, ...) row comparison would drop the whole NULL
		// tail (NULL inside a row comparison → UNKNOWN). The rank
		// component therefore rides COALESCE(rank, 0) on both sides —
		// equal within the null partition, where d.id takes over.
		cmp, order := ">", []string{"(d.rank IS NULL)", "COALESCE(d.rank, 0)", orderIDAsc}
		if backward {
			cmp, order = "<", []string{"(d.rank IS NULL) DESC", "COALESCE(d.rank, 0) DESC", orderIDDesc}
		}
		if seek != nil {
			rank := int32(0)
			if seek.Rank != nil {
				rank = *seek.Rank
			}
			q = q.Where(sq.Expr(fmt.Sprintf(
				"((d.rank IS NULL), COALESCE(d.rank, 0), d.id) %s (%t, %d, %d)",
				cmp, seek.RankNull, rank, seek.ID)))
		}
		q = q.OrderBy(order...)
	case ListSortHost:
		if seek != nil && seek.Host != "" {
			if backward {
				q = q.Where(sq.Expr("d.host < ?", seek.Host))
			} else {
				q = q.Where(sq.Expr("d.host > ?", seek.Host))
			}
		}
		if backward {
			q = q.OrderBy("d.host DESC")
		} else {
			q = q.OrderBy("d.host ASC")
		}
	case ListSortRankDesc:
		cmp, order := "<", []string{"d.rank DESC", orderIDDesc}
		if backward {
			cmp, order = ">", []string{"d.rank ASC", orderIDAsc}
		}
		if afterRank != nil {
			q = q.Where(sq.Expr(fmt.Sprintf("d.rank < %d", *afterRank)))
		} else if seek != nil && seek.Rank != nil {
			q = q.Where(sq.Expr(fmt.Sprintf("(d.rank, d.id) %s (%d, %d)", cmp, *seek.Rank, seek.ID)))
		}
		q = q.OrderBy(order...)
	default: // rank
		cmp, order := ">", []string{"d.rank ASC", orderIDAsc}
		if backward {
			cmp, order = "<", []string{"d.rank DESC", orderIDDesc}
		}
		if afterRank != nil {
			q = q.Where(sq.Expr(fmt.Sprintf("d.rank > %d", *afterRank)))
		} else if seek != nil && seek.Rank != nil {
			q = q.Where(sq.Expr(fmt.Sprintf("(d.rank, d.id) %s (%d, %d)", cmp, *seek.Rank, seek.ID)))
		}
		q = q.OrderBy(order...)
	}

	return q
}

// ListDomains runs the built query and returns limit+1 rows at most.
func ListDomains(ctx context.Context, pool *pgxpool.Pool, f *DomainListFilter,
	sortKey ListSort, seek *DomainSeek, afterRank *int32, limit int, backward bool,
) ([]DomainRow, error) {
	if err := f.validateLiterals(); err != nil {
		return nil, err
	}
	q := buildDomainList(f, sortKey, seek, afterRank, backward)
	return collectKeysetRows[DomainRow](ctx, pool,
		q.Limit(uint64(limit+1)), backward, "domain list") // N+1 fetch
}

// ListDomainsAround is the §3.2 centered-window deep link: the ⌈limit/2⌉
// rows ranked ≤ N plus the ⌊limit/2⌋ rows ranked > N, both under the same
// filters. moreAbove/moreBelow report window truncation for cursor minting.
func ListDomainsAround(ctx context.Context, pool *pgxpool.Pool, f *DomainListFilter,
	around int32, limit int,
) (rows []DomainRow, moreAbove, moreBelow bool, err error) {
	ceilHalf := (limit + 1) / 2
	floorHalf := limit / 2

	// Upper half: rank ≤ N, fetched descending (backward) so the window
	// hugs N, then re-reversed by ListDomains.
	upper := *f
	if upper.RankMax == nil || *upper.RankMax > around {
		upper.RankMax = &around
	}
	top, err := ListDomains(ctx, pool, &upper, ListSortRank, nil, nil, ceilHalf, true)
	if err != nil {
		return nil, false, false, err
	}
	if len(top) > ceilHalf {
		moreAbove = true
		top = top[1:] // backward N+1 overflow sits at the front
	}

	// Lower half: rank > N ascending.
	lower := *f
	next := around + 1
	if lower.RankMin == nil || *lower.RankMin < next {
		lower.RankMin = &next
	}
	bottom, err := ListDomains(ctx, pool, &lower, ListSortRank, nil, nil, floorHalf, false)
	if err != nil {
		return nil, false, false, err
	}
	if len(bottom) > floorHalf {
		moreBelow = true
		bottom = bottom[:floorHalf]
	}
	return append(top, bottom...), moreAbove, moreBelow, nil
}

// EstimateDomainListCount returns the plan-row estimate for the filtered
// list (07 §3.4 — never an exact count on the hot path).
func EstimateDomainListCount(ctx context.Context, pool *pgxpool.Pool, f *DomainListFilter) (int64, error) {
	if err := f.validateLiterals(); err != nil {
		return 0, err
	}
	// No LIMIT: the estimate covers the whole scope.
	sqlText, args, err := buildDomainList(f, ListSortRank, nil, nil, false).ToSql()
	if err != nil {
		return 0, err
	}
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

// DependentRow is a §4.2 summary row plus the dependency-link attributes
// carried on the reverse dependents list (07 §4.11).
type DependentRow struct {
	DomainRow
	Source   string `db:"source"`
	Required bool   `db:"required"`
}

// DependentSeek is the decoded null-flag-first seek tuple (07 §3.2).
type DependentSeek struct {
	RankNull bool
	Rank     *int32
	ID       int64
}

// ListDependents serves GET /resources/{host}/dependents: domains linked to
// one resource host, ordered rank NULLS LAST via the null-flag-first key.
// The spec's literal `(rank IS NULL, rank, id) > ($1,$2,$3)` drops the whole
// rank-NULL tail (NULL inside a row comparison → UNKNOWN), so the rank
// component rides COALESCE(rank, 0) on both sides — equal within the null
// partition, where the id tiebreaker takes over.
func ListDependents(ctx context.Context, pool *pgxpool.Pool, resourceHostID int64,
	seek *DependentSeek, limit int, backward bool,
) ([]DependentRow, error) {
	q := baseDomainSelect("dr.source::text AS source", "dr.required").
		Join(fmt.Sprintf("domain_resource dr ON dr.domain_id = d.id AND dr.resource_host_id = %d", resourceHostID)).
		Where(sq.Expr("NOT d.disabled"))
	cmp := ">"
	order := []string{"(d.rank IS NULL)", "COALESCE(d.rank, 0)", "d.id"}
	if backward {
		cmp = "<"
		order = []string{"(d.rank IS NULL) DESC", "COALESCE(d.rank, 0) DESC", orderIDDesc}
	}
	if seek != nil {
		rank := int32(0)
		if seek.Rank != nil {
			rank = *seek.Rank
		}
		q = q.Where(sq.Expr(fmt.Sprintf(
			"((d.rank IS NULL), COALESCE(d.rank, 0), d.id) %s (%t, %d, %d)",
			cmp, seek.RankNull, rank, seek.ID)))
	}
	return collectKeysetRows[DependentRow](ctx, pool,
		q.OrderBy(order...).Limit(uint64(limit+1)), backward, "dependents")
}

// MaxRank is the O(1) global-list count estimate (07 §3.4).
func MaxRank(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var n *int64
	if err := pool.QueryRow(ctx, "SELECT max(rank)::bigint FROM domain").Scan(&n); err != nil {
		return 0, fmt.Errorf("max rank: %w", err)
	}
	if n == nil {
		return 0, nil
	}
	return *n, nil
}
