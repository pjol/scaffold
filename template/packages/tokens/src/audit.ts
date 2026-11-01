/**
 * Programmatic audits of a palette: WCAG contrast for every pairing the UI uses, and
 * the 4px rhythm. `pnpm check` fails on any finding.
 */
import { contrastRatio } from "./color";
import { LAYOUT_SPACE, COMPACT_SIZE, SIZE_RESPONSIVE, size, tap } from "./layout";
import { INTENTS, TYPE_ROLES, type ColorMode, type ColorTokens, type Palette } from "./types";

export interface ContrastCheck {
  mode: ColorMode;
  /** Human label, e.g. "primaryFg on primary". */
  label: string;
  fg: string;
  bg: string;
  ratio: number;
  /** Required minimum: 7 (body text), 4.5 (text), 3 (placeholders, UI boundaries). */
  min: number;
  pass: boolean;
}

const SURFACES = ["canvas", "surface", "surfaceRaised", "sunken"] as const;

function checksFor(c: ColorTokens, mode: ColorMode): Omit<ContrastCheck, "ratio" | "pass">[] {
  const out: Omit<ContrastCheck, "ratio" | "pass">[] = [];
  const add = (label: string, fg: string, bg: string, min: number) => out.push({ mode, label, fg, bg, min });

  for (const s of SURFACES) {
    add(`text on ${s}`, c.text, c[s], 7);
    add(`textMuted on ${s}`, c.textMuted, c[s], 4.5);
  }
  add("textSubtle on surface", c.textSubtle, c.surface, 3);
  add("textSubtle on canvas", c.textSubtle, c.canvas, 3);
  add("textInverse on inverse", c.textInverse, c.inverse, 7);

  for (const i of INTENTS) {
    add(`${i}Fg on ${i}`, c[`${i}Fg`], c[i], 4.5);
    add(`${i}Strong on ${i}Soft`, c[`${i}Strong`], c[`${i}Soft`], 4.5);
    add(`${i}Strong on surface`, c[`${i}Strong`], c.surface, 4.5);
    add(`${i}Strong on canvas`, c[`${i}Strong`], c.canvas, 4.5);
    add(`text on ${i}Soft`, c.text, c[`${i}Soft`], 7);
  }

  add("borderStrong on surface", c.borderStrong, c.surface, 3);
  add("borderStrong on canvas", c.borderStrong, c.canvas, 3);
  add("ring on canvas", c.ring, c.canvas, 3);
  add("ring on surface", c.ring, c.surface, 3);
  return out;
}

/** Every contrast check for both modes. */
export function auditPalette(palette: Palette): ContrastCheck[] {
  return (["light", "dark"] as const).flatMap((mode) =>
    checksFor(palette.colors[mode], mode).map((ch) => {
      const ratio = contrastRatio(ch.fg, ch.bg);
      return { ...ch, ratio, pass: ratio >= ch.min };
    }),
  );
}

/** Only the failures (empty = compliant). */
export function contrastFailures(palette: Palette): ContrastCheck[] {
  return auditPalette(palette).filter((c) => !c.pass);
}

/** Values that break the 4px rhythm (line heights, spacing, sizes). Empty = clean. */
export function rhythmIssues(palette: Palette): string[] {
  const issues: string[] = [];
  const onGrid = (label: string, px: number) => {
    if (px % 4 !== 0) issues.push(`${label} = ${px}px is not a multiple of 4`);
  };
  for (const role of TYPE_ROLES) onGrid(`type.${role}.lineHeight`, palette.type[role].lineHeight);
  for (const [name, values] of Object.entries(LAYOUT_SPACE)) values.forEach((v, i) => onGrid(`space.${name}[${i}]`, v));
  for (const [name, v] of Object.entries(size)) onGrid(`size.${name}`, v);
  for (const [name, values] of Object.entries(SIZE_RESPONSIVE)) (values ?? []).forEach((v, i) => onGrid(`size.${name}[${i}]`, v));
  for (const [name, v] of Object.entries(COMPACT_SIZE)) onGrid(`compact.${name}`, v ?? 0);
  for (const [name, v] of Object.entries(tap)) onGrid(`tap.${name}`, v);
  if (tap.base < 48) issues.push(`tap.base = ${tap.base}px is under the 48px minimum touch target`);
  return issues;
}
