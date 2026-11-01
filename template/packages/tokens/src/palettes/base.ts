import { BASE_MOTION, INTER, elevation, typeScale } from "../presets";
import type { ColorTokens, InkScale, Palette } from "../types";

/**
 * BASE: warm paper, near-black ink and one confident indigo. Calm by default; the
 * clay accent is for rare emphasis. The starting point the design process replaces.
 */

const ink: InkScale = {
  50: "#FAF9F6",
  100: "#F3F1EC",
  200: "#E7E3DB",
  300: "#D3CDC2",
  400: "#ABA498",
  500: "#857E72",
  600: "#665F55",
  700: "#4B463F",
  800: "#34302B",
  900: "#23201C",
  950: "#151412",
};

const light: ColorTokens = {
  canvas: "#F7F5F0",
  surface: "#FFFEFB",
  surfaceRaised: "#FFFFFF",
  sunken: "#EFEBE3",
  overlay: "rgba(28, 26, 23, 0.5)",
  border: "#E5E0D6",
  borderStrong: "#857E72",
  ring: "#4338CA",

  text: "#1C1A17",
  textMuted: "#5E584F",
  textSubtle: "#857E72",
  textInverse: "#F7F5F0",
  inverse: "#24211D",

  primary: "#4338CA",
  primaryFg: "#FFFFFF",
  primarySoft: "#EAE8FB",
  primaryStrong: "#3730A3",

  accent: "#B4532A",
  accentFg: "#FFFFFF",
  accentSoft: "#F8E9E0",
  accentStrong: "#8F3E1C",

  success: "#2F7D4F",
  successFg: "#FFFFFF",
  successSoft: "#E2F1E7",
  successStrong: "#23633D",

  warning: "#D99A1E",
  warningFg: "#1C1A17",
  warningSoft: "#FBEFD5",
  warningStrong: "#8A5A00",

  danger: "#C2352B",
  dangerFg: "#FFFFFF",
  dangerSoft: "#FBE6E3",
  dangerStrong: "#A32A21",

  info: "#1F6FB2",
  infoFg: "#FFFFFF",
  infoSoft: "#E2EEF8",
  infoStrong: "#195A91",

  ink,
};

const dark: ColorTokens = {
  canvas: "#141311",
  surface: "#1C1A17",
  surfaceRaised: "#24221E",
  sunken: "#0D0C0B",
  overlay: "rgba(0, 0, 0, 0.6)",
  border: "#2F2C27",
  borderStrong: "#7A746A",
  ring: "#8F8AF5",

  text: "#F2EFE9",
  textMuted: "#A8A196",
  textSubtle: "#7A746A",
  textInverse: "#1C1A17",
  inverse: "#ECE8E1",

  primary: "#8F8AF5",
  primaryFg: "#110F2E",
  primarySoft: "#23214A",
  primaryStrong: "#B7B3FA",

  accent: "#E08A5F",
  accentFg: "#1F0D04",
  accentSoft: "#3A2117",
  accentStrong: "#F0AE8C",

  success: "#4FB477",
  successFg: "#04170B",
  successSoft: "#142A1D",
  successStrong: "#86D3A2",

  warning: "#E8B04B",
  warningFg: "#1F1503",
  warningSoft: "#33270F",
  warningStrong: "#F5CB7C",

  danger: "#F06F63",
  dangerFg: "#200605",
  dangerSoft: "#3B1916",
  dangerStrong: "#F8A39A",

  info: "#5AA6E6",
  infoFg: "#03152A",
  infoSoft: "#11263A",
  infoStrong: "#94C6F2",

  ink,
};

export const base: Palette = {
  id: "base",
  name: "Base",
  rationale:
    "Warm off-white paper and near-black ink read as calm and trustworthy; a single " +
    "indigo carries every primary action so the eye always knows where to go. No " +
    "gradients, no neon: it gets out of the way of the product.",
  colors: { light, dark },
  fonts: { display: INTER, body: INTER },
  type: typeScale(),
  radius: { xs: 4, sm: 6, md: 10, lg: 14, xl: 20, "2xl": 28, pill: 999 },
  elevation: { light: elevation("#2A2218", "light"), dark: elevation("#000000", "dark") },
  motion: BASE_MOTION,
};
