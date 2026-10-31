import { useEffect, useState, type ReactNode } from "react";
import { Pressable, View, type LayoutChangeEvent, type StyleProp, type ViewStyle } from "react-native";
import Animated, { useAnimatedStyle, useSharedValue, withSpring } from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { renderIcon, type IconLike } from "../lib/icon";
import { useControllableState } from "../lib/useControllable";
import { useTheme } from "../theme/ThemeProvider";
import { Text } from "./Text";

export interface SegmentOption<V extends string = string> {
  value: V;
  label: ReactNode;
  icon?: IconLike;
  disabled?: boolean;
}

export interface SegmentedControlProps<V extends string = string> {
  options: SegmentOption<V>[];
  value?: V;
  defaultValue?: V;
  onValueChange?: (value: V) => void;
  style?: StyleProp<ViewStyle>;
  accessibilityLabel?: string;
}

/** Equal-width segments with a spring-sliding indicator. 48 tall; labels wrap at large text. */
export function SegmentedControl<V extends string = string>({ options, value, defaultValue, onValueChange, style, accessibilityLabel }: SegmentedControlProps<V>) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [current, setCurrent] = useControllableState<V>({ value, defaultValue: defaultValue ?? (options[0]?.value as V), onChange: onValueChange });
  const [w, setW] = useState(0);
  const pad = s[1];
  const segW = options.length ? (w - pad * 2) / options.length : 0;
  const idx = Math.max(0, options.findIndex((o) => o.value === current));
  const x = useSharedValue(0);
  const spring = theme.motion.spring.snappy;
  useEffect(() => {
    x.value = withSpring(idx * segW, spring);
  }, [idx, segW, x, spring]);
  const pill = useAnimatedStyle(() => ({ transform: [{ translateX: x.value }] }));
  return (
    <View
      accessibilityRole="radiogroup"
      {...(accessibilityLabel ? { accessibilityLabel } : {})}
      onLayout={(e: LayoutChangeEvent) => setW(e.nativeEvent.layout.width)}
      style={[{ minHeight: theme.tap.base, padding: pad, borderRadius: theme.radius.lg, backgroundColor: c.sunken, flexDirection: "row" }, style]}
    >
      {w > 0 ? (
        <Animated.View
          style={[{ position: "absolute", top: pad, bottom: pad, left: pad, width: segW, borderRadius: theme.radius.md, backgroundColor: c.surfaceRaised, boxShadow: theme.elevation.xs }, pill]}
        />
      ) : null}
      {options.map((o) => {
        const selected = o.value === current;
        const fg = selected ? c.text : c.textMuted;
        return (
          <Pressable
            key={o.value}
            accessibilityRole="radio"
            accessibilityState={{ checked: selected, disabled: Boolean(o.disabled) }}
            disabled={o.disabled}
            onPress={() => {
              if (!selected) haptic.selection();
              setCurrent(o.value);
            }}
            style={{ flex: 1, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: s[1.5], paddingVertical: s[2], paddingHorizontal: s[2], opacity: o.disabled ? 0.4 : 1 }}
          >
            {renderIcon(o.icon, { size: 16, color: fg })}
            {typeof o.label === "string" ? (
              <Text role="label" weight={600} align="center" style={{ color: fg, flexShrink: 1 }}>
                {o.label}
              </Text>
            ) : (
              o.label
            )}
          </Pressable>
        );
      })}
    </View>
  );
}
