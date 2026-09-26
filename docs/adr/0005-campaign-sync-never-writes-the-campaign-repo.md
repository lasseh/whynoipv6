# 0005 — The campaign sync never writes to the campaign repo

- **Status:** Accepted
- **Date:** 2026-09-26
- **Deciders:** project owner
- **Touches:** `internal/campaign` (sync, config, validate), `internal/config` (registry), `cmd/v6ctl` (campaign sync output and exit reasons), `compose.yaml`, 06 §3.2/§3.3/§3.5/§4.2, 09 §2.6, `docs/deploy.md`, the `whynoipv6-campaign` Makefile

## Context

Two things wrote campaign uuids. The sync minted a uuid for any campaign file
without one, inserted the campaign under it, wrote the `uuid:` line back into
the file, and committed and pushed it (`campaign.push`). The campaign repo's
`make fix-uuids` did the same job with sed and awk. The spec said both "only
the import bot ever writes it" and "`make fix-uuids` fills empty `uuid:`
fields".

In practice the bot never pushed. Production runs the sync in the distroless
crawler with `CAMPAIGN_PUSH=false` against a read-only volume, and the campaign
repo's CI and assign-uuid workflow were dropped in the campaign repo's a389b61.
The maintainer has been assigning uuids with `make all`. The write-back
existed only to be failed or skipped. It also kept a second copy of the
uuid-splice rules, and the two copies had drifted apart on edge cases.

A file merged without a uuid could also fork a campaign's identity. A sync
running before `make all` landed would mint one uuid. The maintainer's commit
would then bring a different one, which the sync adopts as a new campaign
while disabling the minted one.

## Decision

**A human assigns uuids. The backend only reads.**

1. The maintainer runs `make all` in the campaign repo after merging a campaign
   PR. It assigns missing uuids and checks that every file has a unique,
   well-formed one. The repo's pre-push hook blocks a push that skipped it.
2. The sync never writes to the checkout and never pushes. `writeBackUUIDs`,
   `insertUUIDLine`, the push-failure rollback, `upsertNoUUID` and the
   `campaign.push` key are gone. `campaign.pull` and `campaign.git_remote`
   stay, for `git pull --ff-only`.
3. A campaign file without a uuid is a rejected file:
   `no uuid: run make all in the campaign repo`. It creates nothing. If the
   file's `source_file` already maps to a campaign, that campaign is frozen,
   not disabled, like any other rejected file.
4. `v6ctl campaign validate` still fails a PR that adds or changes a uuid. The
   message now says who does assign it: "leave uuid out; the maintainer assigns
   it with make all".

## Consequences

- A new campaign shows up on the first sync after the maintainer's `make all`
  commit, not the first sync after the merge. Until then it appears in the sync
  report as a rejected file, and `v6ctl campaign sync` exits non-zero.
- The campaign repo's Makefile is the only uuid writer, so its edge cases
  (quoted or empty values, CRLF, case, placement) are fixed there, and it has
  its own `make test`.
- Do not bring back bot write-back or a sync-side mint to shorten that window.
  The window is one maintainer step long, and a second writer is what forked
  identities before.
