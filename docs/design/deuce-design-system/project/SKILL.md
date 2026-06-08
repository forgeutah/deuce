---
name: deuce-design
description: Use this skill to generate well-branded interfaces and assets for Deuce, either for production or throwaway prototypes/mocks/etc. Contains essential design guidelines, colors, type, fonts, assets, and UI kit components for prototyping. Deuce is an open-source, channel-style shared workspace where humans and AI agents collaborate on the same product (GitHub Primer dark UI; terminal-green brand).
user-invocable: true
---

Read the `README.md` file within this skill, and explore the other available files.

If creating visual artifacts (slides, mocks, throwaway prototypes, etc), copy assets out and create static HTML files for the user to view. If working on production code, you can copy assets and read the rules here to become an expert in designing with this brand.

If the user invokes this skill without any other guidance, ask them what they want to build or design, ask some questions, and act as an expert designer who outputs HTML artifacts _or_ production code, depending on the need.

## What's here
- `README.md` — full context, content + visual foundations, iconography, manifest. **Start here.**
- `colors_and_type.css` — all design tokens (brand terminal-green + Primer-dark neutrals, semantic colors, agent role colors, type scale + semantic type roles, radii, shadows, spacing, motion). Import it or copy the values.
- `assets/` — `deuce-logo.png` (terminal-green pixel logo lockup), `deuce-bolt-legacy.svg` (legacy mark), `social-icons.svg`.
- `preview/` — specimen cards for every token group.
- `ui_kits/app/` — interactive, reusable recreation of the Deuce workspace (React + JSX, no build). Lift components from here.

## Rules of thumb
- **Dark mode only.** Canvas `#0D1117`, panels `#151B23`, text `#D1D7E0`/`#F0F6FC`.
- **Blue `#58a6ff` is the in-app accent.** Use the terminal **green `#60C070`** only for the logo/splash/marketing — never as an in-app accent.
- **System font stack, no webfonts.** Mono for all code/diffs/terminal. Small + dense: 14px base.
- **Lucide icons** only, thin outline. No emoji, ever.
- **Borders separate, not shadows.** 6px default radius. Agent identity is carried by color (Coder blue, Reviewer purple, Planner green, Tester yellow, Designer pink).
- Voice: plain, direct, technical, lowercase-kebab session names, em dashes. No hype.
