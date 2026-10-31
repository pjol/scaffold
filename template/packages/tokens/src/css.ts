/**
 * Palettes → CSS custom properties. `scripts/tokens.mjs` writes the result to
 * `src/generated/tokens.css`; `theme.css` maps it onto Tailwind. Framework-free, so
 * any HTML surface (emails, static pages) can use it too.
 *
 * Every variable is prefixed `--app-`. Colors and shadows change per mode; fonts, type,
 * radii and motion per palette; spacing, sizes and z-index are global.
 */
import { COMPACT_SIZE, LAYOUT_BREAKPOINTS, LAYOUT_SPACE, SIZE_RESPONSIVE, containers, size, tap, zIndex, type SizeName } from "./layout";
import { cubicBezierCss, springToCssLinear } from "./motion";
import { DEFAULT_PALETTE_ID, PALETTE_IDS, PALETTES } from "./registry";
import { TYPE_ROLES, type ColorMode, type Palette } from "./types";

export const CSS_VAR_PREFIX = "--app-";

const kebab = (s: string) =>
  s
    .replace(/_/g, "-")
    .replace(/([a-z0-9])([A-Z])/g, "$1-$2")
    .toLowerCase();

/** `"primarySoft"` → `"--app-primary-soft"`; `"type.bodyLg.size"` → `"--app-type-body-lg-size"`. */
export function cssVarName(path: string): string {
  return CSS_VAR_PREFIX + path.split(".").map(kebab).join("-");
}

/** `var(--app-…)` reference for a token path. */
export function cssVar(path: string, fallback?: string): string {
  return fallback ? `var(${cssVarName(path)}, ${fallback})` : `var(${cssVarName(path)})`;
}

/** `16` → `"1rem"`; `6` → `"0.375rem"`. */
export function rem(px: number): string {
  return `${Math.round((px / 16) * 10000) / 10000}rem`;
}

/** Color + shadow variables for one mode. */
export function paletteColorVars(palette: Palette, mode: ColorMode): Record<string, string> {
  const c = palette.colors[mode];
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(c)) {
    if (typeof value === "string") out[cssVarName(key)] = value;
  }
  for (const [step, value] of Object.entries(c.ink)) out[cssVarName(`ink.${step}`)] = value;
  for (const [level, shadow] of Object.entries(palette.elevation[mode])) out[cssVarName(`shadow.${level}`)] = shadow;
  return out;
}

export interface StaticVarOptions {
  /**
   * Custom properties holding the loaded font families (next/font's `variable`). When
   * unset at runtime the plain family name is used.
   * @default { display: "--app-face-display", body: "--app-face-body" }
   */
  fontVars?: { display: string; body: string };
}

/** Mode-independent palette variables: fonts, type scale, radii, motion. */
export function paletteStaticVars(palette: Palette, opts: StaticVarOptions = {}): Record<string, string> {
  const fv = opts.fontVars ?? { display: "--app-face-display", body: "--app-face-body" };
  const f = palette.fonts;
  const out: Record<string, string> = {
    [cssVarName("font.display")]: `var(${fv.display}, "${f.display.name}"), ${f.display.fallback}`,
    [cssVarName("font.body")]: `var(${fv.body}, "${f.body.name}"), ${f.body.fallback}`,
  };
  for (const role of TYPE_ROLES) {
    const s = palette.type[role];
    out[cssVarName(`type.${role}.size`)] = rem(s.size);
    out[cssVarName(`type.${role}.line`)] = rem(s.lineHeight);
    out[cssVarName(`type.${role}.weight`)] = String(s.weight);
    out[cssVarName(`type.${role}.tracking`)] = `${s.letterSpacing}em`;
    out[cssVarName(`type.${role}.family`)] = `var(${cssVarName(`font.${s.family}`)})`;
  }
  // hero grows from the display size at 375px wide to its own size at 1024px.
  const lo = palette.type.display.size;
  const hi = palette.type.hero.size;
  const slope = (hi - lo) / (1024 - 375);
  const intercept = lo - slope * 375;
  out[cssVarName("type.hero.fluid")] = `clamp(${rem(lo)}, ${rem(intercept)} + ${Math.round(slope * 10000) / 100}vw, ${rem(hi)})`;

  for (const [k, v] of Object.entries(palette.radius)) out[cssVarName(`radius.${k}`)] = k === "pill" ? "9999px" : rem(v);
  const m = palette.motion;
  for (const [k, v] of Object.entries(m.duration)) out[cssVarName(`duration.${k}`)] = `${v}ms`;
  for (const [k, v] of Object.entries(m.easing)) out[cssVarName(`ease.${k}`)] = cubicBezierCss(v);
  for (const [k, v] of Object.entries(m.spring)) {
    const { easing, duration } = springToCssLinear(v);
    out[cssVarName(`spring.${k}`)] = easing;
    out[cssVarName(`spring.${k}.duration`)] = `${duration}ms`;
  }
  out[cssVarName("stagger")] = `${m.stagger}ms`;
  out[cssVarName("press.scale")] = String(m.pressScale);
  return out;
}

const sizeVar = (name: SizeName) => cssVarName(`size.${name}`);
const tapVar = (name: keyof typeof tap) => (name === "base" ? cssVarName("tap") : cssVarName(`tap.${name}`));

