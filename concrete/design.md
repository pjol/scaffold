# Design system

All styling comes from `packages/tokens` (root-level definitions shared by web and mobile). Components live in `packages/ui-web` and `packages/ui-native`. The look of each app comes from the design process (design-process.md); this file is the system every look plugs into.

## House rules

1. **No subtitles.** No secondary text under titles, labels, rows, cards, dialogs or empty states. Context goes in an `InfoTip` (hover/focus on desktop, click pins; tap on touch). Components take `info`, never `description`.
2. **Tokens only.** No raw hex/rgb, no Tailwind default palette (`bg-blue-500` doesn't compile; the theme removes it), no off-scale spacing, no arbitrary `p-[13px]`. Native styles read `useTheme()`.
3. **Front pages only where necessary.** `/` is the product. Signed-out visitors land on the product's public surface or sign-in, not a marketing page. Add a landing page only when the app needs search traffic or acquisition, and then just one. Legal, support and `/delete-account` pages always exist.
4. **Motion on state changes**, springs from tokens, `prefers-reduced-motion` respected everywhere (nothing loops, durations drop to zero).
5. **Every async view has skeleton, empty and error states.** Skeletons match the final layout; empty = short title + one action; error = what failed + Retry.
6. **Not AI-looking.** No three-card feature rows, no left-border accent bars, no gradient text, no glow/neon, no emoji in chrome, no fake stats or decorative data, no pill-everything. Shadows only on floating layers.
7. **Mobile first.** Every web screen works one-handed at 375 px; every mobile screen at the largest Dynamic Type size.

A deliberately themed area (a game skin, a branded microsite) may break named house rules: uppercase, glows, a banned word that is the theme's own vocabulary. Keep it to one directory, keep its raw colours in that directory's theme file, and list each exception under Project rules in the project's CLAUDE.md. Everything outside the area follows the rules.

## Colour

Semantic roles per palette and mode (`--app-*` CSS variables, Tailwind classes, `theme.colors` on native):

| Role | Use |
|---|---|
| `canvas`, `surface`, `surfaceRaised`, `sunken`, `overlay` | page, cards, popovers, wells, scrims |
| `border`, `borderStrong` (≥3:1), `ring` (≥3:1) | dividers, input outlines, focus |
| `text` (≥7:1), `textMuted` (≥4.5:1), `textSubtle` (≥3:1, never essential), `textInverse` | copy |
| `primary`, `accent`, `success`, `warning`, `danger`, `info` | each with `x` (fill), `xFg` (text on fill, AA), `xSoft` (tint), `xStrong` (intent-coloured text on surfaces, AA) |
| `ink` 50–950 | brand-tinted neutrals for charts and illustration |

- Light and dark ship for every palette; users choose System / Light / Dark. Palettes switch with `data-palette`; set before first paint (no flash).
- One accent does the work. Colour marks state and action, not decoration.
- `pnpm check` runs the contrast audit for every palette and mode and the Tailwind name check; it must pass.
- Dark mode: shadows stop separating surfaces, so groups use a surface step or a low-alpha border.

## Spacing

Base unit **4 px**. Token name = multiple of 4 = Tailwind's number: `p-4` = 16 on web = `space[4]` on native.

**Approved steps:** 0 · px (hairlines only) · 0.5 (2) · 1 (4) · 1.5 (6) · 2 (8) · 3 (12) · 4 (16) · 5 (20) · 6 (24) · 8 (32) · 10 (40) · 12 (48) · 16 (64) · 20 (80) · 24 (96) · 32 (128). Nothing else for padding, margin or gap. 4-steps inside components, 8-steps for layout, geometric jumps above 48.

**Semantic layout tokens** (responsive at 640 / 1024): `tight` 4 · `related` 8 · `group` 16 · `subsection` 24 · `section` 32→40→48 · `band` 64→96→128 · `gutter` 16→24→32 · `card` 16→24. Use them for page structure: `<main class="mx-auto max-w-content px-gutter py-section flex flex-col gap-section">`. A theme name must not complete an existing Tailwind class: `--spacing-md` hijacks `max-w-md`, and the old `--spacing-block` gave every `inline-block` `inline-size: 1.5rem`. `pnpm check` compiles each theme name against the web app's Tailwind and refuses collisions (`packages/tokens/scripts/tailwind-names.mjs`); add new names to `ui-web/src/lib/cn.ts` too.

**The relationship ladder** — decide the level, read off the number. Each level is clearly larger (≥1.5×) than the one below; uniform spacing everywhere is the most common failure.

| Level | Question | Mobile | Desktop |
|---|---|---|---|
| L0 inside an element | parts of one thing? (icon↔label, value↔unit) | 4–8 | 4–8 |
| L1 inside a group | belong together? (label↔input, chips, button group) | 8–12 | 8–12 |
| L2 between groups | siblings? (field↔field, card↔card, paragraphs) | 16 | 16–24 |
| L3 between sections | different topic? | 32 | 48 (96 marketing) |
| L4 page edge | gutter | 16 | 24–32 |

Rules:
- **Internal ≤ external.** A container's padding ≤ the gap around it; its children's gap < its padding.
- **Headings belong to what follows**: space above ≥ 2× space below. Group heading + content in one `gap-3` stack; separate groups with `gap-section`. No `mt-*` on headings.
- **Components own padding; parents own gaps.** Reusable components never set outer margins. Web: `flex`/`grid` + `gap-*`. Native: `gap`/`rowGap`.
- Insets: square for cards (16/16), squish for buttons/chips/cells (vertical ≈ half horizontal), stretch for multiline inputs.
- Nested radii: inner = outer − padding.

**Defaults**

| Thing | Value |
|---|---|
| Page gutter | 16 phone · 24 tablet · 32 desktop, content max 1152 (`max-w-content`) |
| Section gap (app screens) | 32 · 40 · 48 |
| Card padding / inner gap / grid gap | 16→24 / 8–12 / 16→24 |
| List rows | 48 one-line (56 with avatar/icon) · 72 two-line · 88 three-line; padding-x 16 |
| Forms | label→control 8 · control→hint 4–6 · field→field 16 (24 with hints) · fieldset→fieldset 32 · last field→actions 24–32 · max width 576 (`max-w-form`) |
| Inputs | 48 tall on touch, 40 on desktop; text ≥16 px on touch (iOS zoom) |
| Buttons (height / padding-x) | sm 32/12 (pointer only) · md 40/16 (web default) · lg 48/20 (mobile default) · icon-only square; icon↔label 8 (6 in sm) |
| Top bar | 56 phone · 64 desktop. Mobile tab bar: native, never hardcoded |
| Dialogs | padding 24 (20 narrow); title→body 16, body→actions 24; max 448 confirm / 512 default / 672 form; phones prefer bottom sheets |
| Bottom sheet | padding-x 16–24, handle 32×4, bottom = 16 + safe-area inset |
| Reading width | 65ch (`max-w-prose`) |

## Type

All line heights are multiples of 4. Use the `type-<role>` utility (web) or `<Text role>` (native), never ad-hoc sizes.

| Role | Size/line | Weight | Use |
|---|---|---|---|
| caption | 12/16 | 500 | metadata, timestamps |
| label | 14/20 | 500 | form labels, dense UI |
| body | 16/24 | 400 | default copy, web and mobile |
| bodyLg | 18/28 | 400 | lead text |
| titleSm | 20/28 | 600 | card titles, list headers |
| title | 24/32 | 600 | mobile screen titles, H3 |
| headline | 30/36 | 700 | H2 |
| display | 36/40 | 700 | page H1 |
| hero | 48/52 (fluid) | 700 | the one landing headline, if any |

Tracking tightens as size grows (−0.01 → −0.03em). Never below 12/16 for content. Leave font scaling on (cap at 1.5–2× only on cramped chrome). Tabular figures for numbers in tables and money.

## Touch and density

- Hit areas: mobile ≥ 48 (never < 44); web ≥ 24 mandatory, 40 default, 44 under `(pointer: coarse)`. ≥ 8 between adjacent targets. Small visuals extend their hit area (`before:-inset-*` / `hitSlop`).
- Tap tokens: `tap-sm` 40 · `tap` 48 · `tap-lg` 56 · `tap-xl` 64.
- Density: comfortable everywhere; `data-density="compact"` only for pointer-only admin tables (controls −8, rows 40), forced off on touch.

## Shape and elevation

Radii `xs sm md lg xl 2xl pill` from the palette. Resting cards use a border or surface step; shadows (`elevation`) only on menus, popovers, dialogs, sheets and toasts. Layers come from `zIndex` (tokens): menus and selects open on the popover layer, above dialogs and sheets, so they work inside them.

## Logos and imagery

- Anything real (a company, product, tool, school) is shown by its real logo, never a redrawn lookalike, a letter in a circle or an emoji. Download each file once into `web/public/brand/` (mobile: `mobile/assets/brand/`); never hotlink. One registry per app, `src/lib/brands.ts`: `{ <id>: { src, mask } }`.
- `mask: true` for one-colour marks (most Simple Icons, any single-colour SVG or PNG with alpha). `BrandMark` (ui-web, ui-native) paints them with the surface's colour: `currentColor` by default, or a token, gradient or metal fill on a badge, so they follow palette and mode. Full-colour logos (`mask: false`) show as they are, on a plate when the surface is busy; never recolour or redraw one.
- In chrome (chips, menus, lists) marks are one colour. Full colour only where the logo is the content (a client list, a profile's employers). Connected platforms use `ProviderIcon`.
- A sheen over a mark (foil, holo; themed areas only) is a second `BrandMark` with the same `src`, a gradient `fill` and a `mix-blend-mode` (`screen` for foil, `color-dodge` for holo).
- One row per file in `design/brand/README.md`: id, entity, licence, source URL. Simple Icons are CC0 except where their metadata says otherwise (Solidity CC BY 4.0, Rust CC BY-SA 4.0): credit those on the site. Other marks are trademarks, used only to identify what they name.
- Imagery shows the real thing: the product's own screens, real photos (with the subject's consent), renders of the app's own models. Stock or generated images never stand in for a real person, place or product.

## Motion

- Web: `motion` (`motion/react`) with springs from tokens (`snappy`, `gentle`, `bouncy`, `sheet`, `press`) via `motionSpring()`; CSS transitions use `--ease-spring-*`. Native: Reanimated 4 with `reanimatedSpring()` (mass always explicit).
- Animate: enter/exit (`Presence`, fade + 8 px slide), list insertions (`Stagger`), layout changes (`layout`), press feedback (scale 0.97), live numbers (`AnimatedNumber`), state swaps (`SwapText`), success moments (`Celebrate`, sparingly).
- Durations 90 / 160 / 240 / 380 / 560 ms; stagger 45 ms; nothing over 600 ms on interaction. Showpiece sequences the person starts and can skip (an opening, a reel, a reveal) follow scenes.md.
- **OriginKit** (originkit.dev, animated React + Tailwind components, copied in as source): fetch per project with the owner's `ORIGINKIT_API_KEY` (`npx originkit@latest add <slug>` or its MCP), save to `web/src/components/motion/originkit/<name>.tsx`, switch `framer-motion` imports to `motion/react`, replace colours with tokens, wire `useReducedMotion`. Its license forbids vendoring components into templates, so the scaffold never contains them. Web only; native uses Reanimated equivalents.

## Layout and navigation

- **Web shell**: top bar (logo, primary action, account menu); side nav at ≥1024, bottom tab bar below 1024 (≤5 items, labelled icons). Admin is a section of the same app (`/admin`, side tabs), visible only to support/admin.
- **Mobile shell**: expo-router native tabs (≤5, labelled, badges for unread); stacks inside tabs with native headers; modals and sheets for create/edit flows.
- **Tabs behave predictably**: the active tab is in the URL (route or `?tab=`), so refresh, back and share keep it; re-tapping the active tab scrolls to top / pops to root; each tab keeps its scroll and state; arrow keys move between tabs (Radix Tabs); one active indicator, animated with a shared layout transition; overflow beyond 5 goes into a "More"/account menu, never a scrolling tab bar on mobile.
- **Menus**: Radix dropdown on web, native action sheet or menu on mobile; keyboard navigable; destructive items last, in `danger`, and confirmed.
- **Back** always returns to where the user came from with state intact; deep links open the right tab with the right stack beneath.
- One primary action per screen (sticky footer on mobile forms).
- Settings structure: Profile · Appearance · Notifications (if the feature is installed) · Account (export, delete) · Legal · Support. Admin entry appears only for staff.

## Components and states

Use the ui packages before writing new primitives: Button, IconButton, Field/Input/TextArea/Select/Switch/Checkbox/SegmentedControl, Card, ListRow, Avatar, BrandMark, Badge, Chip, Tabs, Dialog/ConfirmDialog, Sheet, Menu, Toast, Tooltip/InfoTip, Skeleton*, EmptyState, ErrorState, Spinner, AnimatedNumber, Presence/Stagger/SwapText, AppShell/TopBar/SideNav/BottomNav (web), Screen/TabBar (native), LegalDocument, ConsentForm. A new primitive goes into the ui package, uses tokens only, and has no subtitle prop.

## Accessibility

Text contrast AA (body ≥ 4.5:1, large/UI ≥ 3:1), visible focus ring on every interactive element, labels on every control (icon buttons require `label`), dialogs trap focus and restore it, reduced motion honoured, layouts survive 200% zoom and the largest Dynamic Type, no information by colour alone.

## Screen review checklist

Screenshot at 375 / 768 / 1440 (native: compact + largest text, light + dark). Fail the screen if:
1. spacing is uniform across levels, or a heading floats (space above < 2× below);
2. a container's padding exceeds the gap around it, or children's gap ≥ padding;
3. any off-scale value, raw colour or default-palette class exists;
4. gutters don't align between header, body and footer, or similar screens differ;
5. touch targets are under 48 (mobile) / 24 (web) or closer than 8;
6. lines run past 75ch, or form inputs stretch full width on desktop;
7. content hides under notches, home indicator or the tab bar; the last list item is covered;
8. a reusable component carries an outer margin;
9. controls in one row have different heights;
10. there is a subtitle, a three-card row, decorative data, or a missing skeleton/empty/error state;
11. card boundaries vanish in dark mode;
12. anything animates without reduced-motion handling.
