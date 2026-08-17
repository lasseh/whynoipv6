#!/bin/sh
# unbound_stats.control shim for the dev-compose scraper sidecar. `v6ctl ops
# unbound-stats` invokes `<control> -s 127.0.0.1@8953|8954 stats` (real
# unbound-control syntax — it has no -p flag, so the earlier `-p <port>`
# contract could never work against a host binary; 09-ops §8). Here each port
# maps to one unbound container, reached over the compose network on the
# default control port with the certless client config.
case "$2" in
  *@8954) host=unbound2 ;;
  *)      host=unbound1 ;;
esac
# unbound-control -s takes an IP, not a hostname — resolve the service name.
ip="$(getent hosts "$host" | awk '{print $1; exit}')"
exec unbound-control -c /etc/unbound/control-client.conf -s "${ip}@8953" "$3"
