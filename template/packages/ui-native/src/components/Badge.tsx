import type { ReactNode } from "react";
import { View, type StyleProp, type ViewStyle } from "react-native";
import { renderIcon, type IconLike } from "../lib/icon";
import { useTheme } from "../theme/ThemeProvider";
import { Text } from "./Text";

export type BadgeTone = "neutral" | "primary" | "accent" | "success" | "warning" | "danger" | "info";

export interface BadgeProps {
  tone?: BadgeTone;
  variant?: "soft" | "solid";
  size?: "sm" | "md";
  icon?: IconLike;
  /** Leading status dot. */
  dot?: boolean;
  children?: ReactNode;
  style?: StyleProp<ViewStyle>;
}

/** Compact state label. Grows with the text (never a fixed height). */
export function Badge({ tone = "neutral", variant = "soft", size = "md", icon, dot, children, style }: BadgeProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const map: Record<BadgeTone, { solid: string; fg: string; soft: string; strong: string }> = {
    neutral: { solid: c.inverse, fg: c.textInverse, soft: c.sunken, strong: c.textMuted },
    primary: { solid: c.primary, fg: c.primaryFg, soft: c.primarySoft, strong: c.primaryStrong },
    accent: { solid: c.accent, fg: c.accentFg, soft: c.accentSoft, strong: c.accentStrong },
    success: { solid: c.success, fg: c.successFg, soft: c.successSoft, strong: c.successStrong },
    warning: { solid: c.warning, fg: c.warningFg, soft: c.warningSoft, strong: c.warningStrong },
    danger: { solid: c.danger, fg: c.dangerFg, soft: c.dangerSoft, strong: c.dangerStrong },
    info: { solid: c.info, fg: c.infoFg, soft: c.infoSoft, strong: c.infoStrong },
  };
  const t = map[tone];
  const solid = variant === "solid";
  const fg = solid ? t.fg : t.strong;
  const sm = size === "sm";
  return (
    <View
      style={[
        {
          flexDirection: "row",
          alignItems: "center",
          alignSelf: "flex-start",
          gap: s[1],
          paddingHorizontal: sm ? s[2] : s[3],
          paddingVertical: sm ? s[0.5] : s[1],
          borderRadius: theme.radius.pill,
          backgroundColor: solid ? t.solid : t.soft,
        },
        style,
      ]}
    >
      {dot ? <View style={{ width: s[2], height: s[2], borderRadius: s[1], backgroundColor: solid ? t.fg : t.solid }} /> : null}
      {renderIcon(icon, { size: sm ? 12 : 14, color: fg, strokeWidth: 2.5 })}
      {typeof children === "string" || typeof children === "number" ? (
        <Text role={sm ? "caption" : "label"} weight={600} maxFontSizeMultiplier={1.5} style={{ color: fg }}>
          {children}
        </Text>
      ) : (
        children
      )}
    </View>
  );
}
