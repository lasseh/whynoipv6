# WhyNoIPv6 — Frontend Redesign Brief (design-exploration prompt)

**How to use this file:** paste it into a fresh Claude session (or the design/prototype
tooling) and ask for the deliverable in the final section. It is self-contained — Claude
does not need the codebase to produce the concepts. The goal is to **decide whether to
commit to a redesign**, so the output is a set of concrete, comparable directions, not a
finished site.

---

## 1. What you are designing

**WhyNoIPv6** (whynoipv6.com) is a public, non-commercial IPv6-advocacy site — *"Shame as
a Service."* It crawls the world's most-visited websites (the Tranco top 1,000,000) plus
community-submitted campaign lists every day, measures whether each one supports IPv6, and
publishes the results as public **Heroes vs. Sinners** leaderboards by domain, country, and
network operator (ASN). The site has run since ~2016 and has real standing in the network-
operator community; this is a refresh of a living site, not a launch.

It is fundamentally a **data site wearing an advocacy site's clothes**: ranked tables,
per-domain diagnostic pages, country and provider standings, a live changelog of who
flipped IPv6 on or off, and downloadable datasets. The marketing-landing-page framing is
secondary to the data.

**Voice — keep this, it is the brand.** Snarky, tongue-in-cheek, gleefully judgmental, but
grounded in a genuine technical mission. The current site calls itself *"Shame as a
Service,"* runs a *"Wall of Shame,"* splits the world into *"Heroes"* and *"Sinners,"* and
nudges *"red-faced domains ... one at a time."* Verbatim samples to preserve the register:

- *"Introducing Shame as a Service!"*
- *"Wall of Shame"* / *"Top IPv6 Sinners"* / *"Shame on them!"*
- *"the forward-thinking Heroes who've embraced IPv6 ... and the Sinners, who despite their influence, hold us back."*
- Visitor toast for non-IPv6 users: *"No IPv6?! Your internet connection does not seem to support current internet standards."*
- 404: *"IPv4 and this page: both elusive."*

The humor is the differentiator. A redesign that makes it look "professional" by sanding
off the snark would be a failure.

---

## 2. Your task

Produce **2–3 distinct visual directions** for a refreshed WhyNoIPv6, so the maintainer can
compare them side by side and decide whether to proceed. Each direction must cover the same
two screens so they are comparable:

1. **The homepage** — hero/intro + the "Wall of Shame" ranked table + the top-sinners
   highlight + the visitor-IP notification.
2. **One data-dense screen** — either the full domain leaderboard (`/domain`, with a
   Heroes/Sinners toggle) or a single domain's diagnostic page (per-record IPv6 status +
   the 90-day history timeline). Pick whichever better shows the direction's take on
   dense data.

The directions should be **genuinely different takes on the same brand** (e.g. one leaning
editorial/newspaper, one leaning scoreboard/terminal, one leaning modern-but-restrained),
not three shades of the same layout. For each, include a one-paragraph rationale: what the
concept *is*, and why it fits "Shame as a Service" without looking generic.

---

## 3. Keep this (the identity the maintainer wants preserved)

**Dark theme.** The site is and stays dark. Near-black page background.

