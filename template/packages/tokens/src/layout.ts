/**
 * Spacing, sizing and layout. Palette-independent: every palette shares one rhythm.
 *
 * The base unit is 4px, so Tailwind's default spacing numbers apply unchanged
 * (`--spacing: 0.25rem`, `p-4` = 16px). Semantic names are never `sm/md/lg/xl`: in
 * Tailwind v4 a `--spacing-md` token hijacks `max-w-md`.
 */

/** Approved spacing steps, in Tailwind numbers (× 4px). */
export const SPACE_STEPS = [0, "px", 0.5, 1, 1.5, 2, 3, 4, 5, 6, 8, 10, 12, 16, 20, 24, 32] as const;
export type SpaceStep = (typeof SPACE_STEPS)[number];

/** Step → px for native, plus the fixed semantic gaps. `space[4]` = 16, `space.group` = 16. */
export const space = {
  0: 0,
  px: 1,
  0.5: 2,
  1: 4,
  1.5: 6,
  2: 8,
  3: 12,
  4: 16,
  5: 20,
  6: 24,
  8: 32,
  10: 40,
  12: 48,
  16: 64,
  20: 80,
  24: 96,
  32: 128,
  /** Icon to label, chip padding. */
  tight: 4,
  /** Items that belong together: label to input, list row parts. */
  related: 8,
  /** Fields in a form, cards in a grid. */
  group: 16,
  /** Between blocks inside a section. */
  block: 24,
} as const;

/** Breakpoints the responsive tokens switch at (Tailwind `sm` and `lg`). */
export const LAYOUT_BREAKPOINTS = { sm: 640, lg: 1024 } as const;

/**
 * Semantic layout spacing in px: `[base, ≥640, ≥1024]` (missing entries keep the
 * previous value). Web: `--app-space-<name>` → `p-gutter`, `py-section`, `gap-group`.
 */
export const LAYOUT_SPACE = {
  tight: [4],
  related: [8],
  group: [16],
  block: [24],
  /** Between sections of a page. */
  section: [32, 40, 48],
  /** Between full-width bands of a landing page. */
  band: [64, 96, 128],
  /** Page side padding. */
  gutter: [16, 24, 32],
  /** Card padding. */
  card: [16, 24],
} as const satisfies Record<string, readonly number[]>;
export type LayoutSpaceName = keyof typeof LAYOUT_SPACE;

/** Component sizes in px (comfortable density). */
export const size = {
  controlSm: 32,
  control: 40,
  controlLg: 48,
  row: 48,
  topbar: 56,
} as const;
export type SizeName = keyof typeof size;

/** Sizes that grow at ≥640px on the web. */
export const SIZE_RESPONSIVE: Partial<Record<SizeName, readonly number[]>> = { topbar: [56, 64] };

/** `[data-density=compact]` sizes. Ignored under `(pointer: coarse)`. */
export const COMPACT_SIZE: Partial<Record<SizeName, number>> = {
  controlSm: 24,
  control: 32,
  controlLg: 40,
  row: 40,
};

/** Minimum touch targets in px. Web: `h-tap-sm`, `min-h-tap`, `size-tap-lg`, `size-tap-xl`. */
export const tap = { sm: 40, base: 48, lg: 56, xl: 64 } as const;
export type TapName = keyof typeof tap;

/** Max content widths in px. Web: `max-w-content` (72rem), `max-w-form` (36rem). */
export const containers = { content: 1152, form: 576 } as const;

/** Tailwind's default breakpoints, in px. */
export const BREAKPOINTS = { sm: 640, md: 768, lg: 1024, xl: 1280, "2xl": 1536 } as const;

/** Stacking order. Web: `z-(--app-z-modal)`. */
export const zIndex = {
  base: 0,
  raised: 10,
  sticky: 100,
  dropdown: 1000,
  overlay: 1100,
  modal: 1200,
  popover: 1300,
  toast: 1400,
  tooltip: 1500,
} as const;

/** Material 3 window size classes. */
export type WindowClass = "compact" | "medium" | "expanded";

export interface WindowLayout {
  windowClass: WindowClass;
  /** Screen side padding. */
  gutter: number;
  /** Between sections. */
  section: number;
  /** Card padding. */
  card: number;
  /** Max width for reading content; center it on wider screens. */
  maxContent: number;
}

/** Native layout values for a window width in dp (compact < 600 ≤ medium < 840 ≤ expanded). */
export function layoutFor(width: number): WindowLayout {
  if (width >= 840) return { windowClass: "expanded", gutter: 24, section: 48, card: 24, maxContent: 720 };
  if (width >= 600) return { windowClass: "medium", gutter: 24, section: 40, card: 24, maxContent: 720 };
  return { windowClass: "compact", gutter: 16, section: 32, card: 16, maxContent: 720 };
}
