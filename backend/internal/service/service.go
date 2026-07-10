// Package service is the use-case layer the api handlers call (07-api.md):
// it owns the pool, the sqlc queries, and the envelope meta source.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// Service wires the read surface's data access.
type Service struct {
	Pool *pgxpool.Pool
	Q    *db.Queries
}

// New builds the service over an open pool.
func New(pool *pgxpool.Pool) *Service {
	return &Service{Pool: pool, Q: db.New(pool)}
}

// Ping reports database reachability (the /readyz probe).
func (s *Service) Ping(ctx context.Context) error {
	return s.Pool.Ping(ctx)
}

// Generation returns the envelope meta sources (07 §2.4): generation =
// integer YYYYMMDD of max(stats_global_daily.day); as_of = its
// generated_at, falling back to the day at 00:00:00Z (the day-0 seed row).
func (s *Service) Generation(ctx context.Context) (generation int32, asOf time.Time, err error) {
	row, err := s.Q.StatsGeneration(ctx)
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
