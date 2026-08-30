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
2. `unbound-control -c /etc/unbound/instances/1.conf stats_noreset | grep
   -E 'total.num|answer.rcode.SERVFAIL'` (use `2.conf` for the second
   instance) — SERVFAIL ratio over ~5% means upstream/network trouble,
   not Unbound.
3. `dig @127.0.0.1 -p 53 AAAA one.one.one.one` and the same against
   `-p 5353` — one failing instance implicates that instance only.
4. Check the per-minute `unbound_stats` rows (`v6ctl ops unbound-stats`
   writes them; Grafana panel) for cache-hit-rate trends.
5. Both instances dead with `fatal error: failed to init modules` and
   `failed to read /var/lib/unbound/root.key` in the journal — the DNSSEC
   trust anchor is empty, not a config error. `unbound-checkconf` reports
   it as a config failure, which sends you looking in the wrong place.
   Confirm with `stat -c '%s %U:%G' /var/lib/unbound/root.key`: expect
   `768 unbound:unbound`, never `0`.

## Recovery

- One instance down: `systemctl restart unbound@<n>`. The resolver
  seam round-robins upstreams and tolerates a single dead instance;
  no crawler restart needed.
- Both down: restart both, then confirm the crawler preflight passes
  (`journalctl -u whynoipv6-crawler | grep preflight | tail`). The
  crawler idles (does not crash) while preflight fails — scans resume
  on the next pass.
- Empty trust anchor (`root.key` is 0 bytes): stop both instances, then
  `systemctl reset-failed unbound@1 unbound@2` — the crash loop trips the
  start limit and a plain `start` gets refused. Then:

      rm -f /var/lib/unbound/root.key
      /usr/libexec/unbound-helper root_trust_anchor_update

  Deleting first is required: the helper guards on existence, not size, so
  it no-ops on a zero-byte file. Do not `cp` the key in as root — the
  helper copies via `setpriv --reuid=unbound`, and a root-owned anchor
  starts fine while breaking every future RFC 5011 update silently.
  Verify with `dig +dnssec @127.0.0.1 -p 53 cloudflare.com` and the same
  on `-p 5353`: the `ad` flag must be present, since a resolver with a
  dead validator still answers.
- Config change: edit `deploy/unbound/unbound-base.conf` or the
  per-instance drop-in, `unbound-checkconf`, then restart one instance
  at a time.

## Notes

- A full disk truncates `root.key` to zero during an RFC 5011 rewrite.
  The running daemons keep serving from their in-memory copy and only
  die at the next restart, so the outage can surface hours after the
  disk event (2026-08-30: truncated 06:34, both instances down from the
  07:18 reboot). The `anchor.conf` drop-in the Ansible role installs
  repairs this on every start.
- The resetting `stats` variant is used by the per-minute collector
  (`whynoipv6-unbound-stats.timer`, `OnCalendar=*:*:00`) — do not run
  `unbound-control stats` by hand between collector runs or that
  minute's row under-reports; use `stats_noreset` interactively.
