# 12 — Frontend Rebuild

_Status: Draft 2.0 — deep review applied 2026-07-11: web-verified ecosystem research (Vite 8/Rolldown, openapi-fetch maintenance-mode, AOS replacement, AI-crawler prerender question) + adversarial contract review against openapi.yaml and the old site (campaign-list adoption, search scope, trailing-slash redirects, Tracker sourcing, changelog phrasing). Originally authored from a full inventory of the live frontend (`whynoipv6-web`), the Tailwind-v4 attempt (`whynoipv6-web2`), and 07-api.md Round 3.0._

**Purpose:** The complete contract for rebuilding the Vue app in `frontend/`. Two locked goals, in priority order:

1. **Visual fidelity is absolute.** The rebuilt site must be pixel-faithful to the current whynoipv6.com — same dark theme, same palette, same fonts and type scale, same layout, same component look. §2 pins every token; nothing in this rebuild is a redesign.
2. **The code is modernized and bound to the new API.** Vue 3.5 `<script setup>` + strict TS everywhere, Tailwind v4 via `@tailwindcss/vite` with the old theme ported into `@theme` tokens, a fully-typed API layer generated from the committed `openapi/openapi.yaml` (07 §7), cursor pagination, RFC 9457 error handling. No axios, no untyped services, no dead config.

**Deliverables:** the `frontend/` subtree (§4 layout), consuming the drift-gated `openapi/schema.ts`; the `@theme` token port (§2.2); the page + component set (§8–§9); vitest coverage for the mapping layer (§11); Vite/nginx build + deploy config (§12).

**Companion files:** 07-api.md (every endpoint, envelope, cursor, and error shape consumed here), 00-overview.md §4 (monorepo layout — `frontend/` is a sibling of `backend/`, never compiled by the backend workflow), 08-migration-cutover.md (cutover is a DNS flip; this app ships to the same origin).

**Reference repos (read-only inputs, never imported):** `../whynoipv6-web` — the visual source of truth; `../whynoipv6-web2` — scaffold reference for tooling (strict tsconfig, ESLint 9 flat config, vitest setup, router-meta SEO guard); `../taillight/frontend` — the **structural** reference: its conventions (thin typed fetch layer, CSS-first `@theme` token design system, generic factories for repeated list plumbing, colocated `__tests__/`, type-check-as-blocking-gate) are adopted where they fit (§3.1); its product machinery (SSE streams, theming, feature flags, Pinia stores) is not. web2's cautionary lesson is normative: under Tailwind v4 its config files were dead (`@import 'tailwindcss'` with no `@theme`/`@config`), so the site silently fell back to stock Tailwind palettes. **The token port in §2.2 is what prevents that drift; it is a build gate, not a nice-to-have.**

---

## 1. Product scope & phasing

The rebuild lands in two phases. Phase 1 is a **parity rebuild**: the current site's pages, sections, and behaviors, re-implemented on the new API. Phase 2 is **additive surfaces** the new API unlocks (§10) — same visual language, new pages/blocks. Nothing in phase 2 blocks the cutover.

Phase 1 page set (the current site, §8): Home, Domain list, Domain detail, Search, Metrics (overview + network providers), Country list/detail, Campaign list/detail/domain, Changelog, FAQ, 404.

Out of scope entirely: accounts, auth, theming/light mode, i18n, SSR (§3 decision).

---

## 2. The visual contract (locked)

### 2.1 Identity

