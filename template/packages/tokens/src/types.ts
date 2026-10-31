/**
 * @scaffoldapp/tokens — type definitions.
 *
 * A {@link Palette} is pure data: every color, font, radius, shadow and motion value the
 * UI needs. Nothing here depends on React, the DOM or React Native, so the same object
 * drives the web (CSS variables → Tailwind) and native (a JS theme object).
 */

export type ColorMode = "light" | "dark";
export const COLOR_MODES: readonly ColorMode[] = ["light", "dark"];

/** A CSS/React Native color string. Hex (`#RRGGBB`) unless noted (overlays use rgba). */
export type Color = string;

/** Neutral scale, tinted per palette. 50 = lightest, 950 = darkest (same in both modes). */
export interface InkScale {
  50: Color;
  100: Color;
  200: Color;
  300: Color;
  400: Color;
  500: Color;
  600: Color;
  700: Color;
  800: Color;
  900: Color;
  950: Color;
}
export type InkStep = keyof InkScale;
export const INK_STEPS: readonly InkStep[] = [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950];

export type Intent = "primary" | "accent" | "success" | "warning" | "danger" | "info";
export const INTENTS: readonly Intent[] = ["primary", "accent", "success", "warning", "danger", "info"];

/**
 * Semantic color tokens for one mode.
 *
 * Every intent (`primary`, `accent`, `success`, `warning`, `danger`, `info`) has:
 * - `x`        solid fill (buttons, chips, badges)
 * - `xFg`      text/icon on top of `x`                                   (≥ 4.5)
 * - `xSoft`    tinted background
 * - `xStrong`  text/icon in the intent color on `xSoft`, `surface`, `canvas` (≥ 4.5)
 */
export interface ColorTokens {
  /** App background. */
  canvas: Color;
  /** Cards, sheets, inputs. */
  surface: Color;
  /** Popovers, menus, raised cards. */
  surfaceRaised: Color;
  /** Wells, tracks, skeleton base, pressed rows. */
  sunken: Color;
  /** Modal scrim (rgba). */
  overlay: Color;
  /** Hairline dividers and card outlines (decorative). */
  border: Color;
  /** Input outlines and essential boundaries (≥ 3 on surface). */
  borderStrong: Color;
  /** Focus ring (≥ 3 on canvas and surface). */
  ring: Color;

  /** Primary text (≥ 7 on every surface). */
  text: Color;
  /** Secondary text: metadata, inactive tabs (≥ 4.5 on every surface). */
  textMuted: Color;
  /** Placeholders, disabled labels (≥ 3 on surface). Never for essential information. */
  textSubtle: Color;
  /** Text on `inverse` (tooltips, snackbars). */
  textInverse: Color;
  /** Background for inverted elements. */
  inverse: Color;

  primary: Color;
  primaryFg: Color;
  primarySoft: Color;
  primaryStrong: Color;

  accent: Color;
  accentFg: Color;
  accentSoft: Color;
  accentStrong: Color;

  success: Color;
  successFg: Color;
  successSoft: Color;
  successStrong: Color;

  warning: Color;
  warningFg: Color;
  warningSoft: Color;
  warningStrong: Color;

  danger: Color;
  dangerFg: Color;
  dangerSoft: Color;
  dangerStrong: Color;

  info: Color;
  infoFg: Color;
  infoSoft: Color;
  infoStrong: Color;

  ink: InkScale;
}

/** The flat (string) color keys, for typed lookups. */
export type FlatColorKey = {
  [K in keyof ColorTokens]: ColorTokens[K] extends string ? K : never;
}[keyof ColorTokens];

// ───────────────────────────────────────────────────────── typography ──

export type FontWeight = 400 | 500 | 600 | 700 | 800 | 900;

