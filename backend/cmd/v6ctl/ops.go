package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// opsCmd holds the operational helpers (09-ops.md §8).
func opsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ops",
		Short: "Operational helpers",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "unbound-stats",
		Short: "Scrape `unbound-control stats` (resetting) into unbound_stats",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := cfgFromCmd(cmd)
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			control := cfg.String("unbound_stats.control")
			hostname, _ := os.Hostname()
			q := db.New(pool)

			// One row per instance; the resetting `stats` variant yields
			// per-interval deltas (control ports 8953/8954 — 09-ops §8).
			// Port selection is `-s ip@port` — unbound-control has no -p
			// flag, so the old `-p <port>` form exited 1 on every real
			// host and only ever worked through the dev-compose shim.
			inserted := 0
			for _, port := range []string{"8953", "8954"} {
				//nolint:gosec // operator-config command path
				out, err := exec.CommandContext(cmd.Context(), control, "-s", "127.0.0.1@"+port, "stats").Output()
				if err != nil {
					// Output keeps the child's stderr on the ExitError;
					// without it every failure reads "exit status 1".
					var ee *exec.ExitError
					if errors.As(err, &ee) && len(ee.Stderr) > 0 {
						err = fmt.Errorf("%w: %s", err, bytes.TrimSpace(ee.Stderr))
					}
					fmt.Fprintf(os.Stderr, "instance %s: %v\n", port, err)
					continue
				}
				stats := parseUnboundStats(string(out))
				raw, _ := json.Marshal(stats)
				params := db.InsertUnboundStatsParams{
					Host: hostname + ":" + port,
					Raw:  raw,
				}
				params.NumQueries = i64ptr(stats, "total.num.queries")
				params.CacheHits = i64ptr(stats, "total.num.cachehits")
				params.CacheMiss = i64ptr(stats, "total.num.cachemiss")
				params.RcodeServfail = i64ptr(stats, "num.answer.rcode.SERVFAIL")
				params.RcodeNxdomain = i64ptr(stats, "num.answer.rcode.NXDOMAIN")
				if v, ok := stats["total.recursion.time.avg"]; ok {
					ms := float32(v * 1000)
					params.RecursionTimeAvgMs = &ms
				}
				if v, ok := stats["total.requestlist.avg"]; ok {
					avg := float32(v)
					params.RequestlistAvg = &avg
				}
				if err := q.InsertUnboundStats(cmd.Context(), params); err != nil {
					return err
				}
				inserted++
			}
			// The timer unit must go red, not green, when no instance
			// answered — the per-instance lines above are only diagnostics.
			if inserted == 0 {
				return errors.New("unbound-stats: no instance answered")
			}
			return nil
		},
	})
	return cmd
}

// parseUnboundStats parses `key=value` lines into a float map.
func parseUnboundStats(out string) map[string]float64 {
	stats := map[string]float64{}
	for line := range strings.Lines(out) {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			stats[key] = f
		}
	}
	return stats
}

func i64ptr(stats map[string]float64, key string) *int64 {
	if v, ok := stats[key]; ok {
		n := int64(v)
		return &n
	}
	return nil
}
