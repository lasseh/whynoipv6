package postgres

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// collectKeysetRows executes a built keyset window and restores display
// order after a backward walk — the run half shared by every seek builder.
func collectKeysetRows[T any](ctx context.Context, pool *pgxpool.Pool,
	q sq.SelectBuilder, backward bool, label string,
) ([]T, error) {
	sqlText, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build %s: %w", label, err)
	}
	rows, err := pool.Query(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[T])
	if err != nil {
		return nil, fmt.Errorf("%s scan: %w", label, err)
	}
	if backward {
		reverseRows(out)
	}
	return out, nil
}

// reverseRows restores display order after a backward (prev_cursor) fetch;
// the N+1 overflow row sits at index 0 afterwards, so callers trim the
// FRONT on backward pages.
func reverseRows[T any](rows []T) {
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
}