**Color palette — keep these roles and hues** (current exact values; you may refine shades
but stay in this family, don't re-hue the brand):

| Role | Current hex |
|---|---|
| Page background (near-black) | `#18181B` (zinc-900) |
| Raised surface / card | `#25282C` / `#27272A` |
| Body text | `#E2E8F0` (slate-200) |
| Muted text | `#9BA9B4` |
| Borders / dividers | `#334155` / `#3F3F46` |
| **Brand accent (magenta/fuchsia)** | `#C026D3` → `#A21CAF` (fuchsia-600/700) |

**The IPv6 status color language — this is sacred, users read the site by it:**

| Meaning | Color | Hex |
|---|---|---|
| **supported** — has IPv6 (Hero-side) | emerald/green | `#10B981` |
| **unsupported** — missing IPv6 (Sinner-side) | pink/red | `#EC4899` |
| **no_record** — no DNS record at all | amber/yellow | `#F59E0B` |

Green = good, pink = shame, amber = nothing there. Any new design must make these three
states instantly, unambiguously legible in tables, on the domain page, and on the history
timeline. This traffic-light semantics is the core UX.

**Content structure & the Heroes/Sinners framing.** The leaderboards, per-domain
diagnostics, country/provider standings, live changelog, and the hero/sinner vocabulary all
stay. The redesign is visual, not an information-architecture teardown.

---

## 4. Shed this (the current site is a recognizable off-the-shelf template)

The current build sits on the **Cruip "Open" Tailwind landing template**, and it still shows
the template's fingerprints. These are the parts that make it read as "a template," and the
refresh should replace them with something authored:

1. **The swoosh / flowing-lines gradient blob** absolutely positioned top-right on every
   page (Cruip's single most recognizable artifact). Kill it or replace it with a motif
   that actually belongs to *this* site (see §5 for ideas rooted in IPv6 itself).
2. The **narrow `max-w-6xl` centered column with a big centered hero** and generic top
   padding rhythm — the default landing-page skeleton.
3. The **"three feature cards with an outline icon + gradient-clipped title"** block on the
   homepage (Public Accountability / Community Engagement / User-Led Shaming). Generic SaaS
   furniture.
4. The **gradient-clipped wordmark** logo treatment and stock hamburger nav.
5. The **404 page** still ships the template's original purple (`#5D5DFF`) scribble —
   evidence of how templated it is.
6. Leftover **Inter-everywhere** typography with no display face doing any character work
   (the config even loads "Architects Daughter" and never uses it).

---

## 5. The hard rule: **do not make it look like an "AI-generated website"**

This is the maintainer's primary concern. The redesign must not look like the instantly-
recognizable, seen-it-a-thousand-times aesthetic that generic AI/startup site-builders
produce in 2025–2026. **Actively avoid these tells:**

- **The purple/violet-to-blue gradient hero** with a glowing aurora/blob behind giant
  centered bold text. (Ironically the current site is adjacent to this — the refresh must
  move *away*, not toward it. Our accent is magenta-fuchsia; lean into it as an *ink/
  highlight* color, not a glowing radial background.)
- **Glassmorphism** — frosted semi-transparent cards with blurred backgrounds and faint
  1px white borders.
- **Everything rounded-2xl and floating** with soft drop shadows; the "bento box" grid of
  uniformly-rounded tiles.
- **Gradient-clipped headline text**, glow effects, and neon accents on a dark canvas.
- **Inter/Geist for absolutely everything**, perfectly even spacing, no typographic
  personality or hierarchy beyond size.
- **Emoji as section bullets or in headings.**
- **Pill badges everywhere**, generic outline (heroicons/lucide) icons in evenly-spaced
  feature rows, the "trusted by" logo strip.
- The **Linear/Vercel/Framer-clone** look generally: symmetrical, weightless, cool-toned,
  characterless, "clean" to the point of anonymous.

**Instead, aim for something with authored character** — a point of view a human designer
would defend. Because this is a data-and-judgment site, promising directions to explore
(these are prompts, not mandates — surprise us):

- **Editorial / newspaper-of-record**: strong masthead, real typographic hierarchy, a
  serif or distinctive display face for headlines against a workhorse body/mono, rules and
  dividers instead of cards, dense tables treated like published standings. "The paper of
  record for IPv6 shame."
- **Scoreboard / leaderboard / standings-board**: sports-table or departures-board energy;
  ranks, deltas ("moved up 3"), streaks, a genuine "wall." Monospace numerics.
- **Terminal / diagnostic**: the site literally runs `dig AAAA`; a restrained,
  `dig`-output-inspired treatment for the data pages could feel native to the audience
  (network engineers) — but keep it a *flavor*, not a full green-on-black gimmick.
- **A real IPv6 motif** instead of the generic swoosh: the `::` double-colon, hextet
  groupings, `2001:db8::` address shapes, hex-nibble grids — visual language drawn from
  IPv6 notation itself, used sparingly as texture/dividers/marks.

Monospace already has a home here (rank pills, the changelog, the technical data) — leaning
into a **strong mono + a characterful display face + a clean body** trio is one good way to
escape the "Inter-everywhere" anonymity.

---

## 6. The screens & content (what has to fit)

The site's real surfaces, so a direction is judged on real content, not lorem ipsum:

- **Homepage** — intro/hero (keep the "Shame as a Service" pitch, tighter), the **Wall of
  Shame** ranked table (Rank · Domain · Apex · WWW · E-mail/MX · Nameserver, each a
  green-check / pink-cross / amber-dash status), a **Top Sinners** highlight, and the
  bottom-right **visitor-IP notification toast** ("No IPv6?!").
- **Domain leaderboard** (`/domain`) — the full ranked top-1M table with a **Heroes /
  Sinners** segmented toggle and pagination (50/page).
- **Domain detail** (`/domain/{name}`) — rank, per-record IPv6 status cards (Apex, WWW, NS,
  MX, real IPv6-only reachability), and a **90-day history timeline** of daily status
  blocks colored by the traffic-light language.
- **Country standings** (`/country`) — grid of countries with circle-flag + a
  percent-v6-ready figure.
- **Network providers** (`/metrics` → ASN) — per-operator dual-stack-vs-IPv4-only bars.
- **Live changelog** — a monospace feed of "went green / went red" events with timestamps.
- **Campaigns**, **FAQ/methodology**, **search**, **404**.

**Forward-compatibility with the rebuilt backend (design for these, they're coming):**

- The status model is **three states** — `supported` / `unsupported` / `no_record` (plus
  "not applicable"). There are **no letter grades and no 0–100 scores** — do not invent a
  score UI. Per-domain classification is a **deterministic ladder: Hero → Partial →
  Sinner**, with a **Gold** tier for domains that are fully IPv6-ready including their
  sub-resources. Design a clean way to show Hero / Partial / Sinner / **Gold** status and
  the reason flags (e.g. "www missing," "mail IPv4-only," "broken IPv6"). *(Country and
  campaign **percentages** are fine — those are aggregate adoption stats, not per-domain
  scores.)*
- New surfaces the design should leave room for: an **"Almost There"** list (`/domain/almost`
  — domains one step from Hero), an **embeddable status badge** per domain (a small
  SVG others put in their README — think a shields.io-style chip, in our language), a
  **methodology page** stating the exact Hero/Sinner rules, and a separate **mail (MX)
  heroes** track.

---

## 7. Deliverable & constraints

- Produce each direction as a **self-contained HTML page** (inline CSS; system/Google
  fonts are fine to name, embed or fall back gracefully) rendered so the maintainer can
  view them — **dark theme, fully responsive** (looks right on a phone and a wide desktop),
  horizontal scroll contained inside wide tables, never on the page body.
- Use **real-looking placeholder data** (actual well-known domains as Heroes and Sinners,
  plausible ranks, real country names) so the tables read like the live site.
- Keep the three status colors and the dark canvas from §3; take liberties with everything
  else per §4–§5.
- For each direction, add the **one-paragraph rationale** (§2) and a short **"what I kept
  vs. what I changed vs. how this avoids the generic-AI look"** note, so the comparison is
  legible.
- **Do not** build a full component library or all pages — two screens per direction, done
  well, is the point. This is a go/no-go exploration.

**The question this is meant to answer:** *is there a redesign here worth committing to —
one that keeps WhyNoIPv6's colors, snark, and status language, sheds the off-the-shelf
template feel, and looks like nothing else (least of all a generic AI-generated site)?*
