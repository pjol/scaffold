/**
 * Building blocks shared by every palette. Palettes start from these and override only
 * what makes them distinct.
 */
import { withAlpha } from "./color";
import type { ColorMode, Elevation, FontFamilyToken, MotionTokens, TypeRole, TypeScale, TypeStyle } from "./types";

/** Inter: the body face for every palette and the default display face. */
export const INTER: FontFamilyToken = {
  name: "Inter",
  google: "Inter",
  weights: [400, 500, 600, 700],
  axes: ["opsz"],
  fallback: 'ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
  native: {
    400: "Inter_400Regular",
    500: "Inter_500Medium",
    600: "Inter_600SemiBold",
    700: "Inter_700Bold",
  },
  expoPackage: "@expo-google-fonts/inter",
};

/** Base motion system; palettes may tune the springs. */
export const BASE_MOTION: MotionTokens = {
  spring: {
    snappy: { stiffness: 520, damping: 36, mass: 1 },
    gentle: { stiffness: 210, damping: 26, mass: 1 },
    bouncy: { stiffness: 420, damping: 16, mass: 1 },
    sheet: { stiffness: 360, damping: 36, mass: 1 },
    press: { stiffness: 760, damping: 34, mass: 0.7 },
  },
  duration: {
    instant: 90,
    fast: 160,
    base: 240,
    slow: 380,
    slower: 560,
    shimmer: 1400,
  },
  easing: {
    standard: [0.2, 0, 0, 1],
    out: [0.16, 1, 0.3, 1],
    in: [0.7, 0, 0.84, 0],
    inOut: [0.65, 0, 0.35, 1],
    emphasized: [0.3, 0, 0, 1],
  },
  stagger: 45,
  pressScale: 0.97,
};

/**
 * The type scale. Line heights are multiples of 4; tracking tightens as size grows.
 * `hero` scales fluidly on the web between `display` and its own size.
 */
export const BASE_TYPE: TypeScale = {
  caption: { family: "body", size: 12, lineHeight: 16, weight: 500, letterSpacing: 0.005 },
  label: { family: "body", size: 14, lineHeight: 20, weight: 500, letterSpacing: 0 },
  body: { family: "body", size: 16, lineHeight: 24, weight: 400, letterSpacing: 0 },
  bodyLg: { family: "body", size: 18, lineHeight: 28, weight: 400, letterSpacing: -0.005 },
  titleSm: { family: "body", size: 20, lineHeight: 28, weight: 600, letterSpacing: -0.01 },
  title: { family: "display", size: 24, lineHeight: 32, weight: 600, letterSpacing: -0.015 },
  headline: { family: "display", size: 30, lineHeight: 36, weight: 700, letterSpacing: -0.02 },
  display: { family: "display", size: 36, lineHeight: 40, weight: 700, letterSpacing: -0.025 },
  hero: { family: "display", size: 48, lineHeight: 52, weight: 700, letterSpacing: -0.03 },
};

/** The base scale with per-role overrides (e.g. a heavier display face). */
export function typeScale(overrides: Partial<Record<TypeRole, Partial<TypeStyle>>> = {}): TypeScale {
  const out = { ...BASE_TYPE };
  for (const role of Object.keys(overrides) as TypeRole[]) out[role] = { ...BASE_TYPE[role], ...overrides[role] };
  return out;
}

/**
 * Elevation scale. `tint` is a dark palette-tinted color so shadows match the palette
 * instead of a dead gray. Dark mode needs heavier shadows to read.
 */
export function elevation(tint: string, mode: ColorMode): Elevation {
  const a = mode === "light" ? 1 : 2.4;
  const s = (alpha: number) => withAlpha(tint, Math.min(alpha * a, 0.9));
  return {
    none: "none",
    xs: `0 1px 2px ${s(0.08)}`,
    sm: `0 1px 2px ${s(0.06)}, 0 2px 6px ${s(0.08)}`,
    md: `0 2px 4px ${s(0.06)}, 0 8px 20px ${s(0.1)}`,
    lg: `0 4px 8px ${s(0.06)}, 0 16px 36px ${s(0.14)}`,
    xl: `0 8px 16px ${s(0.08)}, 0 28px 64px ${s(0.2)}`,
  };
}
