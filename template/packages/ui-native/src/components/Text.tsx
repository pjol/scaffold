import type { FontWeight, TypeRole } from "@scaffoldapp/tokens";
import { getPalette } from "@scaffoldapp/tokens/registry";
import { Text as RNText, type TextProps as RNTextProps } from "react-native";
import { useTheme } from "../theme/ThemeProvider";

export type TextTone = "default" | "muted" | "subtle" | "primary" | "accent" | "danger" | "success" | "warning" | "info" | "inverse" | "onPrimary";

export interface TextProps extends Omit<RNTextProps, "role"> {
  /** Type role from the scale (concrete/design.md → Type). @default "body" */
  role?: TypeRole;
  tone?: TextTone;
  /** Another weight of the role's family (picks the loaded face; never `fontWeight`). */
  weight?: FontWeight;
  align?: "auto" | "left" | "right" | "center" | "justify";
  /** Tabular figures (tables, money, timers). */
  tabular?: boolean;
}

/**
 * Text in a type role. Font scaling stays on: pass `maxFontSizeMultiplier` (1.5–2) only
 * for cramped chrome such as tab labels and badges.
 */
export function Text({ role = "body", tone = "default", weight, align, tabular, style, ...rest }: TextProps) {
  const theme = useTheme();
  const c = theme.colors;
  const color = {
    default: c.text,
    muted: c.textMuted,
    subtle: c.textSubtle,
    primary: c.primaryStrong,
    accent: c.accentStrong,
    danger: c.dangerStrong,
    success: c.successStrong,
    warning: c.warningStrong,
    info: c.infoStrong,
    inverse: c.textInverse,
    onPrimary: c.primaryFg,
  }[tone];
  const family = getPalette(theme.paletteId).type[role].family;
  return (
    <RNText
      style={[
        theme.type[role],
        { color },
        weight ? { fontFamily: theme.font(family, weight) } : null,
        align ? { textAlign: align } : null,
        tabular ? { fontVariant: ["tabular-nums"] } : null,
        style,
      ]}
      {...rest}
    />
  );
}

type RoleText = Omit<TextProps, "role">;

/** The one landing headline, if any. */
export const Hero = (p: RoleText) => <Text role="hero" accessibilityRole="header" {...p} />;
/** Screen hero (sign-in, empty app). */
export const Display = (p: RoleText) => <Text role="display" accessibilityRole="header" {...p} />;
export const Headline = (p: RoleText) => <Text role="headline" accessibilityRole="header" {...p} />;
/** Mobile screen title. */
export const Title = (p: RoleText) => <Text role="title" accessibilityRole="header" {...p} />;
/** Card titles, list headers, dialog and sheet titles. */
export const TitleSm = (p: RoleText) => <Text role="titleSm" accessibilityRole="header" {...p} />;
export const BodyLg = (p: RoleText) => <Text role="bodyLg" {...p} />;
export const Body = (p: RoleText) => <Text role="body" {...p} />;
/** Form labels and dense UI. */
export const Label = (p: RoleText) => <Text role="label" {...p} />;
/** Metadata and timestamps. Never a subtitle. */
export const Caption = (p: RoleText) => <Text role="caption" tone="muted" {...p} />;
