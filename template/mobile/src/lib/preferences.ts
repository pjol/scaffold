import type { ColorModePreference } from "@scaffoldapp/ui-native";
import { activePaletteId, isPaletteId } from "@scaffoldapp/tokens/registry";
import { useCallback, useSyncExternalStore } from "react";
import { Appearance } from "react-native";
import { DEVICE_KEYS, readJson, writeJson } from "./storage";

/** Device-level display preferences from Settings → Appearance. */
export interface Preferences {
  appearance: ColorModePreference;
  /** Palette id; the build's palette (EXPO_PUBLIC_PALETTE) until the user picks another. */
  palette: string;
}

let state: Preferences = { appearance: "system", palette: activePaletteId() };
let ready = false;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

function applyAppearance(pref: ColorModePreference) {
  // Drives useColorScheme() and native chrome (alerts, keyboard, date pickers) too.
  Appearance.setColorScheme(pref === "system" ? "unspecified" : pref);
}

/** Restore saved preferences once at launch (the splash stays up until it resolves). */
export async function restorePreferences(): Promise<void> {
  const [appearance, palette] = await Promise.all([readJson<ColorModePreference>(DEVICE_KEYS.appearance), readJson<string>(DEVICE_KEYS.palette)]);
  state = {
    appearance: appearance === "light" || appearance === "dark" ? appearance : "system",
    palette: isPaletteId(palette) ? palette : activePaletteId(),
  };
  applyAppearance(state.appearance);
  ready = true;
  emit();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function usePreferences(): Preferences & {
  ready: boolean;
  setAppearance: (pref: ColorModePreference) => void;
  setPalette: (id: string) => void;
} {
  const snapshot = useSyncExternalStore(subscribe, () => state);
  const isReady = useSyncExternalStore(subscribe, () => ready);
  const setAppearance = useCallback((pref: ColorModePreference) => {
    state = { ...state, appearance: pref };
    applyAppearance(pref);
    emit();
    void writeJson(DEVICE_KEYS.appearance, pref === "system" ? null : pref);
  }, []);
  const setPalette = useCallback((id: string) => {
    if (!isPaletteId(id)) return;
    state = { ...state, palette: id };
    emit();
    void writeJson(DEVICE_KEYS.palette, id === activePaletteId() ? null : id);
  }, []);
  return { ...snapshot, ready: isReady, setAppearance, setPalette };
}
