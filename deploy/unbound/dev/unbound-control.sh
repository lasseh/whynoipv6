#!/bin/sh
# unbound_stats.control shim for the dev-compose scraper sidecar. `v6ctl ops
# unbound-stats` invokes `<control> -p 8953|8954 stats` (the prod same-host
# convention, 09-ops §8); here each port maps to one unbound container,
# reached over the compose network on the default control port with the
# certless client config.
case "$2" in
  8954) host=unbound2 ;;
  *)    host=unbound1 ;;
esac
exec unbound-control -c /etc/unbound/control-client.conf -s "${host}@8953" "$3"
