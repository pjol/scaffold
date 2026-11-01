import { withAlpha } from "@scaffoldapp/tokens";
import type { NativeTheme } from "@scaffoldapp/tokens/native";
import type { ReactNode } from "react";
import { Pressable, StyleSheet, View, type PressableProps, type StyleProp, type ViewStyle } from "react-native";
import Animated, { useAnimatedStyle } from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { renderIcon, type IconLike } from "../lib/icon";
import { fadeIn, fadeOut } from "../lib/motion";
import { usePressScale } from "../lib/press";
import { useTheme } from "../theme/ThemeProvider";
import { Spinner } from "./Spinner";
import { Text } from "./Text";

export type ButtonVariant = "primary" | "secondary" | "outline" | "ghost" | "soft" | "danger";
/** `lg` (48) is the mobile default; `md` (40) grows its hit area to 48; `xl` (56) for a hero action. */
export type ButtonSize = "md" | "lg" | "xl";

export function buttonColors(theme: NativeTheme, variant: ButtonVariant): { bg: string; fg: string; border?: string } {
  const c = theme.colors;
  switch (variant) {
    case "primary":
      return { bg: c.primary, fg: c.primaryFg };
    case "secondary":
      return { bg: withAlpha(c.text, 0.08), fg: c.text };
    case "outline":
      return { bg: "transparent", fg: c.text, border: c.borderStrong };
    case "ghost":
      return { bg: "transparent", fg: c.primaryStrong };
    case "soft":
      return { bg: c.primarySoft, fg: c.primaryStrong };
    case "danger":
      return { bg: c.danger, fg: c.dangerFg };
  }
}

export interface ButtonProps extends Omit<PressableProps, "children" | "style"> {
  /** One `primary` per screen. @default "primary" */
  variant?: ButtonVariant;
  /** @default "lg" */
  size?: ButtonSize;
  loading?: boolean;
  /** lucide component (`Save`) or element. */
  leftIcon?: IconLike;
  rightIcon?: IconLike;
  fullWidth?: boolean;
  /** Light haptic on press. @default true */
  haptics?: boolean;
  /** A verb naming the outcome (concrete/writing.md). */
  children?: ReactNode;
  style?: StyleProp<ViewStyle>;
}

/**
 * Press scales with the `press` spring and fires a light haptic; `loading` swaps the left
 * icon for a spinner. Labels wrap at large text sizes instead of clipping.
 *
 * ```tsx
 * <Button leftIcon={Save} onPress={save} loading={update.isPending}>Save</Button>
 * ```
 */
export function Button({ variant = "primary", size = "lg", loading, leftIcon, rightIcon, fullWidth, haptics = true, disabled, children, style, onPress, ...rest }: ButtonProps) {
  const theme = useTheme();
  const s = theme.space;
  const { bg, fg, border } = buttonColors(theme, variant);
  const press = usePressScale();
  const overlay = useAnimatedStyle(() => ({ opacity: press.pressed.value * 0.12 }));
  const isDisabled = Boolean(disabled || loading);
  const minHeight = size === "md" ? theme.size.control : size === "lg" ? theme.tap.base : theme.tap.lg;
  const px = size === "md" ? s[4] : size === "lg" ? s[5] : s[6];
  const iconSize = size === "md" ? 18 : 20;
  const hit = Math.max(0, (theme.tap.base - minHeight) / 2);

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ disabled: isDisabled, busy: Boolean(loading) }}
      disabled={isDisabled}
      hitSlop={hit}
      onPressIn={press.onPressIn}
      onPressOut={press.onPressOut}
      onPress={(e) => {
        if (haptics) haptic.light();
        onPress?.(e);
      }}
      style={[{ alignSelf: fullWidth ? "stretch" : "flex-start" }, style]}
      {...rest}
    >
      <Animated.View
        style={[
          styles.base,
          { minHeight, paddingHorizontal: px, paddingVertical: s[2], gap: s[2], borderRadius: theme.radius.md, backgroundColor: bg, opacity: isDisabled ? 0.5 : 1 },
          border ? { borderWidth: 1, borderColor: border } : null,
          press.style,
        ]}
      >
        <Animated.View pointerEvents="none" style={[StyleSheet.absoluteFill, { backgroundColor: fg, borderRadius: theme.radius.md }, overlay]} />
        {loading ? (
          <Animated.View key="spin" entering={fadeIn(theme)} exiting={fadeOut(theme)}>
            <Spinner size={iconSize} color={fg} />
          </Animated.View>
        ) : leftIcon ? (
          <View key="icon">{renderIcon(leftIcon, { size: iconSize, color: fg })}</View>
        ) : null}
        {children !== undefined && children !== null ? (
          typeof children === "string" || typeof children === "number" ? (
            <Text role={size === "md" ? "label" : "body"} weight={600} align="center" style={{ color: fg, flexShrink: 1 }}>
              {children}
            </Text>
          ) : (
            children
          )
        ) : null}
        {rightIcon ? <View>{renderIcon(rightIcon, { size: iconSize, color: fg })}</View> : null}
      </Animated.View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  base: { flexDirection: "row", alignItems: "center", justifyContent: "center", overflow: "hidden" },
});
