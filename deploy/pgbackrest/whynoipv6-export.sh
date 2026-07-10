#!/usr/bin/env bash
# Weekly logical export of the two irreplaceable tables (09-ops.md §10.3):
# plain CSV via COPY — zero PG/extension version coupling on restore.
set -euo pipefail
d=$(date +%F); out=/var/backups/whynoipv6
psql -Atq service=whynoipv6 -c "COPY (SELECT * FROM changelog ORDER BY ts) TO STDOUT WITH (FORMAT csv, HEADER)" | zstd -q -o "$out/changelog-$d.csv.zst"
psql -Atq service=whynoipv6 -c "COPY (SELECT * FROM domain ORDER BY id) TO STDOUT WITH (FORMAT csv, HEADER)" | zstd -q -o "$out/domain-$d.csv.zst"
rsync -a "$out/" pgbackrest@{{ backup_host }}:/srv/logical-exports/whynoipv6/
