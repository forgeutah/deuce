# Deuce — Design System

A folder-based design system for **Deuce**, an open-source, channel-style shared workspace where humans (engineer, designer, PM) and AI agents collaborate on the same product across scope, plan, design, build, and test — instead of each person prompting agents in private CLI sessions and meeting at the PR.

> **Status of the product:** very early, pre-alpha. Built in the open by [Forge Utah](https://github.com/forgeutah).
> **What this folder is:** everything an agent or designer needs to produce on-brand Deuce interfaces and assets — tokens, type, real logo/icon assets, written guidelines, and a high-fidelity UI kit recreation of the app.

---

## Sources

This system was reverse-engineered from the real Deuce codebase. If you have access, explore these to go deeper — the code is the source of truth, and reading it will let you build far more faithful Deuce designs than this summary alone:

- **GitHub repo:** <https://github.com/forgeutah/deuce> (branch `main`)
  - `src/styles/globals.css` — the canonical color/type/radius/shadow token set (Primer dark)
  - `src/components/` — the real React + shadcn/ui components recreated in the UI kit
  - `src/mocks/data/seed.ts` — product copy, sample sessions, agents, messages (the source of the tone examples below)
  - `README.md`, `STRATEGY.md`, `CLAUDE.md` — product positioning, strategy, and engineering conventions
- **Design inspiration cited by the project:** Maggie Appleton's [Zero Alignment](https://maggieappleton.com/zero-alignment) essay, and GitHub's ACE research prototype.

There is no Figma file. The app is **dark-mode only** and uses **GitHub Primer dark** color primitives with a **Slack three-pane layout** — that DNA is documented throughout.

---

## The product at a glance

Each **session** is like a Slack channel for one piece of work. Humans and agents post in the same thread; every session is backed by an isolated [DevPod](https://devpod.sh) dev container with the repo checked out. A session carries the whole arc through four tabs:

| Tab | What it is |
| --- | --- |
| **Chat** | The channel. Humans + agents message in one thread, with `@mentions`, typing indicators, and expandable diffs/test-results inline. |
| **Plan** | A markdown plan document (editor / split / preview) shared by the room. |
| **Files** | Read-only file browser of the workspace, with git-status markers. |
| **Terminal** | A live shared terminal attached to the DevPod container. |

The shell is three resizable panes: **Session sidebar** (left) · **Center panel** with the tabs (middle) · **Summary panel** with participants + activity feed (right).

**Agent roles** are first-class and color-coded:

| Role | Color | Does |
| --- | --- | --- |
| Coder | Blue `#58a6ff` | Writes and modifies code |
| Reviewer | Purple `#BE8FFF` | Reviews diffs critically |
| Planner | Green `#3fb950` | Breaks work into ordered plans |
| Tester | Yellow `#d29922` | Writes and runs tests |
| Designer | Pink `#f778ba` | UI/UX suggestions grounded in the system |

---

## Content Fundamentals

How Deuce writes. The voice is that of a **calm, technical, open-source product** — closer to GitHub/Linear than to a consumer SaaS. It respects that the reader is an engineer.

**Tone & vibe**
- **Plain, direct, declarative.** Sentences state things. "Implementation is no longer the bottleneck — alignment is." No hype, no exclamation marks in product copy.
- **Confident but humble about maturity.** The product openly labels itself "very early, pre-alpha" and invites contributors. Honesty over polish.
- **Thesis-driven.** Marketing/positioning copy argues a point ("agents as teammates, not tools") rather than listing features.

**Person & address**
- Product UI speaks **to the user with imperatives and short fragments**: "Search sessions…", "Message (@ to mention an agent)", "Start a conversation", "Select a session from the sidebar or create a new one to get started."
- Strategy/README docs use **"we" / "our"** for the team's bets ("Deuce's bet:", "Our approach"), and **"you"** when addressing a contributor ("Pick something unchecked above").
- Agents speak in **first person, matter-of-fact**: "I've set up the auth middleware and user model." "Tests are written and passing. I've covered valid tokens, expired tokens, invalid format, and empty input."

**Casing**
- **Sentence case everywhere** — headings, buttons, menu items, dialog titles. ("New Session", "SSH Keys", "Open in VS Code" are title-ish only because they're proper UI/product nouns.)
- Section eyebrows in the right rail are the one **UPPERCASE** moment: `PARTICIPANTS`, `ACTIVITY`, `AGENTS`, `MEMBERS` — small, letter-spaced, muted.
- The product name is always **Deuce** (capital D). Sessions are lowercase-kebab nouns prefixed with `#`: `#auth-module`, `#api-rate-limiting`, `#homepage-redesign`.

**Mechanics & specifics**
- **Em dashes** for asides and definitions — used liberally, the project's signature punctuation.
- Session descriptions are **terse, concrete, one line**: "JWT validation and refresh-token flow for the v2 API", "Token-bucket rate limiter via Redis, per-endpoint config".
- **Code is shown, not described** — agents attach real diffs (`internal/auth/validate.go (+12 -3)`) and test output (`4/4 passing`) rather than narrating them.
- Empty states are **instructive, never cute**: they tell you the next action.

**Emoji:** none. Zero emoji in product UI or copy. Status and meaning are carried by color, icons, and dots — never by emoji. Do not introduce them.

---

## Visual Foundations

The aesthetic is **GitHub Primer dark + Slack layout** — a dense, quiet, professional developer tool. Read `colors_and_type.css` for the full token set.

**Color & theme**
- **Dark mode only.** There is no light theme. The canvas is near-black `#0D1117`; panels step up through a 13-step neutral scale to `#F0F6FC` text.
- **Surfaces are flat, distinguished by value, not by shadow.** Sidebar/summary rails are `#151B23` (subtle), the active row is `#262C36` (emphasis), hover is `#212830`, the code/terminal well drops to the deepest inset `#010409`.
- **Blue `#58a6ff` is the single product accent** — active tab underline, links, the active-session left border, focus rings, primary buttons (`#1f6feb`). Everything else stays neutral until it needs to mean something.
- **Semantic colors are Primer's:** success green, danger red, warning yellow, plus purple/pink/orange used mostly for **agent identity**, not decoration.
- **Two color worlds, kept in their lanes.** The **brand** is a phosphor-green retro-terminal lockup (the logo: a 2-of-clubs *deuce* card in a CRT window, `#60C070` on CRT navy `#151922`); the **product UI** is Primer neutrals + a blue accent. Use the green for the logo / splash / marketing chrome only — never as an in-app accent. They harmonize because the brand green sits beside Primer's success green and the CRT navy is within a hair of Primer `neutral-2` (`#151B23`).
- **Imagery vibe:** almost none inside the app. No photography, no hero images. The only brand image is the terminal-green pixel logo lockup; in-app, DiceBear `avataaars` cartoon avatars stand in for humans. The visual interest is the data: code, diffs, status dots, colored agent chips. Where brand imagery does appear (splash, marketing), the register is **8-bit / CRT terminal** — pixel type, scanline-thin strokes, phosphor glow.

**Typography**
- **Native system stack only** — `-apple-system, "Segoe UI", …`. Deuce ships **no webfonts**; it deliberately reads like the OS / like GitHub. Monospace is `ui-monospace, "SF Mono", Menlo, …` for all code, diffs, and terminal.
- **Small and dense.** Root size is **14px**. Most metadata is 12px; eyebrows and badges 10px; the largest routine text (wordmark, view titles) is 18px. There is no big display type.
- Weights used: 400 / 500 / 600. Headings are 600; nothing heavier.

**Spacing, radii, borders**
- **4px spacing base** (Tailwind). Tight rhythm — rows are `py-1.5`, panels `p-3`, controls `h-7`/`h-8`/`h-9`.
- **Radii:** 4 / 6 / 8 / full. **6px is the default** (buttons, inputs, cards). Avatars, status dots and pills are fully round.
- **Borders are the primary separator,** not shadows: `#3D444D` default, `#2F3742` muted. Dividers everywhere. The active session uses a **2px accent left-border**; agent messages use a **2px left-border in the agent's color** over a ~3%-opacity tint of that color (`${color}08`).

**Elevation & shadows**
- Shadows are subtle and reserved for **true overlays** (dropdowns, dialogs) — `0 1px 2px` up to `0 16px 32px`, all built on near-black `rgba(1,4,9,…)`. In-flow surfaces (cards, panels, rows) get **no shadow**; they separate by background value + border. No glows, no neon.

**Motion**
- **Fast and functional.** Most transitions are 150ms color/opacity (`transition-colors`). New messages do a 4px `fade-in-up` over 150ms.
- **Three decorative loops, each tied to live status:** `pulse-dot` (2s, on a "starting"/working status dot), `typing-dots` (1.4s staggered, the agent-working indicator), and `shimmer` (1.5s, loading skeletons). No bounces, no easing flourishes, no ambient animation.

**Interaction states**
- **Hover:** background lifts one neutral step (`hover:bg-background-hover`) or muted text brightens to `--fg`. Icon buttons go muted→foreground.
- **Active/selected:** emphasis background + accent left-border (sessions), or accent underline (tabs).
- **Focus:** accent border + a 3px accent-at-50% ring (`focus-visible:ring-ring/50`).
- **Press:** no shrink/scale — color is the only feedback. Disabled is `opacity-50` + `cursor-not-allowed`.
- **Read-only / paused / archived** sessions dim to `opacity-60` / `opacity-40`.

**Transparency & blur**
- Used sparingly: agent-tint message backgrounds (`color + 08` hex alpha), the danger hover (`danger/10`), focus rings at 50%. **No backdrop-blur, no frosted glass.** Overlays are solid `#2F3742` with a real shadow.

**Layout rules**
- **Three resizable horizontal panes** (sidebar 20% · center 55% · summary 25%, min sizes enforced). The center panel has a fixed session header + tab bar, then a scrolling tab body. Custom 8px thin scrollbars styled for dark.

---

## Iconography

- **Lucide** (`lucide-react`) is the **one and only in-app icon system** — thin (≈1.5–2px), rounded-join, outline icons that pair with Primer. They render at `h-3` (12px), `h-3.5` (14px), or `h-4` (16px) and inherit text color (muted by default, foreground on hover/active). Pull Lucide from CDN when recreating UI — see below. Icons seen in the app: `Hash`, `Search`, `Plus`, `Pencil`, `Users`, `Key`, `Settings`, `ChevronDown/Right`, `MessageSquare`, `FileText`, `FolderTree`, `Terminal`, `ScrollText`, `Code`, `SendHorizontal`, `Bot`, `Square`, `Loader2`, `File`, `GitCommit`, `CircleCheck`, `Circle`, `Split`, `Eye`.
- **No icon font, no PNG icons.** Everything is SVG (Lucide at runtime).
- **Status is drawn, not iconified:** small filled `rounded-full` dots carry workspace/agent status (green ready, pulsing yellow starting, red failed, neutral suspended). Unread counts are tiny red `rounded-full` numeric pills.
- **Emoji / unicode as icons:** never. The `#` session prefix is a Lucide `Hash`, not the character.
- **Brand / social marks** (in `assets/social-icons.svg`, a `<symbol>` sprite from the Forge web presence): GitHub, X, Bluesky, Discord, plus two stroked marks (documentation, social). For marketing footers, not the app.
- **Logo:** `assets/deuce-logo.png` — the terminal-green pixel lockup: a 2-of-clubs *deuce* playing card wired with circuit / neural-net nodes, above the pixel wordmark **DEUCE** and the tagline **"Collaborative AI Programming"** (with a blinking cursor block). Use whole as a lockup; don't recolor, stretch, or redraw it. A legacy mark (`assets/deuce-bolt-legacy.svg`, the repo's original purple→blue "deuce" bolt favicon) is kept for reference only.

### Using Lucide in recreations
```html
<script type="module">
  import { createIcons, icons } from "https://cdn.jsdelivr.net/npm/lucide@latest/dist/esm/lucide.js";
  createIcons({ icons });
</script>
<!-- then: <i data-lucide="hash"></i> -->
```
The UI kit in `ui_kits/app/` already wires Lucide this way.

---

## Index / Manifest

Root files:
- **`README.md`** — this file: context, sources, content + visual foundations, iconography, manifest.
- **`colors_and_type.css`** — all design tokens: brand + Primer neutral scale, semantic bg/fg/border, accent + semantic colors, agent role colors, type scale + semantic type roles, radii, shadows, spacing, motion.
- **`SKILL.md`** — Agent-Skill front-matter wrapper so this folder can be used as a Claude Skill.
- **`assets/`** — `deuce-logo.png` (the terminal-green pixel lockup, primary logo), `deuce-bolt-legacy.svg` (the repo's original purple→blue bolt favicon, reference only), `social-icons.svg` (GitHub/X/Bluesky/Discord/etc. sprite).
- **`preview/`** — small HTML specimen cards that populate the Design System tab (colors, type, agents, components, etc.).
- **`ui_kits/app/`** — the high-fidelity, interactive recreation of the Deuce workspace. See its own `README.md`.

### UI kits
- **`ui_kits/app/`** — *Deuce workspace.* The three-pane channel app: session sidebar, chat with agents + expandable diffs, plan/files/terminal tabs, and the participants/activity rail. `index.html` boots into a live, clickable session. This is the one product surface — there is no separate marketing site or docs site in the repo.

---

## Caveats & substitutions

- **Fonts:** none to substitute — Deuce uses the native system stack by design. Recreations should keep `-apple-system, "Segoe UI", …` and not introduce a webfont.
- **Icons:** Lucide is loaded from CDN in the UI kit (the repo bundles it via npm). If you need it offline, vendor `lucide`.
- **Avatars:** the seed data uses DiceBear `avataaars` URLs; the kit uses colored initial-chips / DiceBear to avoid a network dependency where possible.
- There is **no light theme, no Figma, and no marketing website** in the source — don't invent them.
