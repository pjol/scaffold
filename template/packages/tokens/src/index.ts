/**
 * @scaffoldapp/tokens: design tokens for every client. Pure data, consumed as TS source.
 *
 * ```ts
 * import { getPalette, space, layoutFor } from "@scaffoldapp/tokens";
 * const palette = getPalette();          // NEXT_PUBLIC_PALETTE / EXPO_PUBLIC_PALETTE, default "base"
 * palette.colors.light.primary;          // "#4338CA"
 * palette.motion.spring.snappy;          // { stiffness, damping, mass }
 * ```
 *
 * Web: `@import "@scaffoldapp/tokens/theme.css"` after Tailwind. Native: `@scaffoldapp/tokens/native`.
 */
export * from "./types";
export * from "./color";
export * from "./motion";
export * from "./layout";
export * from "./presets";
export * from "./audit";
export * from "./css";
export * from "./native";
export { PALETTES, PALETTE_IDS, DEFAULT_PALETTE_ID, activePaletteId, getPalette, isPaletteId, type PaletteId } from "./registry";
