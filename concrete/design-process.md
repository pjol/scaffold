# Design process

How every app gets its visual identity: three complete options, then iterations, then palettes, then adoption. Run it before building product screens, and again for any major restyle. It worked on ChatMTG; the user's feedback style is short picks plus mix-and-match ("X feels like the obvious choice", "A's fonts with B's logo", "no browns"). Build each round so that kind of decision is easy.

Everything lives in `design/` at the project root. Mockups are static HTML/CSS/JS, served from disk, never shipped.

## Inputs (before round 1)

1. **House rules**: no subtitles (tooltips instead), tokens only, front pages only where needed (`/` is the app), meaning-first copy (writing.md), spacing system (design.md).
2. **A quality reference**: an existing project or site named by the user, "for the level of quality, not the style".
3. **A shared content kit** in `design/assets/` so options differ only in design:
   - `data.js`: real data from the dev database or API, written by a script ("generated; do not edit"). Never invented.
   - `content.js`: the actual copy for every screen, already following writing.md.
   - `kit.js`: data access and **unstyled** markup helpers; each option styles its classes.
   - `icons.js` (the lucide icons the app uses) and any real images.
   - Theme from `?theme=light|dark`, then localStorage, then the option's default.
4. **The token names the app uses** (`packages/tokens`), so every option's `tokens.css` is a drop-in.

## Round 1: three complete identities

`design/<option>/` for each of three genuinely different options (light-first vs dark-first; serif vs grotesk vs condensed; different accent philosophies). Each contains:

| File | Contents |
|---|---|
| `index.html` | style guide, 11 sections: 1 Philosophy (three principles, each with a live demo) · 2 Palette (both modes, values read live from `tokens.css`) · 3 Type (specimen with real content) · 4 Shape and elevation · 5 Icons · 6 Components · 7 Motion (durations, easings, reduced motion) · 8 Voice (Moment / Say / Not table) · 9 Do and don't (paired panels) · 10 Screens · 11 Adoption notes (tokens that change meaning, new tokens, font snippet) |
| 4–6 screen mockups | the app's key surfaces, live HTML, work at 390 px, `?theme=` both modes |
| `tokens.css` | the full app token set in light and dark, AA text contrast |
| `shots/` | from `design/tools/capture.mjs` (desktop 1440×900 and phone 390×844, light and dark, reduced motion) |
| notes | a one-line tagline, a **Signature** note and an honest **Watch-outs** note |

`design/index.html` is the **comparison board**: neutral graphite/paper styling so each option's colour stands on its own; a strip per option (name, default mode, swatches, tagline, fonts); an "At a glance" table (feels like, default mode, fonts, principles, accent, strongest at, new tokens, build effort); per-option proofs (phone + desktop) with Signature/Watch-outs; "Same screen, three ways"; toggles for each default / light / dark.

## Round 2: five iterations of the pick

`design/<pick>-iterations/<name>/`, same file contract plus `logo.html` (logo sheet), `logo.svg` and `favicon.svg` (16 px cut). Each iteration changes the colour scheme, a few details (radii, type weight, glass), and has its own logo concept with a one-line idea. The board puts **the original first in every row** and gives each iteration "What works" / "Watch-outs".

## Round 3: lock, then five palettes

The user locks a hybrid (e.g. original fonts and components + one iteration's logo, with fixes) and states negative constraints. `design/<pick>-palettes/<palette>/`: five complete token sets in both modes, each with named modes, one light-first, AA text everywhere. The board opens with a "Locked in" panel listing what no longer changes.

## Round 4: final design

`design/final/` (or the app's name): the screens in the chosen default palette, `palettes/<id>.css`, a registry + boot script that sets `data-palette` and `data-theme` before first paint (`?palette=` sticks), favicons per palette, a Settings → Appearance mockup (palette picker, mode: system/light/dark, motion switch), the logo sheet with its animation.

## Round 5: adopt into the app

1. Convert each palette's CSS into `packages/tokens/src/palettes/<id>.ts`, register them, set the default; `pnpm tokens` then `pnpm check` (every palette × mode passes the contrast audit).
2. Fonts: `next/font` on web, `@expo-google-fonts/*` on mobile, named in the palette/type tokens.
3. Logo component(s) from the logo sheet (web SVG + native `react-native-svg`), app icon and splash generated from it.
4. Settings → Appearance picker if more than one palette ships.
5. Rework screens area by area. Parallel agents work from one shared brief, verify with live screenshots across palettes and modes, and each reports a "Not matched" list (what the mockup shows that the app can't yet do). The lead reconciles.
6. Keep `design/` in step with the app afterwards: when the user reviews a mockup, the change must land in both, and say which surface changed.

## Rules for every round

- Run `node design/tools/capture.mjs design/<folder>` and fix errors: it fails on page errors, failed requests and **raw colours** in screen files (hex/rgb/hsl/oklch outside `tokens.css`, the style guide and the logo sheet).
- Mockups obey the house rules: no subtitles, factual copy, real data, no three-card feature rows, no gradient text, no emoji chrome.
- Every option ships both modes and works at 390 px.
- Motion in mockups uses the token durations and respects reduced motion.
- Present each round with the board path and a 3-line summary per option; wait for the pick. Never adopt before the user locks a choice.
