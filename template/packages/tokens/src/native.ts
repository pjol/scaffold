/**
 * React Native theme from a palette. Pure data (no react-native import): the mobile app
 * memoizes `createNativeTheme(activePaletteId(), colorScheme)` in its ThemeProvider.
 */
import { size, space, tap } from "./layout";
import { reanimatedSpring } from "./motion";
import { activePaletteId, getPalette } from "./registry";
import { TYPE_ROLES, type ColorMode, type ColorTokens, type Elevation, type FontRole, type FontWeight, type Fonts, type MotionTokens, type Radii, type SpringName, type TypeRole } from "./types";

/** The subset of React Native's `TextStyle` a type role sets. */
export interface NativeTextStyle {
  fontFamily: string;
  fontSize: number;
  lineHeight: number;
  /** px (React Native has no em). */
  letterSpacing: number;
}

export interface NativeTheme {
  paletteId: string;
  mode: ColorMode;
  colors: ColorTokens;
  /** Spacing steps and semantic gaps in dp: `space[4]` = 16, `space.related` = 8. */
  space: typeof space;
  /** Component sizes in dp (`size.control` = 40). */
  size: typeof size;
  /** Minimum touch targets in dp (`tap.base` = 48). */
  tap: typeof tap;
  /** Ready-to-spread text styles; `fontFamily` is the loaded face for the role's weight. */
  type: Record<TypeRole, NativeTextStyle>;
  radius: Radii;
  /** `boxShadow` strings (React Native ≥ 0.76). */
  elevation: Elevation;
  motion: {
    /** Reanimated `withSpring` configs, `mass` always set. */
    spring: Record<SpringName, { stiffness: number; damping: number; mass: number }>;
    duration: MotionTokens["duration"];
    easing: MotionTokens["easing"];
    stagger: number;
    pressScale: number;
  };
  fonts: Fonts;
  /** Face for a role and weight, e.g. `font("body", 600)` → `"Inter_600SemiBold"`. */
  font: (role: FontRole, weight: FontWeight) => string;
}

/** Nearest shipped face for a weight (fonts only ship the weights the UI uses). */
function pickFace(map: Partial<Record<FontWeight, string>>, weight: FontWeight, fallback: string): string {
  const exact = map[weight];
  if (exact) return exact;
  const nearest = (Object.keys(map).map(Number) as FontWeight[]).sort((a, b) => Math.abs(a - weight) - Math.abs(b - weight))[0];
  return (nearest !== undefined && map[nearest]) || fallback;
}

/** Build the native theme for a palette and mode. Pure; memoize it in the provider. */
export function createNativeTheme(paletteId: string = activePaletteId(), mode: ColorMode = "light"): NativeTheme {
  const palette = getPalette(paletteId);
  const fonts = palette.fonts;
  const font = (role: FontRole, weight: FontWeight) => pickFace(fonts[role].native, weight, fonts[role].name);

  const type = {} as Record<TypeRole, NativeTextStyle>;
  for (const role of TYPE_ROLES) {
    const s = palette.type[role];
    type[role] = {
      fontFamily: font(s.family, s.weight),
      fontSize: s.size,
      lineHeight: s.lineHeight,
      letterSpacing: Math.round(s.letterSpacing * s.size * 100) / 100,
    };
  }

  const m = palette.motion;
  const spring = {} as NativeTheme["motion"]["spring"];
  for (const [name, token] of Object.entries(m.spring) as [SpringName, MotionTokens["spring"][SpringName]][]) spring[name] = reanimatedSpring(token);

  return {
    paletteId: palette.id,
    mode,
    colors: palette.colors[mode],
    space,
    size,
    tap,
    type,
    radius: palette.radius,
    elevation: palette.elevation[mode],
    motion: { spring, duration: m.duration, easing: m.easing, stagger: m.stagger, pressScale: m.pressScale },
    fonts,
    font,
  };
}
