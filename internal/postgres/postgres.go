package postgres

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

// NewPostgreSQL creates a new PostgreSQL connection pool with retries and timeout.
// It takes the connection string and returns a pool or an error.
func NewPostgreSQL(conf string, maxRetries int, timeout time.Duration) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var pool *pgxpool.Pool
	var err error

	for i := range maxRetries {
		slog.Info("Attempting to connect to PostgreSQL", "attempt", i+1)
		pool, err = pgxpool.Connect(ctx, conf)
		if err != nil {
			slog.Warn("Failed to connect to PostgreSQL", "error", err, "attempt", i+1)
			if i == maxRetries-1 {
				break
			}
			time.Sleep(2 * time.Second)
			continue
		}

		// Test the connection
		if pingErr := pool.Ping(ctx); pingErr != nil {
			slog.Warn("Failed to ping PostgreSQL", "error", pingErr, "attempt", i+1)
			if i == maxRetries-1 {
				err = pingErr
				break
			}
			pool.Close()
			time.Sleep(2 * time.Second)
			continue
		}

		slog.Info("Successfully connected to PostgreSQL")
		return pool, nil
	}

	slog.Error("Exhausted retries for PostgreSQL connection", "error", err)
	return nil, errors.New("failed to connect to PostgreSQL after retries")
}
