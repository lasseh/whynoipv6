#!/bin/sh
# unbound_stats.control shim for the dev compose stack. `v6ctl ops
# unbound-stats` invokes `<control> -p 8953|8954 stats` (the prod
# same-host convention, 09-ops §8); here each port maps to one unbound
# container and unbound-control runs inside it over the loopback,
# certless control socket.
dir="$(cd "$(dirname "$0")/../../.." && pwd)"
case "$2" in
  8954) svc=unbound2 ;;
  *)    svc=unbound1 ;;
esac
exec docker compose --project-directory "$dir" exec -T "$svc" unbound-control "$3"
