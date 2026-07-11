package postgres

import (
	"context"
	"fmt"
	"time"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// Generation returns the envelope meta sources (07 §2.4): generation =
// integer YYYYMMDD of max(stats_global_daily.day); as_of = its
// generated_at, falling back to the day at 00:00:00Z (the day-0 seed row).
func Generation(ctx context.Context, q *db.Queries) (generation int32, asOf time.Time, err error) {
	row, err := q.StatsGeneration(ctx)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("stats generation: %w", err)
	}
	day := row.Day.Time.UTC()
	generation = int32(day.Year()*10000 + int(day.Month())*100 + day.Day()) //nolint:gosec // YYYYMMDD fits int32
	asOf = day
	if row.GeneratedAt.Valid {
		asOf = row.GeneratedAt.Time.UTC()
	}
	return generation, asOf, nil
}
