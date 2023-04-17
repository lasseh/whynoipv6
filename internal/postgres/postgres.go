package postgres

import (
	"context"

	"github.com/jackc/pgx/v4/pgxpool"
)

// NewPostgreSQL instantiates a new pgxpool.Pool for a PostgreSQL database.
// It takes the connection string as an argument and returns a pointer to the pool if successful,
// or an error if the connection or ping fails.
func NewPostgreSQL(conf string) (*pgxpool.Pool, error) {
	ctx := context.Background()
	pool, err := pgxpool.Connect(ctx, conf)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}

	return pool, nil
}
