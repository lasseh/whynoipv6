#!/usr/bin/env bash
# Nightly backup + version-of-record monitoring (09-ops.md §10.5): alerts the
# ops webhook when the newest backup is >26h old or the newest WAL is >1h old.
set -euo pipefail
log=/var/log/pgbackrest/verify.log
info=$(pgbackrest --stanza=whynoipv6 info --output=json)
versions=$(psql -At -d whynoipv6 -c "SELECT version(), (SELECT extversion FROM pg_extension WHERE extname='timescaledb')")
printf '%s %s\n%s %s\n' "$(date -Is)" "$info" "$(date -Is)" "$versions" >> "$log"

alert() {
  [ -n "${OPS_WEBHOOK_URL:-}" ] || return 0
  curl -fsS --max-time 15 -X POST "$OPS_WEBHOOK_URL" -H "Content-Type: application/json" \
    -d "{\"level\":\"error\",\"source\":\"pgbackrest-verify\",\"text\":\"$1\"}" || true
}

newest_backup=$(echo "$info" | jq -r '.[0].backup[-1].timestamp.stop // 0')
if [ "$(( $(date +%s) - newest_backup ))" -gt $((26 * 3600)) ]; then
  alert "newest pgbackrest backup older than 26h"
fi
newest_wal=$(echo "$info" | jq -r '.[0].archive[-1].max // empty')
if [ -z "$newest_wal" ]; then
  alert "no archived WAL recorded"
fi
wal_age=$(psql -At -c "SELECT coalesce(extract(epoch FROM now() - last_archived_time)::int, -1) FROM pg_stat_archiver")
if [ "$wal_age" -lt 0 ] || [ "$wal_age" -gt 3600 ]; then
  alert "newest archived WAL older than 1h"
fi
