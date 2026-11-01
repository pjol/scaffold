import type { TypeRole } from "@scaffoldapp/tokens";
import { memo, useEffect } from "react";
import { StyleSheet, View, useWindowDimensions, type StyleProp, type TextStyle } from "react-native";
import Animated, { useAnimatedStyle, useReducedMotion, useSharedValue, withSpring } from "react-native-reanimated";
import { formatInteger } from "../lib/format";
import { useTheme } from "../theme/ThemeProvider";
import { Text, type TextTone } from "./Text";

const DIGITS = ["0", "1", "2", "3", "4", "5", "6", "7", "8", "9"];

export interface AnimatedNumberProps {
  value: number;
  /** Number → string. Digits roll; other characters stay put. */
  format?: (n: number) => string;
  /** @default "bodyLg" */
  role?: TypeRole;
  tone?: TextTone;
  style?: StyleProp<TextStyle>;
  /** @default true */
  animate?: boolean;
  spring?: "snappy" | "gentle" | "bouncy";
}

type Spring = { stiffness: number; damping: number; mass: number };

interface DigitProps {
  d: number;
  lh: number;
  role: TypeRole;
  tone: TextTone;
  style: StyleProp<TextStyle>;
  spring: Spring;
}

const Digit = memo(function Digit({ d, lh, role, tone, style, spring }: DigitProps) {
  const y = useSharedValue(-d * lh);
  useEffect(() => {
    y.value = withSpring(-d * lh, spring);
  }, [d, lh, y, spring]);
  const strip = useAnimatedStyle(() => ({ transform: [{ translateY: y.value }] }));
  return (
    <View style={{ height: lh, overflow: "hidden" }}>
      <Text role={role} maxFontSizeMultiplier={1} style={[style, { opacity: 0, lineHeight: lh }]}>
        0
      </Text>
      <Animated.View style={[StyleSheet.absoluteFill, strip]}>
        {DIGITS.map((n) => (
          <Text key={n} role={role} tone={tone} align="center" maxFontSizeMultiplier={1} style={[style, { lineHeight: lh, height: lh }]}>
            {n}
          </Text>
        ))}
      </Animated.View>
    </View>
  );
});

/**
 * Rolling-digit number for counts, totals and timers. The roll needs a fixed line height,
 * so under a larger text size or reduced motion it renders plain text, which scales.
 */
export function AnimatedNumber({ value, format = formatInteger, role = "bodyLg", tone = "default", style, animate = true, spring = "snappy" }: AnimatedNumberProps) {
  const theme = useTheme();
  const reduce = useReducedMotion();
  const { fontScale } = useWindowDimensions();
  const text = format(value);
  const flat = StyleSheet.flatten([theme.type[role], style]);
  const lh = typeof flat.lineHeight === "number" ? flat.lineHeight : theme.type[role].lineHeight;
  const tabular: StyleProp<TextStyle> = [{ fontVariant: ["tabular-nums"] }, style];
  if (!animate || reduce || fontScale > 1) {
    return (
      <Text role={role} tone={tone} style={tabular}>
        {text}
      </Text>
    );
  }
  return (
    <View accessible accessibilityRole="text" accessibilityLabel={text} style={{ flexDirection: "row", alignItems: "flex-end" }}>
      {text.split("").map((ch, i, arr) => {
        const key = arr.length - i;
        return DIGITS.includes(ch) ? (
          <Digit key={`d${key}`} d={Number(ch)} lh={lh} role={role} tone={tone} style={tabular} spring={theme.motion.spring[spring]} />
        ) : (
          <Text key={`c${key}${ch}`} role={role} tone={tone} maxFontSizeMultiplier={1} style={[tabular, { lineHeight: lh }]}>
            {ch}
          </Text>
        );
      })}
    </View>
  );
}