/** Global layout variables: base values plus the overrides at each layout breakpoint. */
export function layoutVars(): { base: Record<string, string>; sm: Record<string, string>; lg: Record<string, string> } {
  const base: Record<string, string> = {};
  const sm: Record<string, string> = {};
  const lg: Record<string, string> = {};
  for (const [name, values] of Object.entries(LAYOUT_SPACE)) {
    const v = values as readonly number[];
    const key = cssVarName(`space.${name}`);
    base[key] = rem(v[0] ?? 0);
    if (v[1] !== undefined) sm[key] = rem(v[1]);
    if (v[2] !== undefined) lg[key] = rem(v[2]);
  }
  for (const [name, v] of Object.entries(size) as [SizeName, number][]) base[sizeVar(name)] = rem(v);
  for (const [name, values] of Object.entries(SIZE_RESPONSIVE) as [SizeName, readonly number[]][]) {
    if (values[1] !== undefined) sm[sizeVar(name)] = rem(values[1]);
    if (values[2] !== undefined) lg[sizeVar(name)] = rem(values[2]);
  }
  for (const [name, v] of Object.entries(tap) as [keyof typeof tap, number][]) base[tapVar(name)] = rem(v);
  for (const [name, v] of Object.entries(containers)) base[cssVarName(`container.${name}`)] = rem(v);
  for (const [name, v] of Object.entries(zIndex)) base[cssVarName(`z.${name}`)] = String(v);
  return { base, sm, lg };
}

/** Size overrides for `[data-density=compact]`, and the comfortable values that undo them. */
export function densityVars(): { compact: Record<string, string>; comfortable: Record<string, string> } {
  const compact: Record<string, string> = {};
  const comfortable: Record<string, string> = {};
  for (const [name, v] of Object.entries(COMPACT_SIZE) as [SizeName, number][]) {
    compact[sizeVar(name)] = rem(v);
    comfortable[sizeVar(name)] = rem(size[name]);
  }
  return { compact, comfortable };
}

const decl = (vars: Record<string, string>, indent = "  ") =>
  Object.entries(vars)
    .map(([k, v]) => `${indent}${k}: ${v};`)
    .join("\n");

const rule = (selector: string, vars: Record<string, string>, extra?: string) =>
  `${selector} {\n${extra ? `  ${extra}\n` : ""}${decl(vars)}\n}`;

const media = (query: string, body: string) => `@media ${query} {\n${body.replace(/^/gm, "  ")}\n}`;

/** One palette's rules: light on its selector, dark via the OS setting and `data-theme`. */
function paletteRules(palette: Palette, isDefault: boolean, opts: StaticVarOptions): string {
  const id = palette.id;
  const p = `[data-palette="${id}"]`;
  const light = paletteColorVars(palette, "light");
  const dark = paletteColorVars(palette, "dark");
  const either = (rootSel: string, paletteSel: string) => (isDefault ? `${rootSel},\n${paletteSel}` : paletteSel);
  return [
    `/* ── ${palette.name} ── */`,
    rule(either(":root", p), { ...paletteStaticVars(palette, opts), ...light }, "color-scheme: light;"),
    media(
      "(prefers-color-scheme: dark)",
      rule(either(':root:not([data-theme="light"])', `${p}:not([data-theme="light"])`), dark, "color-scheme: dark;"),
    ),
    rule(either('[data-theme="dark"]', `${p}[data-theme="dark"],\n${p} [data-theme="dark"]`), dark, "color-scheme: dark;"),
    rule(either('[data-theme="light"]', `${p}[data-theme="light"],\n${p} [data-theme="light"]`), light, "color-scheme: light;"),
  ].join("\n");
}

/**
 * The complete generated stylesheet: global layout variables, then every palette under
 * `[data-palette=<id>]` with the default palette also on `:root`.
 */
export function tokensStylesheet(opts: StaticVarOptions = {}): string {
  const layout = layoutVars();
  const density = densityVars();
  const parts = [
    "/* GENERATED by packages/tokens/scripts/tokens.mjs from src/. Do not edit: run `pnpm tokens`. */",
    `/* Default palette "${DEFAULT_PALETTE_ID}" on :root; set data-palette on an ancestor to switch, data-theme="light|dark" to force a mode. */`,
    "",
    "/* ── layout ── */",
    rule(":root", layout.base),
    media(`(min-width: ${LAYOUT_BREAKPOINTS.sm}px)`, rule(":root", layout.sm)),
    media(`(min-width: ${LAYOUT_BREAKPOINTS.lg}px)`, rule(":root", layout.lg)),
    rule('[data-density="compact"]', density.compact),
    media("(pointer: coarse)", rule('[data-density="compact"]', density.comfortable)),
  ];
  for (const id of PALETTE_IDS) parts.push("", paletteRules(PALETTES[id], id === DEFAULT_PALETTE_ID, opts));
  return `${parts.join("\n")}\n`;
}

/** The per-palette summary `app.config.ts` and native splash screens read. */
export interface PaletteManifestEntry {
  id: string;
  name: string;
  splash: Record<ColorMode, string>;
  primary: Record<ColorMode, string>;
  primaryFg: Record<ColorMode, string>;
}

export interface PaletteManifest {
  default: string;
  palettes: Record<string, PaletteManifestEntry>;
}

export function paletteManifest(): PaletteManifest {
  const palettes: Record<string, PaletteManifestEntry> = {};
  for (const id of PALETTE_IDS) {
    const p = PALETTES[id];
    palettes[id] = {
      id,
      name: p.name,
      splash: { light: p.colors.light.canvas, dark: p.colors.dark.canvas },
      primary: { light: p.colors.light.primary, dark: p.colors.dark.primary },
      primaryFg: { light: p.colors.light.primaryFg, dark: p.colors.dark.primaryFg },
    };
  }
  return { default: DEFAULT_PALETTE_ID, palettes };
}
