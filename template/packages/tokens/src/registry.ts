import { base } from "./palettes/base";
import type { Palette } from "./types";

// Module-scoped so it never collides with @types/node's global `process`. Next.js and
// babel-preset-expo replace the literal `process.env.NEXT_PUBLIC_PALETTE` /
// `process.env.EXPO_PUBLIC_PALETTE` expressions at build time, so they must stay spelled
// out exactly like this.
declare const process: { env: { NEXT_PUBLIC_PALETTE?: string; EXPO_PUBLIC_PALETTE?: string } };
declare const console: { warn: (...args: unknown[]) => void } | undefined;

/**
 * Every shipped palette, keyed by id, in display order. The single source of the
 * palette list: add `palettes/<id>.ts` and one entry here, then run `pnpm tokens`.
 */
export const PALETTES = { base } as const satisfies Record<string, Palette>;

export type PaletteId = keyof typeof PALETTES;

/** Palette ids in display order. */
export const PALETTE_IDS = Object.keys(PALETTES) as PaletteId[];

export const DEFAULT_PALETTE_ID: PaletteId = "base";

export function isPaletteId(value: unknown): value is PaletteId {
  return typeof value === "string" && Object.prototype.hasOwnProperty.call(PALETTES, value);
}

function readPaletteEnv(): string | undefined {
  // Two guards: on native `process.env.NEXT_PUBLIC_PALETTE` is not inlined and may
  // throw, which must not prevent reading the inlined EXPO_PUBLIC_PALETTE (and vice versa).
  let web: string | undefined;
  try {
    web = process.env.NEXT_PUBLIC_PALETTE;
  } catch {
    web = undefined;
  }
  if (web) return web;
  let native: string | undefined;
  try {
    native = process.env.EXPO_PUBLIC_PALETTE;
  } catch {
    native = undefined;
  }
  return native || undefined;
}

/**
 * The palette selected for this build: `NEXT_PUBLIC_PALETTE` (web) or
 * `EXPO_PUBLIC_PALETTE` (native), else {@link DEFAULT_PALETTE_ID}. Unknown values fall
 * back too, with a warning.
 *
 * @param override Force an id (previews, tests, switchers).
 */
export function activePaletteId(override?: string | null): PaletteId {
  const raw = (override ?? readPaletteEnv() ?? "").trim().toLowerCase();
  if (!raw) return DEFAULT_PALETTE_ID;
  if (isPaletteId(raw)) return raw;
  if (typeof console !== "undefined") {
    console.warn(`[@scaffoldapp/tokens] Unknown palette "${raw}", using "${DEFAULT_PALETTE_ID}".`);
  }
  return DEFAULT_PALETTE_ID;
}

/** A palette by id. Defaults to the active palette; unknown ids fall back. */
export function getPalette(id: string = activePaletteId()): Palette {
  return PALETTES[isPaletteId(id) ? id : activePaletteId(id)];
}
