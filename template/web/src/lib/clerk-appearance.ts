import type { ColorMode, Palette } from "@scaffoldapp/tokens";

/**
 * Clerk's prebuilt UI themed from the palette, so sign-in matches the app in light and
 * dark. Values come from tokens (never literals); fonts are the app's CSS variables.
 */
export function clerkAppearance(palette: Palette, mode: ColorMode) {
  const c = palette.colors[mode];
  return {
    variables: {
      colorPrimary: c.primary,
      colorPrimaryForeground: c.primaryFg,
      colorDanger: c.danger,
      colorSuccess: c.success,
      colorWarning: c.warning,
      colorNeutral: mode === "light" ? c.ink[950] : c.ink[50],
      colorForeground: c.text,
      colorMutedForeground: c.textMuted,
      colorMuted: c.sunken,
      colorBackground: c.surface,
      colorInput: c.surface,
      colorInputForeground: c.text,
      colorBorder: c.borderStrong,
      colorRing: c.ring,
      colorShadow: c.ink[950],
      colorModalBackdrop: c.ink[950],
      fontFamily: "var(--app-font-body)",
      fontFamilyButtons: "var(--app-font-body)",
      fontSize: "1rem",
      borderRadius: `${palette.radius.md}px`,
    },
    options: {
      logoPlacement: "none" as const,
      socialButtonsVariant: "blockButton" as const,
    },
    elements: {
      rootBox: "w-full",
      cardBox: "w-full max-w-md shadow-none border border-border rounded-xl",
    },
  };
}
