# Curated subdomain lists

Staged here; copy into `whynoipv6-campaign/SUBDOMAINS.md` (contributor docs
for the `subdomains/` directory). Normative rules live in
`docs/spec/06-ingest.md` §3.7.

---

A domain can pass every check on its front page while the part people
actually use is still IPv4 only. The apex answers over IPv6, `www` answers
over IPv6, four stars, and then the login portal or the API that the site
depends on has no AAAA record at all.

Subdomain lists let you name those hosts. Anything you list gets checked on
the same schedule and with the same checks as any other domain on Why No
IPv6, and shows up in a Subdomains table on the parent domain's page.

## Adding a list

One file per domain, named after the domain, in `subdomains/`:

```yaml
# subdomains/nrk.no.yml
subdomains:
  - tv
  - radio
  - secure.login
```

That tracks `tv.nrk.no`, `radio.nrk.no` and `secure.login.nrk.no`.

Entries are written **relative to the domain in the filename**. Write `tv`,
not `tv.nrk.no`. A list can only ever cover its own domain, which is the
point: no file can quietly add hosts belonging to someone else.

Open a pull request with the file. CI validates it and comments on the PR
with what it found. Once merged, the sync picks the file up and the crawler
starts checking those hosts on its next run.

## Rules

- **The domain must already be tracked** on the site. Subdomain lists add
  hosts under a domain that is already there; they cannot introduce a new
  domain. If the domain is missing, add it through a campaign file first, or
  run a live check on it.
- **The filename is the domain**, lowercase, exactly as it appears on the
  site: `nrk.no.yml`, not `NRK.no.yml` or `www.nrk.no.yml`. For
  internationalized domains use the punycode form (`xn--...`).
- **Up to 20 entries per file.** These lists name the endpoints a service
  needs to work, not every host in a zone.
- **No bare `www`.** Every domain is already checked for `www`, and the
  result has its own row on the domain page. Deeper names that start with
  `www`, like `www.tv`, are fine.
- **Multi-level labels are fine**: `secure.login` gives you
  `secure.login.nrk.no`.
- One invalid entry fails the whole file, so CI will tell you before it can
  affect anything.

## Removing entries

Delete the line, or delete the file. Nothing disappears immediately: a host
that stops being listed keeps its page and its history for 30 days, then
drops out of the index. Put it back within that window and it carries on as
if nothing happened.

## What this does not do

Subdomain results are **informational**. They never change the parent
domain's rating, and they never move the country or campaign numbers.

That is deliberate. What ends up in these lists depends entirely on who took
the time to write them, so a domain would otherwise score worse simply for
having someone pay attention to it. The lists are there to show where a
service still depends on IPv4, not to re-score the domain.
