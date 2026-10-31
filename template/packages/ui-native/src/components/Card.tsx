import type { ReactNode } from "react";
import { Pressable, View, type PressableProps, type StyleProp, type ViewStyle } from "react-native";
import Animated from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { usePressScale } from "../lib/press";
import { useLayout, useTheme } from "../theme/ThemeProvider";

/** `surface` (resting, bordered) · `sunken` (well) · `raised` (floating layers only: it casts a shadow). */
export type CardVariant = "surface" | "sunken" | "raised";

export interface CardProps extends Omit<PressableProps, "children" | "style"> {
  variant?: CardVariant;
  /** `card` = the layout's card padding (16 phone, 24 tablet). @default "card" */
  padding?: "none" | "card" | "compact";
  selected?: boolean;
  children?: ReactNode;
  style?: StyleProp<ViewStyle>;
}

/** Surface container. With `onPress` it becomes pressable (spring + haptic). No outer margin. */
export function Card({ variant = "surface", padding = "card", selected, children, style, onPress, ...rest }: CardProps) {
  const theme = useTheme();
  const layout = useLayout();
  const c = theme.colors;
  const press = usePressScale(0.98);
  const pad = padding === "none" ? 0 : padding === "compact" ? theme.space[3] : layout.card;
  const look: ViewStyle = {
    surface: { backgroundColor: c.surface, borderWidth: 1, borderColor: c.border },
    sunken: { backgroundColor: c.sunken },
    raised: { backgroundColor: c.surfaceRaised, boxShadow: theme.elevation.md },
  }[variant];
  const body: StyleProp<ViewStyle> = [{ borderRadius: theme.radius.lg, padding: pad }, look, selected ? { borderWidth: 2, borderColor: c.primary } : null, style];
  if (!onPress) return <View style={body}>{children}</View>;
  return (
    <Pressable
      accessibilityRole="button"
      onPressIn={press.onPressIn}
      onPressOut={press.onPressOut}
      onPress={(e) => {
        haptic.light();
        onPress(e);
      }}
      {...rest}
    >
      <Animated.View style={[body, press.style]}>{children}</Animated.View>
    </Pressable>
  );
}
