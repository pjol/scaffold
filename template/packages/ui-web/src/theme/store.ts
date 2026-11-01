"use client";

import { activePaletteId, DEFAULT_PALETTE_ID, getPalette, isPaletteId, PALETTE_IDS, type ColorMode, type ColorTokens, type Palette, type PaletteId } from "@scaffoldapp/tokens";
import { useCallback, useSyncExternalStore } from "react";
import { PALETTE_STORAGE_KEY, THEME_STORAGE_KEY } from "./script";

// One MutationObserver and one matchMedia listener for the whole page, shared by every
// hook below, so a screen full of buttons doesn't create an observer each.
const listeners = new Set<() => void>();
let teardown: (() => void) | null = null;

function notify() {
  for (const l of [...listeners]) l();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  if (!teardown && typeof window !== "undefined") {
    const mql = window.matchMedia?.("(prefers-color-scheme: dark)");
    mql?.addEventListener("change", notify);
    const mo = new MutationObserver(notify);
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme", "data-palette"] });
    teardown = () => {
      mql?.removeEventListener("change", notify);
      mo.disconnect();
    };
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0 && teardown) {
      teardown();
      teardown = null;
    }
  };
}

const readThemeAttr = (): ColorMode | null => {
  const v = document.documentElement.getAttribute("data-theme");
  return v === "light" || v === "dark" ? v : null;
};
const readSystemDark = (): boolean => window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false;
const readPaletteAttr = (): string | null => document.documentElement.getAttribute("data-palette");
const serverNull = (): null => null;
const serverFalse = (): boolean => false;

function store(key: string, value: string | null) {
  try {
    if (value === null) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, value);
  } catch {
    // Private mode or storage disabled: the choice lasts for this page only.
  }
}

/** What the user picked: follow the system, or force a mode. */
export type ThemePreference = ColorMode | "system";

export interface ColorModeState {
  /** The mode on screen now. SSR renders `light`. */
  mode: ColorMode;
  /** The stored choice. */
  preference: ThemePreference;
  setPreference: (next: ThemePreference) => void;
}

/** The resolved colour mode (`data-theme` on `<html>`, else the OS) and the user's choice. */
export function useColorMode(): ColorModeState {
  const attr = useSyncExternalStore(subscribe, readThemeAttr, serverNull);
  const systemDark = useSyncExternalStore(subscribe, readSystemDark, serverFalse);
  const setPreference = useCallback((next: ThemePreference) => {
    const el = document.documentElement;
    if (next === "system") {
      store(THEME_STORAGE_KEY, null);
      el.removeAttribute("data-theme");
    } else {
      store(THEME_STORAGE_KEY, next);
      el.setAttribute("data-theme", next);
    }
  }, []);
  return { mode: attr ?? (systemDark ? "dark" : "light"), preference: attr ?? "system", setPreference };
}

export interface PaletteState {
  id: PaletteId;
  palette: Palette;
  /** Every shipped palette id, in display order. Show a picker only when there is more than one. */
  ids: readonly PaletteId[];
  /** Switch palette (stored). `null` returns to the build's palette. */
  setPalette: (id: PaletteId | null) => void;
}

/** The palette on screen (`data-palette`, else the build's `NEXT_PUBLIC_PALETTE`). */
export function usePalette(): PaletteState {
  const attr = useSyncExternalStore(subscribe, readPaletteAttr, serverNull);
  const id = isPaletteId(attr) ? attr : activePaletteId();
  const setPalette = useCallback((next: PaletteId | null) => {
    const el = document.documentElement;
    store(PALETTE_STORAGE_KEY, next);
    const target = next ?? activePaletteId();
    if (target === DEFAULT_PALETTE_ID && next === null) el.removeAttribute("data-palette");
    else el.setAttribute("data-palette", target);
  }, []);
  return { id, palette: getPalette(id), ids: PALETTE_IDS, setPalette };
}

/** Concrete colour values for the current palette and mode, for code that can't use CSS variables (Clerk, canvas, charts). */
export function useThemeColors(): ColorTokens {
  const { palette } = usePalette();
  const { mode } = useColorMode();
  return palette.colors[mode];
}
