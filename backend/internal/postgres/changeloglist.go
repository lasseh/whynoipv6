package postgres

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ChangelogFilter is the validated /changelog read filter (07 §4.8):
// optional per-domain scope, dimension, and time window.
type ChangelogFilter struct {
	DomainID *int64
	Field    string // "" = all dimensions
	From, To *time.Time
}

// ChangelogSeek is the decoded (ts, domain_id, field) cursor tuple.
type ChangelogSeek struct {
	TS     time.Time
	Domain int64
	Field  string
}

// ChangelogRow is the feed row scanned via RowToStructByName.
type ChangelogRow struct {
	Ts       time.Time `db:"ts"`
	Host     string    `db:"host"`
	Field    string    `db:"field"`
	OldValue string    `db:"old_value"`
	NewValue string    `db:"new_value"`
	DomainID int64     `db:"domain_id"`
}

// ListChangelog runs one keyset window over the global or per-domain feed:
// (ts, domain_id, field) descending, the direction flip derived from the
// backward flag — one query text instead of a forward/backward SQL pair.
// Returns up to limit+1 rows in display order; a backward window carries
// its overflow row at the front.
func ListChangelog(ctx context.Context, pool *pgxpool.Pool, f *ChangelogFilter,
	seek *ChangelogSeek, limit int, backward bool,
) ([]ChangelogRow, error) {
	cmp, order := "<", "cl.ts DESC, cl.domain_id DESC, cl.field DESC"
	if backward {
		cmp, order = ">", "cl.ts ASC, cl.domain_id ASC, cl.field ASC"
	}

	q := sq.Select("cl.ts", "d.host", "cl.field",
		"cl.old_value::text AS old_value", "cl.new_value::text AS new_value", "cl.domain_id").
		From("changelog cl").
		Join("domain d ON d.id = cl.domain_id").
		PlaceholderFormat(sq.Dollar)
	if f.DomainID != nil {
		q = q.Where(sq.Eq{"cl.domain_id": *f.DomainID})
	}
	if f.Field != "" {
		q = q.Where(sq.Eq{"cl.field": f.Field})
	}
	if f.From != nil {
		q = q.Where(sq.GtOrEq{"cl.ts": *f.From})
	}
	if f.To != nil {
		q = q.Where(sq.LtOrEq{"cl.ts": *f.To})
	}
	if seek != nil {
		q = q.Where(fmt.Sprintf("(cl.ts, cl.domain_id, cl.field) %s (?, ?, ?)", cmp),
			seek.TS, seek.Domain, seek.Field)
	}

	return collectKeysetRows[ChangelogRow](ctx, pool,
		q.OrderBy(order).Limit(uint64(limit+1)), backward, "changelog list")
}
