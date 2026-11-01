# @scaffoldapp/tokens

Design tokens for web and native. Pure data (no React, DOM or React Native), consumed as TypeScript source; no build step.

## What's exported

| Import | Contents |
|---|---|
| `@scaffoldapp/tokens` | `PALETTES`, `PALETTE_IDS`, `DEFAULT_PALETTE_ID`, `activePaletteId()`, `getPalette()`, types, color math, `auditPalette`, `motionSpring` / `reanimatedSpring` / `springToCssLinear`, `space`, `size`, `tap`, `containers`, `zIndex`, `BREAKPOINTS`, `layoutFor(width)` |
| `@scaffoldapp/tokens/theme.css` | Tailwind v4 layer. Import after `@import "tailwindcss";` |
| `@scaffoldapp/tokens/native` | `createNativeTheme(paletteId, mode)` → `{ colors, space, size, tap, type, radius, elevation, motion, fonts, font }` |
| `@scaffoldapp/tokens/palettes.json` | `{ default, palettes: { id: { name, splash, primary, primaryFg } } }` for `app.config.ts` |

The palette comes from `NEXT_PUBLIC_PALETTE` / `EXPO_PUBLIC_PALETTE` (default `base`). On the web, put `data-palette={activePaletteId()}` on `<html>` when it isn't the default; `data-theme="light|dark"` forces a mode, otherwise the OS decides. Load fonts with next/font `variable: "--app-face-body"` / `"--app-face-display"`.

## Tailwind classes

Tailwind's color palette is removed (`bg-red-500` doesn't compile). Colors: `bg-canvas`, `bg-surface`, `bg-surface-raised`, `bg-sunken`, `bg-overlay`, `border-border`, `border-border-strong`, `ring-ring`, `text-fg`, `text-fg-muted`, `text-fg-subtle`, `text-fg-inverse`, `bg-inverse`, and per intent (`primary accent success warning danger info`) `bg-x`, `text-x-fg`, `bg-x-soft`, `text-x-strong`; neutrals `ink-50`…`ink-950`.

- Spacing: 4px base, Tailwind numbers. Approved steps 0, px, 0.5, 1, 1.5, 2, 3, 4, 5, 6, 8, 10, 12, 16, 20, 24, 32. Semantic: `tight` 4, `related` 8, `group` 16, `block` 24, `section` 32/40/48, `band` 64/96/128, `gutter` 16/24/32, `card` 16/24 (switching at 640 and 1024px), e.g. `px-gutter py-section gap-group p-card`.
- Sizes: `h-control-sm` 32, `h-control` 40, `h-control-lg` 48, `h-row` 48, `h-topbar` 56/64; touch `min-h-tap-sm` 40, `min-h-tap` 48, `size-tap-lg` 56, `size-tap-xl` 64; `max-w-content` 72rem, `max-w-form` 36rem. `data-density="compact"` shrinks controls by 8 and rows to 40, except on touch screens.
- Type: `type-caption`, `type-label`, `type-body`, `type-body-lg`, `type-title-sm`, `type-title`, `type-headline`, `type-display`, `type-hero` (fluid), or `text-<role>` for size + line height only.
- Radius `rounded-xs…2xl`, `rounded-pill`; shadows `shadow-xs…xl`; easing `ease-standard`, `ease-spring-snappy` (and `gentle`, `bouncy`, `sheet`, `press`); `spring-*` sets easing and duration together; `animate-shimmer` (with `shimmer`), `animate-fade-in/out`, `animate-pop-in/out`, `animate-slide-up/down`; `pb-safe`, `pt-safe`; z-index `z-(--app-z-modal)`; durations `duration-(--app-duration-base)`.

Never name a spacing token `sm/md/lg/xl`: in Tailwind v4 it hijacks `max-w-*`.

## Adding a palette

Copy `src/palettes/base.ts` to `src/palettes/<id>.ts`, add it to `PALETTES` in `src/registry.ts` (the only list of ids), then run `pnpm tokens` and `pnpm check`.

## Commands

- `pnpm tokens`: regenerate `src/generated/tokens.css` and `palettes.json` (commit them).
- `pnpm check`: contrast audit (7 / 4.5 / 3) for every palette and mode, 4px-grid check, stale generated files.
- `pnpm --filter @scaffoldapp/tokens contrast`: every ratio as a Markdown table.
