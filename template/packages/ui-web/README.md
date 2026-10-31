# @scaffoldapp/ui-web

Web components for `web/`: Tailwind v4 (CSS-first), `motion` (`motion/react`) and Radix, themed entirely from `@scaffoldapp/tokens` classes and `--app-*` variables. Consumed as TypeScript source; client components carry their own `"use client"`.

## Setup (done in web/)

```css
/* web/app/globals.css */
@import "tailwindcss";
@import "@scaffoldapp/tokens/theme.css";
@source "../../packages/ui-web/src";
```

```tsx
// web/app/layout.tsx (server)
<html suppressHydrationWarning><head><ThemeScript /></head><body><Providers>{children}</Providers></body></html>
// web/src/providers.tsx (client)
<UIProvider>{children}<Toaster /></UIProvider>
```

- `ThemeScript` restores the stored palette (`data-palette`) and mode (`data-theme`) before first paint. Keys: `scaffoldapp.palette`, `scaffoldapp.theme`.
- `UIProvider` = `MotionConfig reducedMotion="user"` + the Radix tooltip provider.
- `useColorMode()` → `{ mode, preference, setPreference("system" | "light" | "dark") }`; `usePalette()` → `{ id, palette, ids, setPalette }`; `useThemeColors()` for code that needs concrete colours (Clerk, charts).

## House rules the API enforces

- **No subtitles.** No component has a `description`/`subtitle` prop. Context goes in `info`, which renders an `InfoTip` (hover/focus on fine pointers with click-to-pin; a popover on touch; 24 px hit area, 44 on coarse pointers).
- **Tokens only.** Colours, type (`type-*`), spacing (4 px steps and `gutter`/`section`/`card`), sizes (`h-control`, `min-h-tap`), radii and shadows come from the theme.
- **Sizes from design.md.** Buttons `sm` 32 · `md` 40 · `lg` 48 · `auto` (48 below 640 px, 40 above); inputs 40, 48 on touch, 16 px text; sm/md targets grow to 44 px on coarse pointers through a transparent `::before`.
- **Shadows only on floating layers** (menus, popovers, dialogs, sheets, toasts). Cards use a border or a surface step.
- **Motion on state changes** with springs from the palette (`useMotionTokens()`), all reduced-motion aware.
- **Every async view** has `Skeleton*`, `EmptyState` (title + one action) and `ErrorState` (what failed + Retry).
- Interactive rows (`ListRow` with `onClick`/`asChild`) can't contain other controls, so they ignore `info`.

## Components

| Area | Exports |
|---|---|
| Theme | `ThemeScript`, `themeInitScript`, `UIProvider`, `useColorMode`, `usePalette`, `useThemeColors` |
| Motion | `Presence` (enter/exit, fade + 8 px), `Collapse`, `SwapText`, `FadeIn`, `Stagger`/`StaggerItem`, `PageTransition`, `useMotionTokens`, re-exports of `motion`, `AnimatePresence`, `LayoutGroup`, `useReducedMotion`… |
| Actions | `Button` (`variant` primary/secondary/outline/ghost/soft/accent/danger/danger-ghost/success, `size`, `loading`, `leftIcon`, `rightIcon`, `asChild`), `IconButton` (`label` required, `tooltip`), `Menu`/`MenuItem`/`MenuSeparator`/`MenuLabel` |
| Surfaces | `Card` (`variant`, `padding` card/compact/none, `interactive`, `asChild`), `ListRow` (`title`, `leading`, `trailing`, `info`, `destructive`, `asChild`), `Divider`, `Banner` |
| Status | `Badge` (`tone` × soft/solid/outline, `dot`, `pop`), `Chip`, `ToggleChip`, `Avatar` (`renderImage` for `next/image`) |
| Help and text | `InfoTip`, `Tooltip`, `Hero`, `Display`, `Headline`, `Title`, `TitleSm`, `BodyLg`, `Body`, `Label`, `Caption` |
| Forms | `Field` (`label`, `info`, `error`; wires id and aria), `Input`, `TextArea`, `Select`, `Switch`, `Checkbox`, `SegmentedControl`, `Slider`, `Stepper` |
| Overlays | `Dialog` (`size` sm 448 / md 512 / lg 672, bottom-aligned on phones), `ConfirmDialog` (async `onConfirm`; a throw keeps it open; `requireText` for type-to-confirm), `Sheet` (bottom sheet on phones, side panel ≥ 768 px), `Tabs`/`TabsList`/`Tab`/`TabPanel`, `Toaster`/`toast` |
| States | `Skeleton`, `SkeletonText`, `SkeletonRow`, `SkeletonGroup`, `Spinner`, `EmptyState`, `ErrorState` |
| Numbers and moments | `AnimatedNumber`, `Money`, `formatCents`, `Celebrate`, `RatingStars` |
| Photos | `PhotoUploader` (wire `upload` to `useUpload().mutateAsync`), `PhotoGrid`, `PhotoLightbox` |
| Legal | `LegalDocument` (contents list, anchors; takes the shared legal documents), `ConsentForm` |
| Shell | `AppShell`, `TopBar`, `SideNav` (≥ 1024 px), `BottomNav` (< 1024 px, ≤ 5 items), `isNavItemActive`, `ShellNavItem` |
| Utils | `cn`, `Slot`, `useMediaQuery`, `useHoverCapable`, `useIsDesktop`, `useControllableState`, `useLatest`, `useMounted` |

A new primitive goes here, uses tokens only, has no subtitle prop, and gets a row in this table.

## OriginKit

Animated components from originkit.dev are added per project with the owner's key, never vendored. See `src/motion/originkit/README.md`.
