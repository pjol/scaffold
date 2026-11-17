/**
 * Swatches: a fixed set of nine hues for content people colour themselves (Notion-style text colours and
 * highlights in docs, labels and tags). Palette-independent, so a page coloured red stays red after a restyle; each
 * hue has a text colour (`fg`, ≥ 4.5 on every surface) and a background (`bg`, body text ≥ 7 on it, `fg` ≥ 4.5 on
 * it) per mode. `pnpm check` audits them against every palette.
 *
 * CSS: `--app-swatch-<hue>` and `--app-swatch-<hue>-bg`; Tailwind: `text-swatch-red`, `bg-swatch-red-bg`.
 * Native: `theme.swatches.red.fg`.
 */
import type { Color, ColorMode } from "./types";

export type SwatchHue = "gray" | "brown" | "orange" | "yellow" | "green" | "blue" | "purple" | "pink" | "red";
export const SWATCH_HUES: readonly SwatchHue[] = ["gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"];

export interface Swatch {
  /** Text in this hue. */
  fg: Color;
  /** A highlight or block background in this hue (default text on top). */
  bg: Color;
}

export const SWATCHES: Record<SwatchHue, Record<ColorMode, Swatch>> = {
  gray: { light: { fg: "#6B6862", bg: "#F0EEEA" }, dark: { fg: "#A39E96", bg: "#2E2B27" } },
  brown: { light: { fg: "#895A43", bg: "#F5EDE8" }, dark: { fg: "#C49A84", bg: "#3D2B22" } },
  orange: { light: { fg: "#A24E0C", bg: "#FBEBDC" }, dark: { fg: "#E59A62", bg: "#47301D" } },
  yellow: { light: { fg: "#80600A", bg: "#FAF2D7" }, dark: { fg: "#D6B356", bg: "#423719" } },
  green: { light: { fg: "#2D6E4C", bg: "#E8F2EB" }, dark: { fg: "#6DBB8E", bg: "#1F3A2B" } },
  blue: { light: { fg: "#2563A0", bg: "#E5F0F9" }, dark: { fg: "#6EAEE6", bg: "#1C3247" } },
  purple: { light: { fg: "#7247A1", bg: "#F2ECF8" }, dark: { fg: "#B394E0", bg: "#33264A" } },
  pink: { light: { fg: "#A13A73", bg: "#F9ECF3" }, dark: { fg: "#E188B8", bg: "#46223A" } },
  red: { light: { fg: "#B0352F", bg: "#FCEBEA" }, dark: { fg: "#F08A80", bg: "#4A2220" } },
};

/** The swatches for one mode, by hue. */
export function swatchesFor(mode: ColorMode): Record<SwatchHue, Swatch> {
  const out = {} as Record<SwatchHue, Swatch>;
  for (const h of SWATCH_HUES) out[h] = SWATCHES[h][mode];
  return out;
}
