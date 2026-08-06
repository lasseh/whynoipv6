#!/usr/bin/env python3
"""Publish the public dashboard as a Grafana *snapshot*: a static artifact with
the query results baked in.

Why this exists (09-ops.md 12.1). A shared dashboard keeps a live path from an
anonymous visitor to Postgres. Grafana only ever runs the panel's own stored
SQL, so there is nothing to inject, but every page load is still real database
work and an unauthenticated visitor controls how often it happens. A snapshot
removes the path entirely: each panel carries its own data, the datasource
reference is gone, and the artifact is a few tens of KB that any cache or CDN
can serve.

The cost is freshness, so run this on a timer. Every run publishes a new
snapshot and deletes the one it replaces, keeping exactly one live URL.

Usage:
    GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=... ./snapshot-public.py
    ./snapshot-public.py --uid wni6-public --state deploy/grafana/.snapshot-state

Stdlib only, so it runs anywhere python3 does. Read-only against Grafana apart
from the snapshot create/delete calls.
"""
import argparse
import base64
import json
import os
import sys
import urllib.error
import urllib.request

DEFAULT_URL = os.environ.get("GRAFANA_URL", "http://localhost:3000")


def auth_header():
    """Prefer a service-account token; fall back to basic auth for dev."""
    token = os.environ.get("GRAFANA_TOKEN")
    if token:
        return "Bearer " + token
    user = os.environ.get("GRAFANA_USER", "admin")
    pw = os.environ.get("GRAFANA_PASSWORD", "admin")
    return "Basic " + base64.b64encode(f"{user}:{pw}".encode()).decode()


def call(base, path, data=None, method=None):
    req = urllib.request.Request(
        base + path,
        data=json.dumps(data).encode() if data is not None else None,
        headers={"Content-Type": "application/json", "Authorization": auth_header()},
        method=method)
    with urllib.request.urlopen(req, timeout=180) as r:
        body = r.read()
    return json.loads(body) if body else {}


def frames_to_snapshot(frames):
    """Convert query response frames into Grafana's snapshotData shape.

    The postgres datasource stamps the fully-expanded SQL into
    frame.schema.meta.executedQueryString. It is inert inside a snapshot (there
    is no datasource left to run it), but it would publish every table and
    column name we touch, so it is dropped rather than shipped.
    """
    out = []
    for f in frames:
        fields = [
            {"name": s["name"], "type": s.get("type", "string"),
             "values": v, "config": s.get("config", {})}
            for s, v in zip(f["schema"]["fields"], f["data"]["values"])
        ]
        meta = {k: v for k, v in (f["schema"].get("meta") or {}).items()
                if k != "executedQueryString"}
        out.append({"fields": fields, "meta": meta})
    return out


def build(base, uid):
    dash = call(base, f"/api/dashboards/uid/{uid}")["dashboard"]
    frm, to = dash["time"]["from"], dash["time"]["to"]
    embedded = failed = 0

    for panel in dash.get("panels", []):
        queries = [
            {"refId": t.get("refId", "A"), "datasource": panel.get("datasource"),
             "rawSql": t["rawSql"], "format": t.get("format", "table"),
             "intervalMs": 86400000, "maxDataPoints": 800}
            for t in (panel.get("targets") or []) if t.get("rawSql")
        ]
        if not queries:
            continue
        try:
            res = call(base, "/api/ds/query",
                       {"queries": queries, "from": frm, "to": to})
        except urllib.error.HTTPError as e:
            print(f"  ! {panel.get('title')}: HTTP {e.code}", file=sys.stderr)
            failed += 1
            continue
        snap = []
        for r in res["results"].values():
            snap.extend(frames_to_snapshot(r.get("frames", [])))
        panel["snapshotData"] = snap
        # The point of the exercise: no SQL and no datasource survive.
        panel.pop("targets", None)
        panel.pop("datasource", None)
        embedded += 1

    dash["snapshot"] = {"timestamp": None}
    dash["refresh"] = ""
    return dash, embedded, failed


def assert_clean(dash):
    """Refuse to publish anything still carrying SQL or a datasource handle."""
    blob = json.dumps(dash)
    for probe in ("rawSql", "executedQueryString", "whynoipv6-pg"):
        if probe in blob:
            raise SystemExit(f"refusing to publish: {probe!r} still present")
    live = [p.get("title") for p in dash.get("panels", []) if p.get("targets")]
    if live:
        raise SystemExit(f"refusing to publish: panels still query live: {live}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--uid", default="wni6-public")
    ap.add_argument("--url", default=DEFAULT_URL)
    ap.add_argument("--state", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), ".snapshot-state"))
    ap.add_argument("--keep", action="store_true", help="do not delete the previous snapshot")
    args = ap.parse_args()

    dash, embedded, failed = build(args.url, args.uid)
    if not embedded:
        raise SystemExit("no panels embedded, refusing to publish an empty snapshot")
    assert_clean(dash)

    resp = call(args.url, "/api/snapshots",
                {"dashboard": dash, "name": dash["title"], "expires": 0})
    key, delete_key = resp.get("key"), resp.get("deleteKey")

    prev = None
    if os.path.exists(args.state):
        try:
            prev = json.load(open(args.state))
        except (json.JSONDecodeError, OSError):
            prev = None
    json.dump({"key": key, "deleteKey": delete_key, "url": resp.get("url")},
              open(args.state, "w"), indent=2)

    if prev and prev.get("deleteKey") and not args.keep:
        try:
            call(args.url, f"/api/snapshots-delete/{prev['deleteKey']}")
            print(f"deleted previous snapshot {prev.get('key')}")
        except urllib.error.HTTPError as e:
            print(f"  ! could not delete {prev.get('key')}: HTTP {e.code}", file=sys.stderr)

    print(f"panels embedded : {embedded}" + (f" ({failed} failed)" if failed else ""))
    print(f"snapshot url    : {resp.get('url')}")


if __name__ == "__main__":
    main()
