// Command api serves the public HTTP surface (07-api.md §1): loopback bind
// behind nginx, per-request timeouts, 15s graceful drain.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/api"
	"github.com/lasseh/whynoipv6/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("api")
	if err != nil {
		return err
	}
	log, flushLogs, err := cfg.InstallLogger()
	if err != nil {
		return err
	}
	defer flushLogs()
	cfg.LogSummary(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open pool: %w", err)
	}
	defer pool.Close()

	srv := &http.Server{
		Addr: cfg.APIListen,
		Handler: api.NewRouter(pool, api.Options{
			PublicBaseURL:     cfg.PublicBaseURL,
			CSVMaxRows:        cfg.Int("export.csv_max_rows"),
			DatasetsDir:       cfg.DatasetsDir,
			RateIPPerHour:     cfg.Int("live_check.rate_ip_per_hour"),
			RateGlobalPerHour: cfg.Int("live_check.rate_global_per_hour"),
			DedupeWindow:      cfg.Duration("live_check.dedupe_window"),
			ResourcesEnabled:  cfg.Bool("crawler.resources.enabled"),
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("api listening", "addr", cfg.APIListen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	// Graceful shutdown: 15s drain budget (07 §1.6).
	drainCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	slog.Info("api shutdown complete")
	return nil
}