- **Dark-only.** No light theme, no toggle, no `dark:` variants. `<body class="font-inter antialiased bg-zinc-900 text-slate-200 tracking-tight">` — page background stock `zinc-900`, default text stock `slate-200`.
- **Brand accent: Tailwind default `fuchsia`.** Logo gradient `from-fuchsia-500 to-fuchsia-700`, buttons `bg-fuchsia-700 hover:bg-fuchsia-800`, active/emphasis links `text-fuchsia-600`, table header accents `text-fuchsia-600`, rank badge hover fuchsia.
- **Status colors (the site's semantic core):** `emerald` = supported/success, `pink` = unsupported/missing, `amber` = no_record, muted `zinc-600` = not-applicable / never-checked (§7.2 — the one new visual state, deliberately quiet).
- **Fonts:** `Inter` (400–900) body via `font-inter`; `Architects Daughter` accent. Loaded via Google Fonts `@import` exactly as today.
- **Layout skeleton (every page):** `flex flex-col min-h-screen overflow-hidden` → absolute overlay `Header` (`h-20 w-full z-30`) → `main.grow` → hex-dot `PageIllustration` (`relative max-w-6xl mx-auto h-0 pointer-events-none`) → sections in `max-w-6xl mx-auto px-4 sm:px-6` → `Footer`.
- **Cards/surfaces:** `bg-zinc-800/50` (or `bg-zinc-800`), `border border-zinc-700`, `rounded-sm`, `shadow-lg`. Table row hover `hover:bg-gray-800` (the **custom** gray-800, §2.2).
- **Rating badges** (country/campaign/detail): Good `bg-emerald-600/10 text-emerald-600 ring-emerald-600/40` (≥60%), Medium `bg-amber-600/10 text-amber-600 ring-amber-600/20` (≥40%), Bad `bg-rose-600/10 text-rose-600/80 ring-rose-600/20`, Unknown gray. Progress-bar gradients: teal-700→800 / amber-700→800 / pink-700→800.
- **Scroll animation: removed entirely** (owner decision 2026-07-11: "visual bloat"). The `aos` package (last release 2019, effectively unmaintained) is dropped and **not replaced** — content renders statically; all `data-aos` attributes are stripped during the port. This is the one sanctioned deviation from behavioral parity: layout, colors, and type are untouched, elements simply appear without the fade-up. *Rejected — keeping `aos`* (dead dependency) *and an IntersectionObserver re-implementation* (faithfully reproducible in ~15 lines, but the owner chose removal over preservation).

### 2.2 Tailwind v4 token port (`@theme`) — the drift gate

The old v3 `theme.extend` **overrode** stock palettes; classes like `border-gray-700` render the *custom* hex on the live site. All of it moves into `@theme` in `src/css/style.css`. Normative values:

**Custom `gray` (overrides default):**

| step | hex | step | hex |
|---|---|---|---|
| 100 | `#EBF1F5` | 500 | `#707D86` |
| 200 | `#D9E3EA` | 600 | `#55595F` |
| 300 | `#C5D2DC` | 700 | `#33363A` |
| 400 | `#9BA9B4` | 800 | `#25282C` |
| | | 900 | `#151719` |

**Custom `purple` (overrides default; illustration/form accent):**

| step | hex | step | hex |
|---|---|---|---|
| 100 | `#F4F4FF` | 500 | `#8D8DFF` |
| 200 | `#E2E1FF` | 600 | `#5D5DFF` |
| 300 | `#CBCCFF` | 700 | `#4B4ACF` |
| 400 | `#ABABFF` | 800 | `#38379C` |
| | | 900 | `#262668` |

**Font size scale (overrides defaults — visually load-bearing, e.g. `3xl` is 2rem not 1.875rem):** `xs .75rem · sm .875rem · base 1rem · lg 1.125rem · xl 1.25rem · 2xl 1.5rem · 3xl 2rem · 4xl 2.5rem · 5xl 3.25rem · 6xl 4rem`. The old config defines these as **bare sizes with no per-size line-height** (`"3xl": "2rem"`), so `text-3xl` inherited the surrounding leading. Tailwind v4's `@theme` *merges* with defaults, which pair every `--text-*` with a default `--text-*--line-height` — the port must **neutralize those pairings** (set `--text-*--line-height` to the inherited value) or every sized text node silently gains a line-height the old site never had. Also note the v4 cascade flip: an explicit `leading-*`/`tracking-*` utility now wins over the size-paired value (v3 was the opposite) — harmless once the pairings are neutralized, but part of the acceptance check below.

**Letter spacing:** `tighter -0.02em · tight -0.01em · normal 0 · wide 0.01em · wider 0.02em · widest 0.4em`. **Spacing extras:** `9/16: 56.25%`, `3/4: 75%`, `1/1: 100%`. **minWidth:** `10: 2.5rem`. **scale:** `98: .98`.

**Fonts:** `--font-inter: Inter, sans-serif`, `--font-architects-daughter: "Architects Daughter", sans-serif`.

**Component classes** (ported verbatim into `@layer components`): `.h1`–`.h4` (extrabold/bold display scale with `md:` bumps), `.btn`/`.btn-sm` (`rounded-sm`, `px-8 py-3` / `px-4 py-2`), `.form-input/-textarea/-select/-checkbox/-radio` (transparent bg, `border-gray-700`, `focus:border-gray-500`, `rounded-sm`), `.a-gradient` (`bg-gradient-to-r from-fuchsia-500 to-fuchsia-700` text-clip). Plus `theme.css` extras: hamburger animation, `.pulse` keyframes, AOS translate-distance overrides (10px).

**Plugins:** `@tailwindcss/forms` (as a v4 CSS `@plugin`).

**Acceptance:** a rendered page's computed styles for `gray-*`, `purple-*`, the type scale, and the component classes match the old site byte-for-hex. A quick manual check exists for each: `border-gray-700` must compute to `#33363A`, `h1` on Home must be `2.5rem` at mobile, and a bare `text-3xl` node must compute the **inherited** line-height (not a v4 size-paired one). *Rejected — keeping `tailwind.config.{js,ts}` files* (the exact web2 failure: two dead configs, zero effect, silent stock-palette fallback).

### 2.3 Assets carried over

`public/` favicon set, `WhyNoSticker.webp` OG image, robots.txt, sitemap.xml, security.txt; the hex-dot `PageIllustration` SVG (the variant actually exported today — `PageIllustrationHex`); the inline Check/Cross/Minus status SVGs; circle-flags (`https://hatscripts.github.io/circle-flags/flags/{code}.svg`) for country flags; the 404 illustration.

---

## 3. Stack & tooling decisions

| Concern | Decision | Rejected |
|---|---|---|
| Framework | Vue **3.5** + TS, `<script setup lang="ts">` in **every** SFC | Options API stragglers (web2 has 3) |
| Build | **Vite** (current major — **v8**, which ships the Rolldown Rust bundler + Oxc transforms as the built-in default: ~10–30× faster builds, zero opt-in) + `vue-tsc` type-check in `build`; keep the v8 default `baseline-widely-available` browser target, no legacy plugin | the transitional `rolldown-vite` package (a shim for old Vite-7 projects; Vite 8 already includes Rolldown) |
| CSS | **Tailwind v4 via `@tailwindcss/vite`**, tokens in `@theme` (§2.2) | v4-via-PostCSS (web2's setup — works, but the Vite plugin is the first-party path); v3 |
| Router | vue-router (current major — v5 at time of writing), `createWebHistory`, lazy route imports | pinning v4 |
| HTTP + types | the committed **openapi-typescript** output (actively maintained, keep) + a **small vendored typed-fetch wrapper** (`src/api/client.ts`, ~50 lines, typed against the generated `paths`) — taillight's pattern, now also the openapi-ts maintainers' own recommendation | **openapi-fetch, the package** (entered maintenance mode Dec 2025 — no future updates; its maintainers explicitly recommend vendoring a small fetch layer instead); axios (both old apps: per-call instances, zero generics); Orval/Hey-API/Kubb (heavier pre-1.0 codegen this API doesn't need; 07 §7) |
| State | **No store library.** URL query is the source of truth for every list/tab/pagination state; component-local `ref`/`computed` for the rest; shared logic in small composables — with the repeated list plumbing unified in one generic composable (§9.1), taillight's factory discipline applied at composable altitude | Pinia (nothing here is cross-page client state; a read-only site whose canonical state is the URL doesn't need a store — add it later if a real one appears) |
| Lint/format | ESLint (current major) flat config: `@eslint/js` + `eslint-plugin-vue` flat/recommended + typescript-eslint + Prettier-compat; Prettier with a normal `printWidth` (100). **`vue-tsc` type-check is the blocking gate; ESLint errors block, warnings are advisory** (`--quiet` in the gate, full run in `make frontend-check`). Optional: an **Oxlint pre-pass** in front of ESLint (create-vue's current default pairing — ~50–100× faster first pass; it complements, never replaces, `eslint-plugin-vue`, which still owns template rules) | the old repo's `printWidth: 1200` one-liner format; lint script with no config; **Biome as the Vue linter/formatter** (its `.vue` support is still labelled experimental) |
| tsconfig | strict-plus: `strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `noUnusedLocals/Parameters`, `noImplicitReturns`, `verbatimModuleSyntax`; **project-references split** (`tsconfig.app.json` browser/DOM + `@/*` alias, `tsconfig.node.json` vite config only); `@` → `./src` | one flat tsconfig mixing node + browser worlds |
| Tests | **Vitest** (current major — v4) + `@vue/test-utils` + jsdom (§11) — still the create-vue default | Vitest Browser Mode (stable in v4, but adds a real-browser dependency + slower CI for a mapping-layer test suite that doesn't need pixel fidelity) |
| SSR/SSG | **None** — SPA, as today. Google renders JS SPAs fine (the site ranks today); the 2026 gap is **AI crawlers (GPTBot, ClaudeBot, PerplexityBot) and Bing, which read static HTML only** — answered with the cheap static surface instead: site-level **JSON-LD** + a frontend **`llms.txt`** + sitemap (§9.6), which point non-JS crawlers at the API/datasets where every fact lives. Full prerendering (vite-ssg) is a **watch item** (§10), revisited only if AI-search visibility measurably matters | Nuxt (a rebuild-the-world move); committing to prerender 1M detail URLs now (real engineering for an unproven payoff) |
| Rendering-time deps | **None** — no UI kit, no chart lib, no animation lib (all bars/trackers stay hand-built divs, exactly the current look; scroll animation removed, §2.1) | headlessui/chart.js/aos |

Node tooling roots at `frontend/` (own `package.json`); the root `Makefile` gains `frontend-dev`, `frontend-build`, `frontend-test`, `frontend-lint` targets that `cd frontend && …`, keeping Make the universal interface.

### 3.1 Code conventions (adopted from taillight)

- **Component idioms:** type-only `defineProps<{…}>()` with `withDefaults` for optionals; type-only tuple `defineEmits<{ select: [value: string] }>()`; `defineModel<T>()` for two-way binding (no manual `modelValue`/`update:` plumbing); SFC block order `<script setup lang="ts">` → `<template>` → optional `<style scoped>` (scoped CSS for transitions/keyframes only — everything else is utility classes).
- **Naming:** PascalCase SFCs; pages suffixed by role (this repo keeps the existing page names); shared primitives stay small and flat (`StatusIcon`, `RatingBadge`, `LoadingSpinner`) — no formal `ui/` kit until duplication demands one.
- **Status→class maps are centralized**, never inline ternaries scattered across templates: the §7.2 table lives in one module (`utils/status.ts`) exporting `statusIcon(value)` / `statusTextClass(value)` / `statusBorderClass(value)`; the rating thresholds live in `utils/rating.ts`. One place to audit visual-contract compliance.
- **Typed route meta:** `env.d.ts` augments vue-router's `RouteMeta` with the `title`/`description` fields the §9.6 guard consumes.
- **A11y baseline:** `role`/`aria-*` on interactive elements (toggles, accordion, pagination), `prefers-reduced-motion` respected globally (the `.pulse` keyframe gets a media-query guard). Non-visual, so fidelity-safe.
- **Flat type-based folders (§4) are a deliberate fit, not a default:** at this app's size (~60–70 files) prefix naming carries structure; feature folders are the escape hatch if it ever grows past taillight scale (~130 files), not a day-1 abstraction.

---

## 4. Project layout

```
frontend/
├── index.html                 # static meta/OG (§9.6), umami script, body classes (§2.1)
├── package.json  vite.config.ts  tsconfig.json  eslint.config.js  .prettierrc.json
├── public/                    # §2.3 assets
└── src/
    ├── main.ts                # createApp, router
    ├── App.vue
    ├── router.ts              # §5 route table + meta guard (§9.6) + scrollBehavior
    ├── api/
    │   ├── client.ts          # the ONE openapi-fetch client (§6.1)
    │   ├── problem.ts         # RFC 9457 parsing → typed ApiProblem (§6.3)
    │   └── index.ts           # narrow per-resource call helpers (§6.2)
    ├── composables/
    │   ├── useCursorList.ts   # §9.1 — the one generic list-page engine
    │   ├── usePageMeta.ts     # route-meta titles (§9.6)
    │   └── useVisitorIp.ts    # GET /ip (§9.5)
    ├── components/            # DomainTable, ChangelogTable, Pagination, Tracker,
    │                          # StatusIcon, RatingStars, RatingBadge, ProgressBar,
    │                          # CountryFlag, Notification, LoadingSpinner
    ├── partials/              # Header, Footer, Searchbar, HomeSaaS, HomeSinners,
    │                          # HomeDomains, MetricCrawler, MetricASN,
    │                          # PageIllustration (hex), icons/
    ├── pages/                 # one SFC per §5 route
    ├── utils/
    │   ├── status.ts          # §7.2 status→icon/class maps (single source, §3.1)
    │   ├── rating.ts          # percent → badge/gradient classes (§2.1 thresholds)
    │   ├── date.ts            # Intl.DateTimeFormat en-GB "DD Month YYYY HH:MM"
    │   └── changelog.ts       # (field, old, new) → message + color (§7.4)
    └── css/
        ├── style.css          # @import 'tailwindcss' + @theme + @layer components (§2.2)
        └── theme.css          # hamburger/pulse extras
```

Tests are **colocated** in per-folder `__tests__/` directories (`components/__tests__/`, `composables/__tests__/`, `utils/__tests__/`, …) — never a top-level `test/` tree (taillight convention; keeps a unit next to what it locks).

Deleted relative to the old repos (never ported): the axios `services/` layer, `types/` namespaces (replaced by generated types), web2's speculative composables (`useCache`, `useCachedApi`, `useAsyncData`, `usePagination`, `useSearch`, `useToggle`, `useErrorHandler` — the browser HTTP cache plus §6 replaces all of them), the `aos` dependency + every `data-aos` attribute + the AOS distance overrides in `theme.css` (§2.1), Alpine remnants (`x-data`, `[x-cloak]`), `ensureTrailingSlash` (§5), unused `HomeMetric.vue`/wave `PageIllustration.vue`/`Dropdown.vue`, `range-slider.css`/`toggle-switch.css`, the stray `import { off } from "process"`.

---

## 5. Routing & URL contract

**Decision (resolved OPEN-F3): canonical paths go plural, old singular URLs 301-redirect.** The new canonical routes use plural collection nouns, matching the API's resource naming (07 §2.2); every existing singular URL keeps working via a **real HTTP 301 at nginx** (SEO-correct: link equity transfers, crawlers re-index) with a router-level `redirect:` backstop for anything that slips through to the SPA. Pagination state moves from `?offset=` to `?cursor=` (opaque token, §9.1) — old `?offset=` links degrade gracefully to page 1; other query params are preserved across the redirect.

| Path | Page | Query state | Data (07-api) |
|---|---|---|---|
| `/` | Home | — | §8.1 |
| `/domains` | DomainList | `filter=sinners\|heroes`, `cursor` | `/sinners`, `/heroes` |
| `/domains/:domain([^/]+)` | DomainDetail | — | `/domains/{host}`, `/domains/{host}/changelog`, `/domains/{host}/history` |
| `/domains/:domain([^/]+)/not-found` | DomainNotFound | — | — |
| `/search` | Search | `q`, `cursor` | `/domains?q=` |
| `/metrics` | Metrics | `t=overview\|asn`, `sort` | `/stats/overview`, `/asns` |
| `/countries` | CountryList | — | `/countries` |
| `/countries/:id` | CountryDetail | `filter`, `cursor` | `/countries/{code}`, `/countries/{code}/domains?class=` |
| `/campaigns` | CampaignList | — | `/campaigns` |
| `/campaigns/:uuid` | CampaignDetail | `cursor` | `/campaigns/{uuid}` (composite), `/campaigns/{uuid}/changelog` |
| `/campaigns/:uuid/:domain([^/]+)` | CampaignDomain | — | `/domains/{host}` + campaign changelog scope |
| `/campaigns/:uuid/:domain([^/]+)/not-found` | DomainNotFound | — | — |
| `/changelog` | Changelog | `filter=tranco\|campaign`, `cursor` | `/changelog` (+ `?scope=campaign`, §7.4) |
| `/faq` | FAQ | `page=1..4` | static |
| `/:catchAll(.*)` | PageNotFound | — | — |

Redirect map — nginx 301s, **anchored** (so `/domain` can never re-match `/domains` and loop) and **trailing-slash-stripping**. The old router *forced* trailing slashes onto both detail routes (`ensureTrailingSlash`), so the URLs search engines hold are `/domain/example.com/` — a naive rewrite would hand the SPA `example.com/`, which fails host canonicalization:

```nginx
rewrite ^/domain$            /domains  permanent;
rewrite ^/domain/(.+?)/?$    /domains/$1  permanent;
rewrite ^/country$           /countries  permanent;
rewrite ^/country/(.+?)/?$   /countries/$1  permanent;
rewrite ^/campaign$          /campaigns  permanent;
rewrite ^/campaign/(.+?)/?$  /campaigns/$1  permanent;
```

(`rewrite` preserves the query string by default.) The SPA router additionally strips any residual trailing slash in a global guard before matching. Route params use `:domain([^/]+)` — hostnames never contain `/`, and the non-greedy pattern keeps the `…/not-found` sibling routes unambiguous (the old greedy `(.*)` could swallow the suffix). The sitemap lists only plural URLs.

Route conventions (from web2, kept): per-route `meta: { title, description }` consumed by a global `beforeEach` guard (§9.6); `scrollBehavior` honoring saved position/hash/top; every page component lazy-imported. The old `ensureTrailingSlash` guard is **deleted** — it existed to appease the old backend; the new API canonicalizes hosts itself (07 §2.8), and `:domain(.*)` params pass through encode-safely.

---

## 6. API integration layer

### 6.1 One typed client

`openapi/schema.ts` (openapi-typescript output, already committed and CI-drift-gated per 07 §7) is the **only** wire-type source; the frontend never hand-writes a response interface. It is consumed in place via the `@openapi` path alias — one generated file, zero copies — with two integration requirements that are easy to get wrong: (a) `../openapi` must be added to `tsconfig.app.json` `include` (composite project-references reject sources outside the project with TS6059; a path alias alone is not enough) and imported with `import type` (a types-only file, fully elided under `verbatimModuleSyntax`); (b) Vite's dev server needs a `server.fs.allow` entry for the directory — that setting affects dev serving only, not `vue-tsc` or the build. CI runs `vue-tsc --build`, not just the dev server, so both halves are exercised.

`src/api/client.ts` is a **small vendored typed-fetch wrapper** (~50 lines; §3 — the openapi-fetch package is maintenance-mode, so we own this file): a `get(path, params?, signal?)` + `post(path, body)` pair typed against the generated `paths` type, applying base URL (`import.meta.env.VITE_API_URL`), JSON negotiation, and problem+json conversion (§6.3) in one place. Wire fields are `snake_case` end-to-end (07 §2.3) — **no** camelCase transform layer; templates bind `domain.class_flags` directly.

Environments: `.env.development` → `http://localhost:8080` (the API's dev bind, 07 §1.1); `.env.production` → `https://api.whynoipv6.com`. The client sends no auth, no custom headers; conditional-request/ETag revalidation is left entirely to the browser HTTP cache (the API's `Cache-Control`/`ETag` design, 07 §6.1, makes a client-side cache layer redundant — this deletes web2's `useCache` machinery).

Request discipline (taillight's fetch-layer conventions, now simply built into our own wrapper): every request carries `AbortSignal.timeout(15_000)` by default; list helpers accept an external `signal` so `useCursorList` (§9.1) can cancel a superseded page fetch instead of racing it. No retry layer and no client cache — the CDN and browser cache are the resilience story. *Rejected — runtime config injection (`window.__CONFIG__`)*: build-once-run-anywhere is the right call for a multi-environment Docker fleet (taillight), but this site ships as one static bundle to one origin; build-time `VITE_API_URL` is simpler and sufficient. Revisit only if containerized multi-env deploys appear.

### 6.2 Call helpers

`src/api/index.ts` exports narrow, typed helpers per resource (`getDomain(host)`, `listTier(tier, params)`, `getOverviewStats()`, …) — thin wrappers over the client so pages never build paths inline and tests can stub one seam. Envelope handling is uniform (07 §2.4): item collections are `{ items, page, meta }`, time series are `{ points, meta }`, single resources carry sibling `meta`. Helpers return the typed body; `error` results are converted via §6.3.

### 6.3 Errors — RFC 9457

Every non-2xx is `application/problem+json` (07 §2.5). `problem.ts` parses it into a typed **`ApiProblem extends Error`** class (taillight's `ApiError` shape) carrying `{ type, title, status, detail }`, keyed by the type-URI tail (`not-found`, `rate-limited`, …) so call sites discriminate with `instanceof` + a string enum, never by re-parsing bodies. Page policy:

- `not-found` on a detail route → redirect to the sibling `…/not-found` page (domain/campaign-domain) or render the inline empty state (country/campaign).
- Zero-result lists are `200` with empty `items` (07 §2.6) — rendered as the existing empty states ("No domains found", "No changes yet"), **never** treated as errors.
- Anything else → the page-level error state (existing card style) with `title`; no toast library.

### 6.4 What the frontend consumes (phase 1 surface)

`/heroes`, `/sinners`, `/shame`, `/domains?q=`, `/domains/{host}`, `/domains/{host}/changelog`, `/domains/{host}/history`, `/countries`, `/countries/{code}`, `/countries/{code}/domains`, `/campaigns`, `/campaigns/{uuid}` (composite), `/campaigns/{uuid}/changelog`, `/campaigns/{uuid}/domains/{host}/changelog`, `/changelog` (+ `?scope=campaign`), `/stats/overview`, `/asns`, `/ip`. Phase 2 adds §10's list.

---

## 7. Data-model mapping (old wire → new wire → pixels)

The visual output of each component is unchanged; only its input changes. This section is the normative mapping.

### 7.1 Domain status dimensions

Old: 4 fields (`base_domain`, `www_domain`, `nameserver`, `mx_record`), 3-value strings. New: 6 status objects (07 §4.1), 4-value enum + `null`. Table columns are **Rank / Domain / Apex / WWW / E-Mail / Nameserver / IPv6 Only** — the first four status columns map to `status.base/www/mx/ns.value`; **IPv6 Only** renders the derived `ipv6_only` field (07 §4.2 — the conn+resources fold, ADR), through the same `StatusIcon` vocabulary (`null` = "Not yet checked" minus, strict until both dimensions confirm). The raw `conn`/`resources` objects stay off the table. The detail status card renders the four §7.1 rows plus an **IPv6 Only** accordion row showing the `ipv6_only` fold, which expands to the two source-dimension Trackers (`conn` labeled "Reachability", `resources` labeled "Page resources").

### 7.2 Status → icon/color (component `StatusIcon`)

| `value` | Icon | Color | Old equivalent |
|---|---|---|---|
| `supported` | Check | `text-emerald-500` | `supported` |
| `unsupported` | Cross | `text-pink-500` | `unsupported` |
| `no_record` | Minus | `text-amber-500` | `no_record` |
| `not_applicable` | Minus | `text-zinc-600`, tooltip "Not applicable" | (was folded into `no_record`) |
| `null` (never confirmed) | Minus | `text-zinc-600`, tooltip "Not yet checked" | (n/a) |

The two muted states are the **only** new pixels in phase 1 — deliberately quieter than the three legacy colors so the page reads identically at a glance. The detail accordion uses the old site's stronger 600 shades for its border-l-4 and label text (`border-emerald-600`/`text-emerald-600` / `border-pink-600`/`text-pink-600`, amber and zinc unchanged — helpers `statusCardBorderClass`/`statusCardTextClass`), status text Success / Missing / No Record / Not applicable / Not yet checked.

### 7.3 Per-view field mapping

| View element | Old source | New source |
|---|---|---|
| Rank badge | `domain.rank` (0 = none) | `rank` (`null` = none — hide badge on `null`, never render 0) |
| Provider line on detail | `asn` display string | `asn.name` (+ `AS{asn.number}`) |
| Country on detail | `country` string | `country.name` + link `/countries/{country.code}` |
| 4-star rating (detail) | count of `supported` among 4 fields | over `status.{base,www,ns,mx}.value`, three star states (resolved OPEN-F1): `supported` → filled emerald star; `not_applicable` → **muted zinc-600 star** (tooltip "Not applicable" — neither earned nor missing; a no-MX domain is never penalized); everything else (`unsupported`/`no_record`/`null`) → empty gray star |
| "Last checked" | `ts_check` | `last_checked_at` |
| Sinners list | `GET /domain?offset=` | `GET /sinners?cursor=` |
| Heroes list | `GET /domain/heroes?offset=` | `GET /heroes?cursor=` |
| Home top-shame | `GET /domain/topsinner` | `GET /shame` (curated; render `host` + `reason`) |
| Search | `GET /domain/search/{q}` + `GET /campaign/search/{q}` (two sections, `{data:{data:[…]}}` quirk) | `GET /domains?q=` — one result list, cursor-paged (§9.1). The `?q=` predicate **spans rank-NULL rows** as of the 2026-07-11 07 §3.3 amendment (without it, campaign-only hosts would be invisible to search — the default `/domains` scope excludes rank-NULL), so the old second "Campaign Domains" section folds into the single list; rank badge hidden on `null` |
| Country card/detail numbers | `sites`, `v6sites`, `percent` (÷10 hack upstream) | `sites`, `v6_sites`, `percent` (served correct — **no client ÷10**) |
| Country scoped lists | `/country/{id}/sinners\|heroes` | `/countries/{code}/domains?class=sinner\|hero` |
| Campaign card % | client-computed from `count`/`v6_ready` | `adoption.v6_ready_percent` (server-computed) — carried on the **campaign list row** as of the 2026-07-11 07 §4.7 amendment (was detail-only, which would have forced an N+1 fan-out for the card grid); `null` before the campaign's first stats tick → render the empty bar |
| Campaign member count | `count` | `meta.count` (exact) |
| Campaign members table | campaign detail rows | composite `domains.items` (§4.2 rows, `rank: null`); "fully ready" row highlight `bg-emerald-900/50` uses the **server's `v6_ready` predicate** (07 §4.7: `base` supported ∧ `ns` supported ∧ `www` ∈ {supported, not_applicable} — MX deliberately excluded) so the highlighted-row count always agrees with the `adoption.v6_ready_percent` shown above the table |
| Metrics overview grid | `/metric/overview` `[0].data.{domains, base_domain, www_domain, nameserver, mx_record, heroes, top_heroes, top_nameserver}` | latest point of `GET /stats/overview` → `{domains, base_supported, www_supported, ns_supported, mx_supported, heroes, top_heroes, top_nameserver}` |
| ASN bars | `/metric/asn?order=ipv4\|ipv6` → `count_v4/count_v6` | `GET /asns?sort=count_total\|count_v6` → `count_v6` (`bg-emerald-600` segment) vs `count_v4` (**`bg-violet-950`** segment — the exact old shades); ASN search via `GET /asns?q=` |
| Visitor banner | `GET /ip`, client sniffs `:` | `GET /ip` → show "No IPv6?!" iff `family !== "ipv6"` (no string sniffing) |
| Tracker (uptime timeline) | `/domain/{d}/log` per-scan rows | `GET /domains/{host}/history` **daily** `points` (an accepted granularity change: the old tracker was one block per scan; the new one is one block per day — same visual language, honest data). Each accordion row's tracker colors day-blocks by **that dimension's** value via §7.2; 30/60/90-day responsive windows kept. The 2026-07-11 07 §4.9 amendment seeds the reconstruction from the confirmed `(value, *_since)` baseline — without it, every never-flipped domain (the overwhelming majority) would render a blank tracker forever, since bootstrap confirmations write no changelog row. Days before a dimension's `since`, and genuinely empty windows, render the neutral `bg-gray-800` blocks |

**Null discipline:** every nullable field this table dereferences — `adoption`, `shame.reason`, `last_checked_at`, `dns_provider`, `asn`, `country`, individual stats-point fields — gets an explicit guard rendering the existing empty treatment (em-dash / hidden element), never a crash. The generated types make these `| null`, so `vue-tsc` enforces the guards; do not cast them away.

### 7.4 Changelog rendering (`utils/changelog.ts`)

Old rows carried a server-rendered `message` + `domain_url`. New rows are structured `{ts, host, field, old_value, new_value}` (07 §4.8); the frontend derives the message. **The template is keyed on `(field, old_value, new_value)` — `old_value` matters** (a host going `not_applicable → unsupported` never *had* IPv6 to lose; phrasing it "lost IPv6" would be false). Normative table for `base`/`www`/`ns`/`mx`:

| `new_value` | `old_value` | Message | Color |
|---|---|---|---|
| `supported` | any | `"{host} now supports IPv6 on {field_label}"` | emerald |
| `unsupported` | `not_applicable` | `"{host} started using {field_label} — without IPv6"` | pink |
| `unsupported` | other | `"{host} lost IPv6 on {field_label}"` | pink |
| `no_record` | `not_applicable` | `"{host} started publishing {field_label} — without IPv6 records"` | amber |
| `no_record` | other | `"{host} no longer publishes records for {field_label}"` | amber |
| `not_applicable` | any | `"{host} no longer uses {field_label}"` | muted zinc |

**`conn` and `resources` are derived dimensions with bespoke wording** — the generic "{host} verb {field_label}" template misdescribes them ("no longer uses connectivity" says nothing). Normative table (the `not_applicable` rows are defensive-only: the commit machine never writes them — 03 §7):

| Field | `new_value` | `old_value` | Message | Color |
|---|---|---|---|---|
| `conn` | `supported` | any | `"{host} is now reachable over IPv6"` | emerald |
| `conn` | `unsupported` | `not_applicable` | `"{host} published IPv6 addresses — but connections fail"` | pink |
| `conn` | `unsupported` | other | `"{host} is no longer reachable over IPv6"` | pink |
| `conn` | `not_applicable` | any | `"{host} has no IPv6 addresses left to test"` | muted zinc |
| `resources` | `supported` | any | `"{host} now loads all page resources over IPv6"` | emerald |
| `resources` | `unsupported` | any | `"{host} loads some page resources without IPv6"` | pink |
| `resources` | `not_applicable` | any | `"{host} no longer has its page resources checked"` | muted zinc |

This is the same `(field, old, new)` key the server's feed serializer renders from (07 §5.4); the §11 goldens pin both wordings so they don't drift apart. The changelog page renders **all six fields** — `conn`/`resources` transitions appear here even though the phase-1 detail accordion shows only four dimensions (deliberate asymmetry: the changelog is the trust surface, not a detail view).

`field_label`: base → "the base domain", www → "www", ns → "nameservers", mx → "e-mail", conn → "connectivity", resources → "page resources". Timestamp column stays violet-500 mono, host links to the domain detail page. The Changelog page's Tranco/Campaigns toggle is preserved (**resolved OPEN-F2**): `?filter=tranco` → `GET /changelog` (paginated), `?filter=campaign` → `GET /changelog?scope=campaign` (07 §4.8 — a fixed recent window of 50; its envelope carries `null` cursors, so the `Pagination` control self-disables with zero special-casing). The 30 s auto-refresh stays (aligned with the API's 300 s public cache: refreshes hit the CDN, which is fine — freshness comes from `max(changelog.ts)`-seeded ETags, 07 §6.1).

---

## 8. Pages (phase 1)

Every page keeps its current copy and section order (scroll-animation attributes are stripped, §2.1). Only data plumbing changes.

1. **Home** — HomeSaaS hero ("Shame as a Service" copy verbatim) → Searchbar (GET-form to `/search?q=`) → HomeSinners (curated `/shame` picks + rotating testimonial) → HomeDomains (top-of-`/sinners` preview table) → Notification banner (§9.5).
2. **DomainList** — title "Unmasking the Top 1M Websites", Sinners/Heroes toggle buttons (`?filter=`), `DomainTable`, `Pagination` (§9.1). Page size 50 (`?limit=50`, the API default).
3. **DomainDetail** — breadcrumb, host + Provider/Rank header (rank badge `bg-fuchsia-900`), Domain Status card: `RatingStars` (4 stars, §7.3) + the §7.1 accordion (four dimension rows + the IPv6 Only fold row) each expanding a `Tracker` (§7.3) + "Last checked", then `ChangelogTable` from `/domains/{host}/changelog`. Unknown host → §6.3 not-found redirect.
4. **CampaignDomain** — DomainDetail variant: campaign breadcrumb, changelog scope `/campaigns/{uuid}/domains/{host}/changelog`.
5. **Search** — input + single "Domains" result table from `/domains?q=`, cursor-paged via §9.1 (the old page showed one unpaged list; results past 50 are now reachable through the same Previous/Next control — §7.3 covers the campaign-domain fold-in).
6. **Metrics** — tabs `?t=overview|asn`. Overview: MetricCrawler stat grid (§7.3 mapping). ASN: MetricASN bars + sort toggle + search (§7.3).
7. **CountryList** — client-side name filter over `/countries`; 2/xl:8 card grid: flag, name, `RatingBadge`, percent `ProgressBar`.
8. **CountryDetail** — country header + counts + big percent bar, Sinners/Heroes toggle over `/countries/{code}/domains?class=`, `DomainTable` + `Pagination`.
9. **CampaignList** — intro copy, client-side filter, "Create Campaign" GitHub link, card grid with `adoption.v6_ready_percent` bars fed directly from the list rows (07 §4.7 amendment — no per-campaign detail fan-out).
10. **CampaignDetail** — name/description/`RatingBadge`/counts, members table (`CampaignDomainTable` look: no Rank column, ready-row highlight), `Pagination` over the composite's `domains.page` cursors, `ChangelogTable` from the campaign scope.
11. **Changelog** — §7.4; toggle + `Pagination` + 30 s refresh.
12. **FAQ** — 4 sub-pages via `?page=`, sidebar nav, active `text-fuchsia-600`. Content ported verbatim **except** the "Rules and API" page, which is rewritten for the new API: base URL `api.whynoipv6.com` (no `/v1`), `/docs` + `/openapi.json` + `/llms.txt` links, datasets (§5.3), badge usage string, feeds. (Content task, tracked in `.scratch/`.)
13. **PageNotFound / DomainNotFound** — unchanged art + copy.

---

## 9. Cross-cutting behaviors

### 9.1 The generic list engine (`useCursorList<T>` + `Pagination`)

Five pages repeat the same plumbing (domain list, country detail, campaign detail, changelog, search): fetch a cursor-paged collection, sync cursor + filter state with the URL, expose loading/empty/error, cancel superseded fetches. Taillight's core lesson — **one deep, tested generic instead of N drifting copies** (its `createEventStore`/`createFilterStore` factories) — applies here at composable altitude:

```ts
function useCursorList<T>(opts: {
  fetch: (params: { cursor?: string; [k: string]: string | undefined },
          signal: AbortSignal) => Promise<ItemCollection<T>>,
  filterKeys?: string[],          // e.g. ['filter'] — synced to URL query alongside cursor
}): { items, page, meta, loading, error, next(), prev(), setFilter(k, v) }
```

Behavior contract:

- **URL is the source of truth.** `?cursor=` and each `filterKeys` entry two-way-sync with `route.query` (watch query→state and state→`router.replace`, with a sync guard against the feedback loop — taillight's filter-store pattern). Back/forward and reload just work; changing a filter clears the cursor.
- **Pagination maps 1:1 onto the API page block** (07 §2.4): Next → `page.next_cursor`, enabled iff `has_more`; Previous → `page.prev_cursor`, enabled iff non-null. The visible control is unchanged (Previous/Next buttons).
- **Superseded fetches are aborted** via `AbortController` (§6.1), never raced.
- A `400 invalid-parameter` on a stale/foreign cursor (07 §3.2) resets to page 1 silently.
- `meta.count_estimate` is exposed but unused in phase 1 (no count is displayed today).

This is the one genuinely deep frontend module; it gets the densest test coverage (§11) and every list page becomes ~10 lines of config over it.

### 9.2 Loading / empty / error states

Loading: the existing animated fuchsia SVG spinner (`LoadingSpinner`). Empty: the existing per-table copy. Error: §6.3. No skeletons, no new spinners.

### 9.3 Dates

`utils/date.ts` — `Intl.DateTimeFormat` en-GB, "DD Month YYYY HH:MM", as today. All API timestamps are RFC 3339 UTC; `day` fields are `YYYY-MM-DD`.

### 9.4 Header / Footer

Verbatim port: logo gradient text, nav (Domains, Campaigns, Countries, Metrics, Changelog, FAQ), active-route underline, mobile hamburger animation; footer GitHub/Twitter icons + Blix hosting credit. **One required addition to the footer:** an IPinfo attribution link (`IP data by IPinfo` → `https://ipinfo.io`) — the country/ASN GeoIP data is IPinfo Lite, whose CC BY-SA 4.0 license mandates crediting the source (a link suffices; 09-ops §11, ADR 0001). This is the single non-verbatim footer element.

### 9.5 Visitor IPv6 banner (`Notification` + `useVisitorIp`)

Bottom-right toast; `GET /ip`; warn iff `family !== "ipv6"`; auto-hide 15 s / on scroll; fade transition. Fails silent (network error → no banner).

### 9.6 SEO / meta / analytics / machine-readable surface

Static OG/Twitter/canonical block in `index.html` (WhyNoSticker.webp, @WhyNoIPv6, `notranslate`) as today; per-route `document.title` + meta-description via route `meta` + the global guard (web2's pattern — replaces the old imperative onMounted titles); umami `<script>` kept verbatim; `public/` robots.txt + sitemap.xml + security.txt carried over (sitemap regenerated for the §5 route set).

**The static crawler surface (Decision 2026-07-11).** AI crawlers (GPTBot, ClaudeBot, PerplexityBot) do not execute JavaScript, and Bing renders it weakly — so anything that must be readable by them has to exist as static bytes. Instead of prerendering the SPA (§10 watch item), phase 1 ships the two cheap static artifacts:

- **JSON-LD in `index.html`** — a single `<script type="application/ld+json">` in the `<head>`; browsers never execute it, crawlers parse it, and because it lives in the static shell it is visible to every non-JS crawler (JS-injected JSON-LD would not be). One `@graph` with stable `@id` fragments (reference: hawksley.dev "JSON-LD explained for personal websites"): a `WebSite` node (`https://whynoipv6.com/#website`, name, description, `potentialAction: SearchAction` targeting `/search?q={search_term_string}`), an `Organization` node (`#org`, `sameAs` → GitHub/Twitter), and a `Dataset` node (`#dataset`) describing the bulk exports — name, `license: CC-BY-NC-4.0`, `distribution` → `https://api.whynoipv6.com/datasets`, the attribution string from 07 §5.3. The `Dataset` node is the high-leverage one: it makes the data itself discoverable (Google Dataset Search, LLM citation) without any per-page rendering. Per-route JSON-LD (e.g. `Dataset`/`WebPage` per domain) is deferred to the prerender watch item — injected client-side it would be invisible to exactly the crawlers it targets.
- **`llms.txt` at the site root** (`public/llms.txt`) — a markdown index for LLM agents: what the site is, the classification vocabulary in one paragraph, and direct links to `https://api.whynoipv6.com/llms.txt`, `/openapi.json`, `/docs`, `/datasets`, and the badge/check endpoints. This is the actual answer path for AI systems: an agent that can't render the SPA reads `llms.txt`, discovers the JSON API, and fetches facts directly — no HTML needed. (The API side already serves its own `llms.txt`, 07 §7; the site's file points at it.)

---

## 10. Phase 2 — new API surfaces (additive, post-cutover)

Same visual language (§2), each independently shippable; none block the DNS flip. Priority order:

**Watch item — prerendering (vite-ssg).** If AI-search/Bing visibility proves to matter beyond what the §9.6 static surface (JSON-LD + `llms.txt` + datasets + API) delivers, the escape hatch is vite-ssg: same Vue/Vite code, hydrates client-side (visual parity untouched), emits static HTML — realistically for the ~14 template/index pages plus a popular-domains slice, with the 1M-URL long tail needing on-demand prerendering at the edge (that long-tail plumbing is the real work, and why this stays a watch item rather than a commitment). Decision trigger: evidence (referrer logs, AI-citation checks) that non-JS crawlers are a meaningful discovery channel for the *pages* rather than the *data*. Nuxt stays rejected regardless.

1. **Live check page** (`/check`) — the flagship new feature: input → `POST /check` → 202 → poll `GET /check/{id}` every 2 s (07 §5.1.2) until `done|failed`; render `result.checks` with §7.2 icons (the raw-observation vocabulary incl. `error`; live results are labelled "live observation", never confirmed state) plus the `confirmed` block when present; handle `429 rate-limited` with `retry_after` countdown; dedupe responses (`cached: true`) get a "checked recently" note.
2. **Classification & gold surfacing** — classification badge (`RatingBadge` visuals) + `class_flags` chips on DomainDetail; `/gold` and `/almost` as new `?filter=` options on DomainList; gold star affordance on hero rows.
3. **Six-dimension detail** — add `conn` (and, once `crawler.resources.enabled` flips, `resources`) rows to the detail accordion; `informational` block (dnssec/ptr/smtp/parity + latency pair) as a quiet secondary card.
4. **Badge promo** — an "Embed this badge" snippet on DomainDetail (`![IPv6](https://api.whynoipv6.com/badge/{host}.svg)` + shields endpoint variant).
5. **Adoption graphs** — `/stats/overview` + country/campaign/asn `/stats` time series as CSS/SVG line-or-bar blocks on Metrics/detail pages (still no chart library unless a real need appears).
6. **Providers league table** — `/providers` page mirroring the ASN view; `?provider=` filter links.
7. **Mail track** (`/mail` preset), **resource dependents** ("this v4-only host breaks N sites", once resources ship), **mandates view** (`/campaigns?tag=mandate`), **feeds/datasets/CSV links** in footer/FAQ (`.atom`, `.feed.json`, `?format=csv`, `/datasets`).

---

## 11. Testing

Vitest + `@vue/test-utils`; API stubbed at the §6.2 helper seam (no live network, no MSW — the helpers are the injection seam, taillight-style). **Test environment is `node` by default for speed; DOM tests opt in per file with `// @vitest-environment jsdom`.** Tests live colocated per §4. Priority coverage — the mapping layer, where regressions are silent:

1. `utils/status.ts` + `StatusIcon`: all five §7.2 states → icon/class/tooltip.
2. `utils/changelog.ts`: message + color table (§7.4) — golden table-driven cases per `(field, old_value, new_value)`, including the `not_applicable`-origin phrasings.
3. `useCursorList`: next/prev enablement from `page`, URL two-way sync + loop guard, filter-change cursor reset, stale-cursor reset, superseded-fetch abort.
4. `utils/rating.ts`: threshold boundaries (0/40/60, zero-total Unknown).
5. `RatingStars`: the three star states across enum combinations (§7.3 — filled/muted/empty, incl. the all-`not_applicable` and `null` edges).
6. `Tracker`: day-block coloring from history points; empty-history rendering.
7. One mount smoke test per page with stubbed helpers (renders, no console errors).
8. **Cross-language contract goldens** (taillight's strongest test idea, cheap in this monorepo): a `__tests__` file imports the backend's committed golden JSON fixtures by relative path and (a) assigns them to the generated `schema.ts` wire types — drift fails `vue-tsc`; (b) runtime-asserts no undeclared keys. This locks the *actual handler output* to the frontend types, complementing the spec-level drift gate (07 §7) which only locks both sides to `openapi.yaml`.

Type safety is itself a gate: `vue-tsc` runs in `build`, and the generated `schema.ts` types make backend contract drift a compile error — that replaces the old world's absent API tests.

---

## 12. Build, environments, deploy

- **Envs:** `VITE_API_URL` — development `http://localhost:8080`, production `https://api.whynoipv6.com`. No other runtime config.
- **Build:** `vite build` (with `vue-tsc`) → static `dist/`. Sensible default chunking; revisit web2's manualChunks only if bundle analysis shows a need.
- **Serve:** static nginx vhost for `whynoipv6.com` (deploy/nginx, 09-ops.md): SPA fallback `try_files $uri $uri/ /index.html`, gzip/brotli, long-cache hashed assets. Cutover remains a DNS flip (08-migration-cutover.md).
- **Make targets (root):** `frontend-dev`, `frontend-build`, `frontend-test`, `frontend-lint`, `frontend-check`; `make generate` already regenerates `openapi/schema.ts`, which CI drift-gates (07 §7). `frontend-lint` is the **blocking gate** = `vue-tsc` type-check + `eslint --quiet` (errors only) + `prettier --check`; `frontend-check` is the advisory full-warning ESLint run (taillight's gate split, §3).

---

## 13. Open items

| # | Question | Status |
|---|---|---|
| OPEN-F1 | `not_applicable` in the 4-star detail rating. Principle: **a domain without an MX record must not be shamed for it.** | **Resolved 2026-07-11: muted third star state** — `not_applicable` renders a muted zinc star (neither filled nor empty, tooltip "Not applicable"), fixed 4-star layout preserved, star language matches the accordion's muted row (§7.3). *Rejected — filled star* (implies IPv6 mail exists) *and dropping to 3 stars* (variable layout, weakest visual parity) |
| OPEN-F2 | Changelog page "Campaigns" toggle. | **Resolved 2026-07-11: keep the page** (`/changelog?filter=campaign`), backed by the additive `GET /changelog?scope=campaign` (07 §4.8); recent-window cap, pagination self-disables (§7.4) |
| OPEN-F3 | Singular vs plural public paths. | **Resolved 2026-07-11: plural canonical, 301 from singular** (§5) |
| OPEN-F4 | FAQ "Rules and API" rewrite copy. | **Resolved 2026-07-11: yes** — draft in phase 1, human wording pass before cutover |
| OPEN-F5 | Phase 2 ordering/scope (§10). | **Resolved 2026-07-11: as listed** — live check stays phase 2 |
| OPEN-F6 | Campaign card grid needs adoption data on list rows (review blocker B1 — `CampaignListItem` carried no `adoption`). | **Resolved 2026-07-11: API amended** — 07 §4.7 adds `adoption` to campaign list rows (nullable pre-first-stats) |
| OPEN-F7 | Search couldn't see campaign-only (rank-NULL) hosts (review blocker B2 — the `/domains` default scope excludes them, contradicting §7.3's fold-in premise). | **Resolved 2026-07-11: API amended** — 07 §3.3 `?q=` spans rank-NULL, non-disabled rows |
| OPEN-F8 | Trackers would render blank forever for never-flipped domains (review S3 — bootstrap confirmations write no changelog row, so the pure replay yields `points: []`). | **Resolved 2026-07-11: API amended** — 07 §4.9 seeds the reconstruction from the confirmed `(value, *_since)` baseline |
| OPEN-F9 | Scroll animation: keep AOS behavior via IntersectionObserver, or remove? | **Resolved 2026-07-11: removed entirely** (owner: "visual bloat") — §2.1 |
| OPEN-F10 | AI/Bing crawlability: prerender (vite-ssg) vs static machine-readable surface? | **Resolved 2026-07-11: static surface now** (JSON-LD `@graph` + site `llms.txt`, §9.6); prerender demoted to a §10 watch item with an explicit decision trigger |
