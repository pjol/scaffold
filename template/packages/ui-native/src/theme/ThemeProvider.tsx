import { createNativeTheme, type NativeTheme } from "@scaffoldapp/tokens/native";
import { activePaletteId } from "@scaffoldapp/tokens/registry";
import { layoutFor, type WindowLayout } from "@scaffoldapp/tokens/layout";
import type { ColorMode } from "@scaffoldapp/tokens";
import { createContext, useContext, useMemo, type ReactNode } from "react";
import { useColorScheme, useWindowDimensions } from "react-native";

/** What the user picked in Settings → Appearance. `system` follows the OS. */
export type ColorModePreference = "system" | ColorMode;

const ThemeContext = createContext<NativeTheme | null>(null);

export interface ThemeProviderProps {
  children: ReactNode;
  /** Palette id from `@scaffoldapp/tokens`. @default activePaletteId() (EXPO_PUBLIC_PALETTE) */
  paletteId?: string | undefined;
  /** @default "system" */
  mode?: ColorModePreference | undefined;
}

/**
 * Tokens → React Native theme for every component below. Light/dark follows the OS
 * unless `mode` forces one (pair it with `Appearance.setColorScheme` so native chrome
 * such as alerts and the keyboard follows too).
 */
export function ThemeProvider({ children, paletteId, mode = "system" }: ThemeProviderProps) {
  const scheme = useColorScheme();
  const resolved: ColorMode = mode === "system" ? (scheme === "dark" ? "dark" : "light") : mode;
  const id = paletteId ?? activePaletteId();
  const theme = useMemo(() => createNativeTheme(id, resolved), [id, resolved]);
  return <ThemeContext.Provider value={theme}>{children}</ThemeContext.Provider>;
}

let fallback: NativeTheme | null = null;

/** The current theme (outside a provider: the build's palette in light mode). */
export function useTheme(): NativeTheme {
  const ctx = useContext(ThemeContext);
  if (ctx) return ctx;
  fallback ??= createNativeTheme(activePaletteId(), "light");
  return fallback;
}

/** Gutter, section gap, card padding and max content width for the current window. */
export function useLayout(): WindowLayout {
  const { width } = useWindowDimensions();
  return useMemo(() => layoutFor(width), [width]);
}

export type { NativeTheme };
