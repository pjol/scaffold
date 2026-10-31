import { withAlpha } from "@scaffoldapp/tokens";
import { LinearGradient } from "expo-linear-gradient";
import { useEffect, useState } from "react";
import { StyleSheet, View, type DimensionValue, type StyleProp, type ViewStyle } from "react-native";
import Animated, { Easing, cancelAnimation, useAnimatedStyle, useReducedMotion, useSharedValue, withRepeat, withTiming } from "react-native-reanimated";
import { useTheme } from "../theme/ThemeProvider";

export interface SkeletonProps {
  width?: DimensionValue;
  height?: DimensionValue;
  shape?: "rect" | "circle" | "text";
  style?: StyleProp<ViewStyle>;
}

/** Placeholder block shaped like the content it stands for. Shimmers; static under reduced motion. */
export function Skeleton({ width = "100%", height = 16, shape = "rect", style }: SkeletonProps) {
  const theme = useTheme();
  const reduce = useReducedMotion();
  const [w, setW] = useState(0);
  const x = useSharedValue(0);
  useEffect(() => {
    if (reduce || !w) return undefined;
    x.value = 0;
    x.value = withRepeat(withTiming(1, { duration: theme.motion.duration.shimmer, easing: Easing.inOut(Easing.ease) }), -1, false);
    return () => cancelAnimation(x);
  }, [reduce, w, x, theme.motion.duration.shimmer]);
  const sweep = useAnimatedStyle(() => ({ transform: [{ translateX: -w + x.value * w * 2 }] }));
  const radius = shape === "circle" ? theme.radius.pill : shape === "text" ? theme.radius.xs : theme.radius.md;
  return (
    <View
      onLayout={(e) => setW(e.nativeEvent.layout.width)}
      style={[{ width, height, borderRadius: radius, backgroundColor: theme.colors.sunken, overflow: "hidden" }, style]}
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
    >
      {!reduce && w > 0 ? (
        <Animated.View style={[StyleSheet.absoluteFill, { width: w }, sweep]}>
          <LinearGradient
            start={{ x: 0, y: 0.5 }}
            end={{ x: 1, y: 0.5 }}
            colors={["transparent", withAlpha(theme.colors.surfaceRaised, theme.mode === "dark" ? 0.1 : 0.7), "transparent"]}
            style={StyleSheet.absoluteFill}
          />
        </Animated.View>
      ) : null}
    </View>
  );
}

/** Lines of text (last one shorter). */
export function SkeletonText({ lines = 3, lineHeight = 16 }: { lines?: number; lineHeight?: number }) {
  const s = useTheme().space;
  return (
    <View style={{ gap: s[2] }}>
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} shape="text" height={lineHeight} width={i === lines - 1 && lines > 1 ? "60%" : "100%"} />
      ))}
    </View>
  );
}

/** A list row: leading circle + title line. Matches `ListRow` with `leading` (56 tall). */
export function SkeletonRow() {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.lg, paddingHorizontal: s[4] }}>
      <Skeleton shape="circle" width={s[8]} height={s[8]} />
      <View style={{ flex: 1 }}>
        <Skeleton shape="text" height={16} width="70%" />
      </View>
    </View>
  );
}
