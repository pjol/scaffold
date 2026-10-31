import { useEffect } from "react";
import Animated, { Easing, cancelAnimation, useAnimatedStyle, useReducedMotion, useSharedValue, withRepeat, withTiming } from "react-native-reanimated";
import Svg, { Circle } from "react-native-svg";
import { useTheme } from "../theme/ThemeProvider";

export interface SpinnerProps {
  size?: number;
  color?: string;
  /** Accessible label; omit when the parent already announces a busy state. */
  label?: string;
}

/** Ring spinner. Static arc under reduced motion (nothing loops). */
export function Spinner({ size = 24, color, label }: SpinnerProps) {
  const theme = useTheme();
  const reduce = useReducedMotion();
  const stroke = color ?? theme.colors.text;
  const sw = Math.max(2, size / 8);
  const r = (size - sw) / 2;
  const circ = 2 * Math.PI * r;
  const rot = useSharedValue(0);
  useEffect(() => {
    if (reduce) return undefined;
    rot.value = withRepeat(withTiming(360, { duration: 800, easing: Easing.linear }), -1, false);
    return () => cancelAnimation(rot);
  }, [reduce, rot]);
  const style = useAnimatedStyle(() => ({ transform: [{ rotate: `${rot.value}deg` }] }));
  return (
    <Animated.View
      style={[{ width: size, height: size }, style]}
      accessible={Boolean(label)}
      {...(label ? { accessibilityRole: "progressbar" as const, accessibilityLabel: label } : {})}
    >
      <Svg width={size} height={size}>
        <Circle cx={size / 2} cy={size / 2} r={r} stroke={stroke} strokeOpacity={0.2} strokeWidth={sw} fill="none" />
        <Circle cx={size / 2} cy={size / 2} r={r} stroke={stroke} strokeWidth={sw} fill="none" strokeLinecap="round" strokeDasharray={`${circ * 0.28} ${circ}`} />
      </Svg>
    </Animated.View>
  );
}
