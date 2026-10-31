import { Check } from "lucide-react-native";
import { useEffect, type ReactNode } from "react";
import { Pressable, View, type StyleProp, type ViewStyle } from "react-native";
import Animated, { useAnimatedStyle, useSharedValue, withSpring, withTiming } from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { usePressScale } from "../lib/press";
import { useControllableState } from "../lib/useControllable";
import { useTheme } from "../theme/ThemeProvider";
import { InfoTip } from "./InfoTip";
import { Text } from "./Text";

export interface CheckboxProps {
  checked?: boolean;
  defaultChecked?: boolean;
  onCheckedChange?: (checked: boolean) => void;
  /** The whole row (48 tall) toggles. Nested `<Text onPress>` links keep their own press. */
  label?: ReactNode;
  info?: ReactNode;
  disabled?: boolean;
  /** Required box left unchecked. */
  invalid?: boolean;
  accessibilityLabel?: string;
  style?: StyleProp<ViewStyle>;
}

/** Checkbox row; the check pops in with the `bouncy` spring. */
export function Checkbox({ checked, defaultChecked = false, onCheckedChange, label, info, disabled, invalid, accessibilityLabel, style }: CheckboxProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [on, setOn] = useControllableState({ value: checked, defaultValue: defaultChecked, onChange: onCheckedChange });
  const press = usePressScale(0.9);
  const p = useSharedValue(on ? 1 : 0);
  useEffect(() => {
    p.value = on ? withSpring(1, theme.motion.spring.bouncy) : withTiming(0, { duration: theme.motion.duration.fast });
  }, [on, p, theme.motion]);
  const check = useAnimatedStyle(() => ({ opacity: Math.min(1, p.value * 1.5), transform: [{ scale: 0.4 + 0.6 * p.value }] }));

  return (
    <View style={[{ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: theme.tap.base }, style]}>
      <Pressable
        disabled={disabled}
        onPressIn={press.onPressIn}
        onPressOut={press.onPressOut}
        onPress={() => {
          haptic.selection();
          setOn(!on);
        }}
        accessibilityRole="checkbox"
        accessibilityState={{ checked: on, disabled: Boolean(disabled) }}
        {...(accessibilityLabel ? { accessibilityLabel } : {})}
        style={{ flex: 1, flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.base, paddingVertical: s[2], opacity: disabled ? 0.5 : 1 }}
      >
        <Animated.View
          style={[
            {
              width: s[6],
              height: s[6],
              borderRadius: theme.radius.sm,
              borderWidth: 2,
              alignItems: "center",
              justifyContent: "center",
              borderColor: on ? c.primary : invalid ? c.dangerStrong : c.borderStrong,
              backgroundColor: on ? c.primary : c.surface,
            },
            press.style,
          ]}
        >
          <Animated.View style={check}>
            <Check size={16} color={c.primaryFg} strokeWidth={3} />
          </Animated.View>
        </Animated.View>
        {label ? typeof label === "string" ? <Text style={{ flex: 1 }}>{label}</Text> : <View style={{ flex: 1 }}>{label}</View> : null}
      </Pressable>
      {info ? <InfoTip content={info} /> : null}
    </View>
  );
}
