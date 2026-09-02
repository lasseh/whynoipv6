package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/lasseh/whynoipv6/internal/domain"
)

// retldCmd is the one-off backfill for review issue 34: `domain.tld` used to
// come from two public-suffix snapshots depending on which ingress created
// the row — x/net/publicsuffix on the Tranco path, weppos elsewhere — so a
// suffix the two disagreed about was split across two `?tld=` facets. Both
// paths now go through domain.TLD; existing rows still carry whichever
// answer their ingress gave at insert time.
//
// Idempotent and safe to re-run: it rewrites only rows whose stored value
// differs from what the single derivation produces today, which also makes
// it useful after a PSL bump.
func domainCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "domain", Short: "Domain-row maintenance"}

	var dryRun bool
	retld := &cobra.Command{
		Use:   "retld",
		Short: "Recompute domain.tld from the single public-suffix derivation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool, err := newPool(cmd)
			if err != nil {
				return err
			}
			defer pool.Close()
			return retld(cmd.Context(), pool, dryRun)
		},
	}
	retld.Flags().BoolVar(&dryRun, "dry-run", false,
		"report what would change without writing")
	cmd.AddCommand(retld)
	return cmd
}

// retldRow is one host whose stored tld disagrees with the derivation.
type retldRow struct {
	id       int64
	host     string
	kind     string
	from, to string
}

func retld(ctx context.Context, pool *pgxpool.Pool, dryRun bool) error {
	rows, err := pool.Query(ctx,
		`SELECT id, host, kind::text, coalesce(tld, '') FROM domain ORDER BY id`)
	if err != nil {
		return fmt.Errorf("read domains: %w", err)
	}
	var changes []retldRow
	var scanned int
	for rows.Next() {
		var r retldRow
		if err := rows.Scan(&r.id, &r.host, &r.kind, &r.from); err != nil {
			rows.Close()
			return fmt.Errorf("scan: %w", err)
		}
		scanned++
		if r.to = domain.TLD(r.host); r.to != r.from {
			changes = append(changes, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read domains: %w", err)
	}

	byKind := map[string]int{}
	moves := map[string]int{}
	for _, c := range changes {
		byKind[c.kind]++
		moves[c.from+" → "+c.to]++
	}
	fmt.Printf("scanned %d domains, %d would change\n", scanned, len(changes))
	for _, k := range sortedKeys(byKind) {
		fmt.Printf("  kind %s: %d\n", k, byKind[k])
	}
	for _, m := range sortedKeys(moves) {
		fmt.Printf("  %s: %d\n", m, moves[m])
	}
	if len(changes) == 0 || dryRun {
		if dryRun && len(changes) > 0 {
			fmt.Println("dry run: nothing written")
		}
		return nil
	}

	batch := &pgx.Batch{}
	for _, c := range changes {
		batch.Queue("UPDATE domain SET tld = $2 WHERE id = $1", c.id, c.to)
	}
	br := pool.SendBatch(ctx, batch)
	defer br.Close() //nolint:errcheck // the Exec loop below surfaces the error
	for range changes {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("update tld: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("update tld: %w", err)
	}
	fmt.Printf("updated %d rows\n", len(changes))
	fmt.Println("NOTE: exported dataset values changed. Bump export.SchemaVersion " +
		"if consumers key on tld (07 §5.3).")
	return nil
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
