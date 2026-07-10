package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

func opsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ops",
		Short: "Operational helpers",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "unbound-stats",
		Short: "Scrape `unbound-control stats` (resetting) into unbound_stats (09-ops §8)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool, cfg, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			control := cfg.String("unbound_stats.control")
			hostname, _ := os.Hostname()
			q := db.New(pool)

			// One row per instance; the resetting `stats` variant yields
			// per-interval deltas (control ports 8953/8954 — 09-ops §8).
			for _, port := range []string{"8953", "8954"} {
				out, err := exec.CommandContext(cmd.Context(),
					control, "-p", port, "stats").Output() //nolint:gosec // operator-config command path
				if err != nil {
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
