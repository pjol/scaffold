import { Check } from "lucide-react-native";
import type { ReactNode } from "react";
import { Pressable, View, type PressableProps, type StyleProp, type ViewStyle } from "react-native";
import Animated from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { renderIcon, type IconLike } from "../lib/icon";
import { popIn } from "../lib/motion";
import { usePressScale } from "../lib/press";
import { useTheme } from "../theme/ThemeProvider";
import { Text } from "./Text";

export interface ChipProps extends Omit<PressableProps, "children" | "style"> {
  icon?: IconLike;
  count?: number;
  selected?: boolean;
  /** `md` 48 · `sm` 40 (hit area grown to 48). @default "md" */
  size?: "sm" | "md";
  children?: ReactNode;
  style?: StyleProp<ViewStyle>;
}

/** Filter or quick-pick button. Place chips with a gap of at least 8. */
export function Chip({ icon, count, selected, size = "md", children, style, onPress, ...rest }: ChipProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const press = usePressScale(0.95);
  const minHeight = size === "sm" ? theme.tap.sm : theme.tap.base;
  const fg = selected ? c.primaryStrong : c.text;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ selected: Boolean(selected) }}
      hitSlop={(theme.tap.base - minHeight) / 2}
      onPressIn={press.onPressIn}
      onPressOut={press.onPressOut}
      onPress={(e) => {
        haptic.selection();
        onPress?.(e);
      }}
      style={style}
      {...rest}
    >
      <Animated.View
        style={[
          {
            minHeight,
            paddingHorizontal: size === "sm" ? s[3] : s[4],
            paddingVertical: s[1],
            gap: s[2],
            borderRadius: theme.radius.pill,
            borderWidth: 1,
            flexDirection: "row",
            alignItems: "center",
            borderColor: selected ? c.primaryStrong : c.borderStrong,
            backgroundColor: selected ? c.primarySoft : c.surface,
          },
          press.style,
        ]}
      >
        {renderIcon(icon, { size: 16, color: fg, strokeWidth: 2.25 })}
        {typeof children === "string" ? (
          <Text role="label" weight={600} style={{ color: fg }}>
            {children}
          </Text>
        ) : (
          children
        )}
        {count !== undefined ? (
          <View style={{ borderRadius: theme.radius.pill, paddingHorizontal: s[1.5], backgroundColor: selected ? c.primary : c.sunken }}>
            <Text role="caption" tabular weight={600} maxFontSizeMultiplier={1.5} style={{ color: selected ? c.primaryFg : c.textMuted }}>
              {count}
            </Text>
          </View>
        ) : null}
      </Animated.View>
    </Pressable>
  );
}

export interface ToggleChipProps extends Omit<ChipProps, "selected"> {
  pressed: boolean;
  onPressedChange: (pressed: boolean) => void;
}

/** Chip that toggles; a check pops in when on. */
export function ToggleChip({ pressed, onPressedChange, icon, children, ...rest }: ToggleChipProps) {
  const theme = useTheme();
  return (
    <Chip
      selected={pressed}
      accessibilityState={{ checked: pressed }}
      onPress={() => onPressedChange(!pressed)}
      icon={
        pressed ? (
          <Animated.View key="on" entering={popIn(theme)}>
            <Check size={16} color={theme.colors.primaryStrong} strokeWidth={2.75} />
          </Animated.View>
        ) : (
          icon
        )
      }
      {...rest}
    >
      {children}
    </Chip>
  );
}
