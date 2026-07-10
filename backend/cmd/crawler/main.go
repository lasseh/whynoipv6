// Command crawler is the autonomous scanning daemon of the whynoipv6 backend.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/lasseh/whynoipv6/internal/config"
)

func main() {
	cfg, err := config.Load("crawler")
	if err != nil {
		fmt.Fprintln(os.Stderr, "crawler: "+err.Error())
		os.Exit(1)
	}
	log := cfg.InstallLogger()
	cfg.LogSummary(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Info("shutting down")
}
