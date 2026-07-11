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
