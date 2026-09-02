package postgres

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ASNSeek is the decoded (count, number) leaderboard cursor tuple; the
// count column is chosen by the sort.
type ASNSeek struct {
	Count  int32
	Number int64
}

// ASNRow is the §4.6 network row scanned via RowToStructByName.
type ASNRow struct {
	Number     int64  `db:"number"`
	Name       string `db:"name"`
	CountTotal int32  `db:"count_total"`
	CountV6    int32  `db:"count_v6"`
}

// ListASNLeaderboard runs one keyset window over the hosting-ASN league
// table (07 §4.6): (count, number) descending on the chosen count column,
// optional ?q= substring on the name, the direction flip derived from the
// backward flag — one builder instead of the sort × direction query matrix.
// Returns up to limit+1 rows in display order; a backward window carries
// its overflow row at the front.
func ListASNLeaderboard(ctx context.Context, pool *pgxpool.Pool, nameQuery string,
	byTotal bool, seek *ASNSeek, limit int, backward bool,
) ([]ASNRow, error) {
	col := "count_v6"
	if byTotal {
		col = "count_total"
	}
	cmp, dir := "<", "DESC"
	if backward {
		cmp, dir = ">", "ASC"
	}

	q := sq.Select("number", "name", "count_total", "count_v6").
		From("asn").
		PlaceholderFormat(sq.Dollar)
	if nameQuery != "" {
		q = q.Where("name ILIKE ?", likeSubstring(nameQuery))
	}
	if seek != nil {
		q = q.Where(fmt.Sprintf("(%s, number) %s (?, ?)", col, cmp), seek.Count, seek.Number)
	}

	return collectKeysetRows[ASNRow](ctx, pool,
		q.OrderBy(fmt.Sprintf("%s %s, number %s", col, dir, dir)).Limit(fetchLimit(limit)),
		backward, "asn leaderboard")
}
