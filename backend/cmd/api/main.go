// Command api serves the public HTTP surface (07-api.md §1): loopback bind
// behind nginx, per-request timeouts, and a graceful drain as long as the
// longest request the middleware allows.
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

	"github.com/lasseh/whynoipv6/internal/postgres"

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
	cfg.LogSummary(log, slog.LevelInfo)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	apiOpts := api.OptionsFrom(cfg)
	apiOpts.PublicBaseURL = cfg.PublicBaseURL
	apiOpts.DatasetsDir = cfg.DatasetsDir

	srv := &http.Server{
		Addr:              cfg.APIListen,
		Handler:           api.NewRouter(pool, apiOpts),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      api.RequestTimeout,
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

	// Graceful shutdown: the drain waits out the longest request the
	// middleware allows (07 §1.6 erratum, review issue 39). At the old 15s
	// a request legitimately running 15–30s — a 10k-row ?format=csv list —
	// was severed mid-body on every deploy, Shutdown returned
	// context.DeadlineExceeded, and a routine restart left the unit failed.
	// systemd's 90s TimeoutStopSec default still covers this comfortably;
	// the api unit sets none of its own.
	drainCtx, cancel := context.WithTimeout(context.Background(), api.RequestTimeout)
	defer cancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	slog.Info("api shutdown complete")
	return nil
}
