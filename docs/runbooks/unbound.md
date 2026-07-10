# Runbook — Unbound resolver pair

The crawler's bulk resolver is two local Unbound instances
(`unbound@1` on `127.0.0.1:53`, `unbound@2` on `127.0.0.1:5353`;
`deploy/unbound/`). The consensus path (cloudflare/google/quad9) does
NOT go through them — only bulk lookups (NS/MX per-host details,
resource sweep, preflight) do.

## Triggers

- Grafana A-series alert on scan error rate, or `v6ctl ops
  unbound-stats` showing a cache-hit collapse / SERVFAIL spike.
- `systemctl status unbound@1 unbound@2` shows a dead instance.
- Crawler logs `preflight failed` repeatedly (the probe host resolves
  through the bulk path).

## Diagnosis

1. `systemctl status unbound@1 unbound@2` — both must be active; the
   crawler units order `After=unbound@1.service unbound@2.service`.
2. `unbound-control -c /etc/unbound/unbound-1.conf stats_noreset | grep
   -E 'total.num|answer.rcode.SERVFAIL'` — SERVFAIL ratio over ~5% means
   upstream/network trouble, not Unbound.
3. `dig @127.0.0.1 -p 53 AAAA one.one.one.one` and the same against
   `-p 5353` — one failing instance implicates that instance only.
4. Check the hourly `unbound_stats` rows (`v6ctl ops unbound-stats`
   writes them; Grafana panel) for cache-hit-rate trends.

## Recovery

- One instance down: `systemctl restart unbound@<n>`. The resolver
  seam round-robins upstreams and tolerates a single dead instance;
  no crawler restart needed.
- Both down: restart both, then confirm the crawler preflight passes
  (`journalctl -u whynoipv6-crawler | grep preflight | tail`). The
  crawler idles (does not crash) while preflight fails — scans resume
  on the next pass.
- Config change: edit `deploy/unbound/unbound-base.conf` or the
  per-instance drop-in, `unbound-checkconf`, then restart one instance
  at a time.

## Notes

- The resetting `stats` variant is used by the hourly collector — do
  not run `unbound-control stats` by hand between collector runs or
  that hour's row under-reports; use `stats_noreset` interactively.