export interface FontFamilyToken {
  /** CSS family name as served by Google Fonts / next/font, e.g. "Inter". */
  name: string;
  /** Google Fonts family id. */
  google: string;
  /** Weights the UI uses. Load exactly these. */
  weights: FontWeight[];
  /** Variable-font axes worth loading on the web (next/font `axes`). */
  axes?: string[];
  /** CSS fallback stack appended after the family. */
  fallback: string;
  /** React Native face per weight, matching `@expo-google-fonts/*` export names. */
  native: Partial<Record<FontWeight, string>>;
  /** `@expo-google-fonts/<pkg>` package (or the bundled asset path) for this family. */
  expoPackage: string;
}

/** Which family a type role uses. */
export type FontRole = "display" | "body";

export type TypeRole = "caption" | "label" | "body" | "bodyLg" | "titleSm" | "title" | "headline" | "display" | "hero";
/** Roles from smallest to largest. */
export const TYPE_ROLES: readonly TypeRole[] = ["caption", "label", "body", "bodyLg", "titleSm", "title", "headline", "display", "hero"];

export interface TypeStyle {
  family: FontRole;
  /** px (web) / dp (native). */
  size: number;
  /** px / dp. Always a multiple of 4. */
  lineHeight: number;
  weight: FontWeight;
  /** em. Converted to px on native (`size * letterSpacing`). */
  letterSpacing: number;
}

export type TypeScale = Record<TypeRole, TypeStyle>;

export interface Fonts {
  display: FontFamilyToken;
  body: FontFamilyToken;
}

// ───────────────────────────────────────────────────────── shape / depth ──

export interface Radii {
  xs: number;
  sm: number;
  md: number;
  lg: number;
  xl: number;
  "2xl": number;
  pill: number;
}

export type ElevationLevel = "none" | "xs" | "sm" | "md" | "lg" | "xl";

/**
 * Shadows as CSS `box-shadow` strings. React Native ≥ 0.76 accepts the same strings
 * through the `boxShadow` style prop, so both platforms share them.
 */
export type Elevation = Record<ElevationLevel, string>;

// ───────────────────────────────────────────────────────── motion ──

/**
 * Physical spring. Identical numbers give identical motion in `motion` (web) and
 * Reanimated (native). Always pass `mass`: Reanimated 4 defaults it to 4.
 */
export interface SpringToken {
  stiffness: number;
  damping: number;
  mass: number;
}

/** Cubic-bezier control points `[x1, y1, x2, y2]`. */
export type BezierToken = readonly [number, number, number, number];

export type SpringName = "snappy" | "gentle" | "bouncy" | "sheet" | "press";

export interface MotionTokens {
  spring: {
    /** Buttons, toggles, indicators: fast, almost no overshoot. */
    snappy: SpringToken;
    /** Cards, page content, layout shifts: soft and settled. */
    gentle: SpringToken;
    /** Success moments and badges popping in: visible overshoot. */
    bouncy: SpringToken;
    /** Bottom sheets and drawers. */
    sheet: SpringToken;
    /** Press-down feedback. */
    press: SpringToken;
  };
  /** Milliseconds. */
  duration: {
    instant: number;
    fast: number;
    base: number;
    slow: number;
    slower: number;
    /** Skeleton shimmer cycle. */
    shimmer: number;
  };
  easing: {
    standard: BezierToken;
    out: BezierToken;
    in: BezierToken;
    inOut: BezierToken;
    emphasized: BezierToken;
  };
  /** Delay between staggered list children, ms. */
  stagger: number;
  /** Scale applied while pressing a tappable surface. */
  pressScale: number;
}

// ───────────────────────────────────────────────────────── palette ──

export interface Palette {
  /** Registry key (`NEXT_PUBLIC_PALETTE` / `EXPO_PUBLIC_PALETTE`). */
  id: string;
  /** Display name for docs and switchers. Never shown as the product name. */
  name: string;
  /** Why the palette works (docs). */
  rationale: string;
  colors: Record<ColorMode, ColorTokens>;
  fonts: Fonts;
  type: TypeScale;
  radius: Radii;
  elevation: Record<ColorMode, Elevation>;
  motion: MotionTokens;
}
