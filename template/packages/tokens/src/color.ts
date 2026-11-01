/**
 * Tiny, dependency-free color helpers shared by web and native.
 */

export interface RGB {
  r: number;
  g: number;
  b: number;
}

/** Parse `#RGB`, `#RRGGBB` or `#RRGGBBAA` (alpha ignored) into 0–255 channels. */
export function hexToRgb(hex: string): RGB {
  let h = hex.trim().replace(/^#/, "");
  if (h.length === 3) {
    h = h
      .split("")
      .map((c) => c + c)
      .join("");
  }
  if (h.length !== 6 && h.length !== 8) {
    throw new Error(`hexToRgb: unsupported color "${hex}"`);
  }
  return {
    r: parseInt(h.slice(0, 2), 16),
    g: parseInt(h.slice(2, 4), 16),
    b: parseInt(h.slice(4, 6), 16),
  };
}

const toHex = (n: number) =>
  Math.round(Math.min(255, Math.max(0, n)))
    .toString(16)
    .padStart(2, "0");

export function rgbToHex({ r, g, b }: RGB): string {
  return `#${toHex(r)}${toHex(g)}${toHex(b)}`.toUpperCase();
}

/** `#FF5B14` + 0.25 → `rgba(255, 91, 20, 0.25)`. */
export function withAlpha(hex: string, alpha: number): string {
  const { r, g, b } = hexToRgb(hex);
  return `rgba(${r}, ${g}, ${b}, ${Math.round(alpha * 1000) / 1000})`;
}

/** Linear mix in sRGB space. `t = 0` → `a`, `t = 1` → `b`. */
export function mix(a: string, b: string, t: number): string {
  const x = hexToRgb(a);
  const y = hexToRgb(b);
  return rgbToHex({
    r: x.r + (y.r - x.r) * t,
    g: x.g + (y.g - x.g) * t,
    b: x.b + (y.b - x.b) * t,
  });
}

function channel(c: number): number {
  const s = c / 255;
  return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
}

/** WCAG 2.x relative luminance (0 = black, 1 = white). Accepts hex or `rgba()` (alpha ignored). */
export function luminance(color: string): number {
  const { r, g, b } = parseColor(color);
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

/** WCAG 2.x contrast ratio between two opaque colors, 1–21. */
export function contrastRatio(a: string, b: string): number {
  const la = luminance(a);
  const lb = luminance(b);
  const [hi, lo] = la > lb ? [la, lb] : [lb, la];
  return (hi + 0.05) / (lo + 0.05);
}

/** Round a ratio for display: 4.4999 → "4.49" (never rounds a failure up to a pass). */
export function formatRatio(ratio: number): string {
  return (Math.floor(ratio * 100) / 100).toFixed(2);
}

/** Parse hex or `rgb()/rgba()` into channels. */
export function parseColor(color: string): RGB {
  const c = color.trim();
  if (c.startsWith("#")) return hexToRgb(c);
  const m = c.match(/^rgba?\(([^)]+)\)$/i);
  if (m && m[1]) {
    const [r = 0, g = 0, b = 0] = m[1].split(",").map((p) => parseFloat(p));
    return { r, g, b };
  }
  throw new Error(`parseColor: unsupported color "${color}"`);
}

/** Is a color "light" (prefers dark text on top)? */
export function isLight(color: string): boolean {
  return contrastRatio(color, "#000000") > contrastRatio(color, "#FFFFFF");
}
