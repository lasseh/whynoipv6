# P1.12 — Resolver-latency spike (risk gate)

Harness: internal/consensus/latencyspike (build tag `spike`, throwaway — go run -tags spike).
Targets: the three consensus providers (package constants) + the two compose Unbound instances.

RESULT: GREEN — the ~24 qps/provider consensus budget (00-overview.md PUBLIC_RESOLVER_QPS) is
comfortably feasible: every provider sustained 24 qps × 10 s with 0 failures; worst-provider
p95 (google, 56.5 ms) adds ≈0.1 s to a consensus fan-out — negligible against the ≈6 s
MEAN_SCAN_DURATION and a <24 h 1M pass (design §2.7). Bulk path via local Unbound answers
cached lookups sub-ms; cold-recursion tail (p99 ≈350 ms) is absorbed by per-check timeouts.
No v1-revert / 4th-resolver escape hatch needed.

# Resolver latency spike — 2026-07-10T10:25:29Z

## cloudflare (1.1.1.1:53)
sequential 200 queries: p50 11.3 ms, p95 21.2 ms, p99 42.7 ms, errors 0
sustained 24 qps × 10 s: 240 ok, 0 failed, p50 12.1 ms, p95 14.4 ms

## google (8.8.8.8:53)
sequential 200 queries: p50 26.4 ms, p95 65.5 ms, p99 162.6 ms, errors 0
sustained 24 qps × 10 s: 240 ok, 0 failed, p50 26.2 ms, p95 56.5 ms

## quad9 (9.9.9.9:53)
sequential 200 queries: p50 8.2 ms, p95 12.3 ms, p99 234.3 ms, errors 0
sustained 24 qps × 10 s: 240 ok, 0 failed, p50 9.6 ms, p95 13.4 ms

## unbound1 (127.0.0.1:5301)
sequential 200 queries: p50 0.7 ms, p95 105.8 ms, p99 354.8 ms, errors 0
sustained 24 qps × 10 s: 240 ok, 0 failed, p50 1.3 ms, p95 1.9 ms

## unbound2 (127.0.0.1:5302)
sequential 200 queries: p50 0.7 ms, p95 155.1 ms, p99 348.4 ms, errors 0
sustained 24 qps × 10 s: 240 ok, 0 failed, p50 1.8 ms, p95 2.1 ms

