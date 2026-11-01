import { useEffect, useRef, useState, type ReactNode } from "react";
import { StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import Animated, { Easing, useAnimatedStyle, useReducedMotion, useSharedValue, withDelay, withTiming } from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { useTheme } from "../theme/ThemeProvider";

interface Particle {
  id: number;
  vx: number;
  vy: number;
  rot: number;
  color: string;
  w: number;
  h: number;
  round: boolean;
  delay: number;
  dur: number;
}

function Piece({ p }: { p: Particle }) {
  const t = useSharedValue(0);
  useEffect(() => {
    t.value = withDelay(p.delay, withTiming(1, { duration: p.dur, easing: Easing.out(Easing.cubic) }));
  }, [p, t]);
  const style = useAnimatedStyle(() => {
    const k = t.value;
    return {
      opacity: k < 0.7 ? 1 : 1 - (k - 0.7) / 0.3,
      transform: [{ translateX: p.vx * k }, { translateY: p.vy * k + 140 * k * k }, { rotate: `${p.rot * k}deg` }, { scale: 0.5 + Math.min(1, k * 3) * 0.5 }],
    };
  });
  return <Animated.View style={[{ position: "absolute", width: p.w, height: p.h, marginLeft: -p.w / 2, marginTop: -p.h / 2, borderRadius: p.round ? p.w : 2, backgroundColor: p.color }, style]} />;
}

function Ring({ color, big }: { color: string; big: boolean }) {
  const t = useSharedValue(0);
  useEffect(() => {
    t.value = withTiming(1, { duration: 560, easing: Easing.out(Easing.cubic) });
  }, [t]);
  const style = useAnimatedStyle(() => ({ opacity: 0.9 * (1 - t.value), transform: [{ scale: 0.2 + t.value * (big ? 3 : 1.4) }] }));
  return <Animated.View style={[{ position: "absolute", width: 64, height: 64, marginLeft: -32, marginTop: -32, borderRadius: 32, borderWidth: 4, borderColor: color }, style]} />;
}

export interface CelebrateProps {
  /** Change it to fire (a counter, an id). */
  fire: number | boolean | string | null | undefined;
  particles?: number;
  /** Max travel in dp. @default 160 */
  power?: number;
  colors?: string[];
  /** Success haptic. @default true */
  haptics?: boolean;
  children?: ReactNode;
  style?: StyleProp<ViewStyle>;
}

/** Confetti + ring burst for a real success moment. Sparingly. Reduced motion → ring only. */
export function Celebrate({ fire, particles = 32, power = 160, colors, haptics = true, children, style }: CelebrateProps) {
  const theme = useTheme();
  const reduce = useReducedMotion();
  const c = theme.colors;
  const palette = colors ?? [c.primary, c.accent, c.success, c.warning, c.info];
  const [bursts, setBursts] = useState<{ key: number; items: Particle[] }[]>([]);
  const first = useRef(true);
  const n = useRef(0);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return undefined;
    }
    if (!fire) return undefined;
    if (haptics) haptic.success();
    const key = ++n.current;
    const items: Particle[] = reduce
      ? []
      : Array.from({ length: particles }, (_, i) => {
          const a = (i / particles) * Math.PI * 2 + Math.random() * 0.5;
          const d = power * (0.45 + Math.random() * 0.55);
          const strip = i % 3 === 2;
          const size = 6 + Math.random() * 6;
          return {
            id: key * 1000 + i,
            vx: Math.cos(a) * d,
            vy: Math.sin(a) * d - 40,
            rot: (Math.random() - 0.5) * 720,
            color: palette[i % palette.length] ?? c.primary,
            w: strip ? size * 0.45 : size,
            h: strip ? size * 1.6 : size,
            round: i % 3 === 1,
            delay: Math.random() * 80,
            dur: 900 + Math.random() * 500,
          };
        });
    setBursts((b) => [...b, { key, items }]);
    const timer = setTimeout(() => setBursts((b) => b.filter((x) => x.key !== key)), 1600);
    return () => clearTimeout(timer);
    // Fires only when `fire` changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fire]);
  return (
    <View style={[children ? null : StyleSheet.absoluteFill, style]} pointerEvents="box-none">
      {children}
      <View pointerEvents="none" style={[StyleSheet.absoluteFill, { alignItems: "center", justifyContent: "center", overflow: "visible" }]}>
        {bursts.map((b) => (
          <View key={b.key} style={{ position: "absolute", left: "50%", top: "50%" }}>
            <Ring color={c.primary} big={!reduce} />
            {b.items.map((p) => (
              <Piece key={p.id} p={p} />
            ))}
          </View>
        ))}
      </View>
    </View>
  );
}
