import { withAlpha } from "@scaffoldapp/tokens";
import { Pressable, type PressableProps, type StyleProp, type ViewStyle } from "react-native";
import Animated from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { renderIcon, type IconLike } from "../lib/icon";
import { usePressScale } from "../lib/press";
import { useTheme } from "../theme/ThemeProvider";
import { Spinner } from "./Spinner";

export type IconButtonVariant = "primary" | "secondary" | "outline" | "ghost" | "soft" | "danger";

export interface IconButtonProps extends Omit<PressableProps, "children" | "style"> {
  /** Accessible name (required: there is no visible label). */
  label: string;
  icon: IconLike;
  /** @default "ghost" */
  variant?: IconButtonVariant;
  /** `md` 40 (hit area grown to 48) · `lg` 48 · `xl` 56. @default "md" */
  size?: "md" | "lg" | "xl";
  shape?: "circle" | "square";
  loading?: boolean;
  haptics?: boolean;
  style?: StyleProp<ViewStyle>;
}

/** Icon-only button; the hit area is never under 48. */
export function IconButton({ label, icon, variant = "ghost", size = "md", shape = "circle", loading, haptics = true, disabled, style, onPress, ...rest }: IconButtonProps) {
  const theme = useTheme();
  const c = theme.colors;
  const press = usePressScale(0.92);
  const dim = size === "md" ? theme.size.control : size === "lg" ? theme.tap.base : theme.tap.lg;
  const colors = {
    primary: { bg: c.primary, fg: c.primaryFg },
    secondary: { bg: withAlpha(c.text, 0.08), fg: c.text },
    outline: { bg: "transparent", fg: c.text, border: c.borderStrong },
    ghost: { bg: "transparent", fg: c.text },
    soft: { bg: c.primarySoft, fg: c.primaryStrong },
    danger: { bg: c.dangerSoft, fg: c.dangerStrong },
  }[variant] as { bg: string; fg: string; border?: string };
  const iconSize = size === "md" ? 20 : size === "lg" ? 22 : 24;
  const isDisabled = Boolean(disabled || loading);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled: isDisabled, busy: Boolean(loading) }}
      disabled={isDisabled}
      hitSlop={Math.max(0, (theme.tap.base - dim) / 2)}
      onPressIn={press.onPressIn}
      onPressOut={press.onPressOut}
      onPress={(e) => {
        if (haptics) haptic.light();
        onPress?.(e);
      }}
      style={style}
      {...rest}
    >
      <Animated.View
        style={[
          { width: dim, height: dim, borderRadius: shape === "circle" ? dim / 2 : theme.radius.md, backgroundColor: colors.bg, alignItems: "center", justifyContent: "center", opacity: isDisabled ? 0.5 : 1 },
          colors.border ? { borderWidth: 1, borderColor: colors.border } : null,
          press.style,
        ]}
      >
        {loading ? <Spinner size={iconSize} color={colors.fg} /> : renderIcon(icon, { size: iconSize, color: colors.fg })}
      </Animated.View>
    </Pressable>
  );
}
