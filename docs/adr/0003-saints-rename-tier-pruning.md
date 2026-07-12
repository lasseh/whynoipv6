# 0003 — Saints rename and almost/mail tier pruning

- **Status:** Accepted
- **Date:** 2026-07-12
- **Deciders:** project owner
- **Touches:** `db/migrations` (domain + stats columns, edited in place), `db/query`, `internal/domain` (`Classify`), `internal/crawler` (commit), `internal/api` (handlers, router, badge, CSV, export), `openapi.yaml` + generated clients, frontend `tiers.ts`/`api`, 00 §6, 03 §10, 05, 07 §2.3/§3.3/§4.4/§5, 10, 12

## Context

The public tier surface was five presets over the `/domains` leaderboard: `/heroes`, `/sinners`, `/gold`, `/almost`, `/mail`. Two problems:

1. **"Gold" is a naming outlier.** The site's identity is the sinners-vs-heroes dichotomy; "gold" belongs to no ladder vocabulary and reads as a grading system the project explicitly rejects (no grades, no scores).
2. **The almost and mail tiers earn no browse traffic.** Both are thin aliases (`class=partial`, `class=hero&mx=supported`) whose queries stay fully expressible through the general `/domains` filter grammar — and "almost" carried a documented wart: "almost there" and `partial` were one class with two names.

Considered and rejected: folding gold's criteria into the hero classification. That would change the classification algorithm itself — every hero with unconfirmed or unsupported resources drops out of hero, the public heroes count craters on NULL data (the resources dimension only just turned on, ADR 0002), and the changelog/stats would record a mass fake regression.

## Decision

1. **`gold` is renamed to `saint`, criteria unchanged:** `saint = (classification == hero AND confirmed resources ∈ {supported, not_applicable})`. Saints ⊂ heroes — the natural counterpart to sinners. It stays a boolean refinement, not a class.
2. **Full-depth rename, one vocabulary.** Pre-cutover (no external API consumers, DB rebuildable), so the rename goes through every layer: DB column `domain.saint`, stats counter `saints`, Go identifiers, JSON wire field `saint`, filter param `saint=true`, path `/saints`, operationId `listSaints`, frontend slug/label, spec, glossary. Grammar mirrors hero/heroes: singular for the value (`saint`), plural for the collection (`/saints`, "Saints", stats `saints`). Migrations are edited in place per repo precedent.
3. **`/almost` and `/mail` are removed end-to-end** — API paths, handlers, OpenAPI entries, frontend tabs and routes. Kept: the `partial` classification, the `?class=` / `?mx=` filter grammar on `/domains` (so `?class=partial` and `?class=hero&mx=supported` remain the canonical spellings), all stats counters, the `mail_missing` class flag, and the scope-or-stats guardrail for global mail counts. The tier surface is exactly the theme: `/sinners`, `/heroes`, `/saints`.
4. **The badge does not say "saint".** 07 §5.2's rule stands: badge copy is public status vocabulary, never ladder branding. The top badge variant renames `gold` → **`full`** (`IPv6: full`) — a neutral statement that everything measured is served over IPv6. The `#ffd700` accent color stays as pure visual.

## Consequences

**Positive**

- The tier vocabulary is a single coherent theme, and every layer spells the refinement the same way — no wire/DB/UI drift.
- Two dead public endpoints and the "one class, two names" alias wart are gone; the general filter grammar loses nothing.
- The badge keeps its neutral, README-safe copy.

**Negative / accepted**

- Breaking wire change (`gold` field/param/path and stats counter disappear) — acceptable pre-cutover with no external consumers; old `?filter=gold|almost|mail` frontend URLs fall back to the sinners tab, and an unknown `?gold=` API param is silently ignored.
- Spec 11 (implementation-plan) keeps its historical "gold" mentions as phase history; this ADR governs.
- ADR 0002 retains its historical "gold rule" wording (ADRs are immutable records).
