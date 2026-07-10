//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

// The claim query's inner SELECT, textually matching 04-lifecycle-scheduling.md
// §3 (and the idx_domain_due predicate, 05-schema.md §3). $1 = claim.batch_size.
const claimInnerSelect = `SELECT id FROM domain
  WHERE (NOT disabled OR disabled_reason IN ('dead', 'delisted'))
    AND next_check_at <= now()
    AND (claimed_at IS NULL OR claimed_at < now() - interval '30 minutes')
  ORDER BY rank ASC NULLS LAST, next_check_at ASC
  LIMIT 200
  FOR UPDATE SKIP LOCKED`

type explainNode struct {
	NodeType   string        `json:"Node Type"`
	IndexName  string        `json:"Index Name"`
	IndexCond  string        `json:"Index Cond"`
	SortKey    []string      `json:"Sort Key"`
	SortMethod string        `json:"Sort Method"`
	ActualRows json.Number   `json:"Actual Rows"`
	Plans      []explainNode `json:"Plans"`
}

type explainResult struct {
	Plan          explainNode `json:"Plan"`
	ExecutionTime float64     `json:"Execution Time"`
}

func explainClaim(t *testing.T, pool *pgxpool.Pool) explainResult {
	t.Helper()
	var raw []byte
	err := pool.QueryRow(context.Background(),
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+claimInnerSelect).Scan(&raw)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	var res []explainResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("parse explain json: %v", err)
	}
	return res[0]
}

func findNode(n explainNode, pred func(explainNode) bool) *explainNode {
	if pred(n) {
		return &n
	}
	for _, c := range n.Plans {
		if hit := findNode(c, pred); hit != nil {
			return hit
		}
	}
	return nil
}

// TestClaimPlanGate is the P1.11 risk gate: at 1M rows with a near-empty due
// backlog the claim inner SELECT must be an index scan on idx_domain_due with
// next_check_at as the index condition, top-N sorted on (rank NULLS LAST,
// next_check_at), executing < 50 ms; the empty-frontier probe < 5 ms; the
// full-backlog case is exercised and its O(due) cost recorded.
func TestClaimPlanGate(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()

	// Seed 1M ranked apexes, next_check_at spread over the NEXT 24h (not
	// yet due), then pull 500 into the past — the near-empty (<1k) backlog.
	t.Log("seeding 1,000,000 domain rows…")
	start := time.Now()
	_, err := pool.Exec(ctx, `
		INSERT INTO domain (host, kind, rank, created_by, asn_id, country_id, tld, next_check_at)
		SELECT 'd' || g || '.example', 'apex', g, 'tranco',
		       (SELECT id FROM asn WHERE number = 0),
		       (SELECT id FROM country WHERE code = 'UN'),
		       'example',
		       now() + interval '1 minute' + (g % 86400) * interval '1 second'
		FROM generate_series(1, 1000000) g`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"UPDATE domain SET next_check_at = now() - interval '1 minute' WHERE rank % 2000 = 17"); err != nil {
		t.Fatalf("mark due: %v", err)
	}
	if _, err := pool.Exec(ctx, "VACUUM ANALYZE domain"); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	t.Logf("seeded in %s", time.Since(start).Round(time.Second))

	// Near-empty backlog (500 due of 1M).
	res := explainClaim(t, pool)
	plan, _ := json.MarshalIndent(res, "", "  ")
	t.Logf("near-empty-backlog plan (execution %.2f ms):\n%s", res.ExecutionTime, plan)

	idx := findNode(res.Plan, func(n explainNode) bool { return n.IndexName == "idx_domain_due" })
	if idx == nil {
		t.Fatalf("STOP-AND-REPORT: claim plan does not use idx_domain_due:\n%s", plan)
	}
	if !strings.Contains(idx.IndexCond, "next_check_at") {
		t.Errorf("index condition %q does not bound next_check_at", idx.IndexCond)
	}
	sort := findNode(res.Plan, func(n explainNode) bool {
		return n.NodeType == "Sort" && len(n.SortKey) >= 1 && strings.Contains(n.SortKey[0], "rank")
	})
	if sort == nil {
		t.Errorf("no top-N sort on (rank NULLS LAST, next_check_at) in plan:\n%s", plan)
	} else if sort.SortMethod != "" && !strings.Contains(sort.SortMethod, "top-N") {
		t.Logf("note: sort method is %q (expected a top-N heapsort)", sort.SortMethod)
	}
	if res.ExecutionTime >= 50 {
		t.Errorf("STOP-AND-REPORT: near-empty-backlog claim took %.2f ms, gate is < 50 ms", res.ExecutionTime)
	}

	// Full backlog (all 1M due): exercised, O(due) cost recorded, not gated.
	if _, err := pool.Exec(ctx, "UPDATE domain SET next_check_at = now() - interval '1 hour'"); err != nil {
		t.Fatalf("full backlog: %v", err)
	}
	if _, err := pool.Exec(ctx, "VACUUM ANALYZE domain"); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	full := explainClaim(t, pool)
	t.Logf("full-backlog (1M due) execution: %.2f ms — O(due) recovery-regime cost, accepted per 04 §3", full.ExecutionTime)
}

// TestClaimPlanEmptyFrontier: the idle probe on an empty frontier is a
// sub-5ms range probe.
func TestClaimPlanEmptyFrontier(t *testing.T) {
	pool := pgtest.NewDB(t)
	res := explainClaim(t, pool)
	t.Logf("empty-frontier execution: %.3f ms", res.ExecutionTime)
	if res.ExecutionTime >= 5 {
		t.Errorf("empty-frontier claim took %.2f ms, gate is < 5 ms", res.ExecutionTime)
	}
}
