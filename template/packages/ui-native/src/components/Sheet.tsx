import { X } from "lucide-react-native";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Modal, Pressable, StyleSheet, View, useWindowDimensions, type StyleProp, type ViewStyle } from "react-native";
import { Gesture, GestureDetector, GestureHandlerRootView } from "react-native-gesture-handler";
import Animated, { Extrapolation, interpolate, useAnimatedStyle, useSharedValue, withSpring } from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { scheduleOnRN } from "react-native-worklets";
import { useLayout, useTheme } from "../theme/ThemeProvider";
import { IconButton } from "./IconButton";
import { InfoTip } from "./InfoTip";
import { Text } from "./Text";

export type SnapPoint = number | `${number}%`;

export interface SheetProps {
  open: boolean;
  /** Called when the user dismisses (drag down, backdrop, back button, ×). */
  onClose: () => void;
  /** After the close animation finished: start the next modal (a picker) from here. */
  onDismissed?: () => void;
  title?: string;
  info?: ReactNode;
  /** Heights (dp or % of the window), smallest first. @default ["55%"] */
  snapPoints?: SnapPoint[];
  /** @default 0 */
  initialSnap?: number;
  /** Drag down / tap backdrop / back button dismiss. @default true */
  dismissible?: boolean;
  /** Sticky footer for the sheet's one primary action. */
  footer?: ReactNode;
  children?: ReactNode;
  contentStyle?: StyleProp<ViewStyle>;
}

/**
 * Bottom sheet with snap points (Reanimated + Gesture Handler): springs between snaps with
 * the release velocity, flick down to dismiss. Handle 32×4; bottom padding 16 + safe area.
 * Phones prefer sheets over centered dialogs for create/edit flows.
 */
export function Sheet({ open, onClose, onDismissed, title, info, snapPoints = ["55%"], initialSnap = 0, dismissible = true, footer, children, contentStyle }: SheetProps) {
  const theme = useTheme();
  const layout = useLayout();
  const c = theme.colors;
  const s = theme.space;
  const insets = useSafeAreaInsets();
  const { height: winH } = useWindowDimensions();
  const snaps = useMemo(
    () => snapPoints.map((p) => (typeof p === "number" ? p : (parseFloat(p) / 100) * winH)).map((v) => Math.min(v, winH - insets.top - s[2])),
    [snapPoints, winH, insets.top, s],
  );
  const sheetH = Math.max(...snaps);
  // translateY: 0 = open at the largest snap; sheetH = hidden.
  const positions = useMemo(() => snaps.map((v) => sheetH - v), [snaps, sheetH]);
  const [rendered, setRendered] = useState(open);
  const y = useSharedValue(sheetH);
  const start = useSharedValue(0);
  const spring = theme.motion.spring.sheet;
  const dismissedRef = useRef(onDismissed);
  dismissedRef.current = onDismissed;
  const finishClose = useCallback(() => {
    setRendered(false);
    dismissedRef.current?.();
  }, []);

  useEffect(() => {
    if (open) {
      setRendered(true);
      y.value = sheetH;
      y.value = withSpring(positions[Math.min(initialSnap, positions.length - 1)] ?? 0, spring);
    } else if (rendered) {
      y.value = withSpring(sheetH + s[10], { ...spring, overshootClamping: true }, (done) => {
        if (done) scheduleOnRN(finishClose);
      });
    }
    // Only open/close drive the animation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const requestClose = useCallback(() => {
    if (dismissible) onClose();
  }, [dismissible, onClose]);

  const pan = Gesture.Pan()
    .onStart(() => {
      start.value = y.value;
    })
    .onUpdate((e) => {
      const next = start.value + e.translationY;
      y.value = next < 0 ? next * 0.25 : next;
    })
    .onEnd((e) => {
      const projected = y.value + e.velocityY * 0.18;
      const lowest = positions[0] ?? 0;
      if (dismissible && projected > lowest + Math.min(140, (sheetH - lowest) * 0.5 + 60)) {
        scheduleOnRN(onClose);
        return;
      }
      let best = positions[0] ?? 0;
      for (const p of positions) if (Math.abs(p - projected) < Math.abs(best - projected)) best = p;
      y.value = withSpring(best, { ...spring, velocity: e.velocityY });
    });

  const sheetStyle = useAnimatedStyle(() => ({ transform: [{ translateY: y.value }] }));
  const backdrop = useAnimatedStyle(() => ({ opacity: interpolate(y.value, [positions[0] ?? 0, sheetH], [1, 0], Extrapolation.CLAMP) }));

  if (!rendered) return null;
  const overscroll = s[10];

  return (
    <Modal visible transparent animationType="none" statusBarTranslucent navigationBarTranslucent onRequestClose={requestClose}>
      <GestureHandlerRootView style={{ flex: 1 }}>
        <Animated.View style={[StyleSheet.absoluteFill, { backgroundColor: c.overlay }, backdrop]}>
          <Pressable style={StyleSheet.absoluteFill} onPress={requestClose} accessibilityLabel="Close" />
        </Animated.View>
        <Animated.View
          accessibilityViewIsModal
          style={[
            styles.sheet,
            {
              height: sheetH + overscroll,
              paddingBottom: overscroll,
              bottom: -overscroll,
              backgroundColor: c.surfaceRaised,
              borderTopLeftRadius: theme.radius["2xl"],
              borderTopRightRadius: theme.radius["2xl"],
              boxShadow: theme.elevation.xl,
            },
            sheetStyle,
          ]}
        >
          <GestureDetector gesture={pan}>
            <View>
              <View style={{ height: s[6], alignItems: "center", justifyContent: "center" }}>
                <View style={{ width: s[8], height: s[1], borderRadius: s[0.5], backgroundColor: c.borderStrong }} />
              </View>
              {title || dismissible ? (
                <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], paddingHorizontal: layout.gutter, paddingBottom: s[2] }}>
                  <View style={{ flex: 1, flexDirection: "row", alignItems: "center", gap: s[2] }}>
                    {title ? (
                      <Text role="titleSm" accessibilityRole="header" style={{ flexShrink: 1 }}>
                        {title}
                      </Text>
                    ) : null}
                    {info ? <InfoTip content={info} size="md" /> : null}
                  </View>
                  {dismissible ? <IconButton label="Close" icon={X} variant="secondary" onPress={onClose} /> : null}
                </View>
              ) : null}
            </View>
          </GestureDetector>
          <View style={[{ flex: 1, paddingHorizontal: layout.gutter }, contentStyle]}>{children}</View>
          {footer ? (
            <View style={{ paddingHorizontal: layout.gutter, paddingTop: s[3], paddingBottom: s[4] + insets.bottom, borderTopWidth: 1, borderTopColor: c.border }}>{footer}</View>
          ) : (
            <View style={{ height: s[4] + insets.bottom }} />
          )}
        </Animated.View>
      </GestureHandlerRootView>
    </Modal>
  );
}

const styles = StyleSheet.create({
  sheet: { position: "absolute", left: 0, right: 0, overflow: "hidden" },
});
