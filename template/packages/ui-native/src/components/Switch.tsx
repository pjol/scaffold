import { useEffect, type ReactNode } from "react";
import { Pressable, View, type StyleProp, type ViewStyle } from "react-native";
import Animated, { interpolateColor, useAnimatedStyle, useSharedValue, withSpring } from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { useControllableState } from "../lib/useControllable";
import { useTheme } from "../theme/ThemeProvider";
import { InfoTip } from "./InfoTip";
import { Text } from "./Text";

export interface SwitchProps {
  value?: boolean;
  defaultValue?: boolean;
  onValueChange?: (value: boolean) => void;
  disabled?: boolean;
  /** Inline label: the whole row (48 tall) toggles. */
  label?: string;
  info?: ReactNode;
  style?: StyleProp<ViewStyle>;
  accessibilityLabel?: string;
}

const W = 52;
const H = 32;
const PAD = 4;

/** Switch with a spring thumb, colour crossfade and a selection haptic. */
export function Switch({ value, defaultValue = false, onValueChange, disabled, label, info, style, accessibilityLabel }: SwitchProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [on, setOn] = useControllableState({ value, defaultValue, onChange: onValueChange });
  const thumb = H - PAD * 2;
  const p = useSharedValue(on ? 1 : 0);
  const spring = theme.motion.spring.snappy;
  useEffect(() => {
    p.value = withSpring(on ? 1 : 0, spring);
  }, [on, p, spring]);
  const offColor = c.borderStrong;
  const onColor = c.primary;
  const track = useAnimatedStyle(() => ({ backgroundColor: interpolateColor(p.value, [0, 1], [offColor, onColor]) }));
  const knob = useAnimatedStyle(() => ({ transform: [{ translateX: p.value * (W - thumb - PAD * 2) }] }));
  const control = (
    <Animated.View style={[{ width: W, height: H, borderRadius: H / 2, padding: PAD, opacity: disabled ? 0.5 : 1 }, track]}>
      <Animated.View style={[{ width: thumb, height: thumb, borderRadius: thumb / 2, backgroundColor: c.surfaceRaised, boxShadow: theme.elevation.sm }, knob]} />
    </Animated.View>
  );
  return (
    <Pressable
      onPress={() => {
        if (disabled) return;
        haptic.selection();
        setOn(!on);
      }}
      disabled={disabled}
      accessibilityRole="switch"
      accessibilityState={{ checked: on, disabled: Boolean(disabled) }}
      accessibilityLabel={accessibilityLabel ?? label}
      hitSlop={label ? 0 : (theme.tap.base - H) / 2}
      style={[label ? { flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.base } : null, style]}
    >
      {label ? (
        <View style={{ flex: 1, flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text weight={500} style={{ flexShrink: 1 }}>
            {label}
          </Text>
          {info ? <InfoTip content={info} label={`About ${label}`} /> : null}
        </View>
      ) : null}
      {control}
    </Pressable>
  );
}
