import { Info } from "lucide-react-native";
import { useRef, useState, type ReactNode } from "react";
import { AccessibilityInfo, Modal, Pressable, StyleSheet, View, useWindowDimensions } from "react-native";
import Animated from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { fadeIn, fadeOut } from "../lib/motion";
import { useTheme } from "../theme/ThemeProvider";
import { Text } from "./Text";

export interface InfoTipProps {
  /** The context a subtitle would have carried. Plain text where possible. */
  content: ReactNode;
  /** Accessible name. @default "More info" */
  label?: string;
  /** Icon size: `sm` 16, `md` 20. @default "sm" */
  size?: "sm" | "md";
  tone?: "muted" | "primary" | "inverse";
}

interface Anchor {
  x: number;
  y: number;
  w: number;
  h: number;
}

const GAP = 8;
const EDGE = 12;

/**
 * ⓘ that opens a bubble anchored to it; tapping anywhere dismisses it. The hit area
 * grows to 48 around the small icon, a selection haptic ticks on open, and screen
 * readers hear the content.
 */
export function InfoTip({ content, label = "More info", size = "sm", tone = "muted" }: InfoTipProps) {
  const theme = useTheme();
  const c = theme.colors;
  const ref = useRef<View>(null);
  const [anchor, setAnchor] = useState<Anchor | null>(null);
  const { width, height } = useWindowDimensions();
  const iconSize = size === "sm" ? 16 : 20;
  const pad = theme.space[0.5];
  const color = tone === "primary" ? c.primaryStrong : tone === "inverse" ? c.textInverse : c.textMuted;

  const open = () => {
    haptic.selection();
    ref.current?.measureInWindow((x, y, w, h) => setAnchor({ x, y, w, h }));
    if (typeof content === "string") AccessibilityInfo.announceForAccessibility(content);
  };
  const close = () => setAnchor(null);

  const bubbleW = Math.min(320, width - EDGE * 2);
  const above = anchor ? anchor.y > height * 0.45 : true;
  const centerX = anchor ? anchor.x + anchor.w / 2 : 0;
  const left = Math.max(EDGE, Math.min(width - EDGE - bubbleW, centerX - bubbleW / 2));
  const arrowLeft = Math.max(EDGE, Math.min(bubbleW - EDGE, centerX - left)) - 6;

  return (
    <>
      <Pressable
        ref={ref}
        onPress={open}
        hitSlop={Math.ceil((theme.tap.base - (iconSize + pad * 2)) / 2)}
        accessibilityRole="button"
        accessibilityLabel={label}
        accessibilityHint="Shows more information"
        style={{ padding: pad }}
      >
        <Info size={iconSize} color={anchor ? c.primaryStrong : color} strokeWidth={2.25} />
      </Pressable>
      <Modal visible={anchor !== null} transparent animationType="none" onRequestClose={close} statusBarTranslucent navigationBarTranslucent>
        <Pressable style={StyleSheet.absoluteFill} onPress={close} accessibilityLabel="Dismiss" />
        {anchor ? (
          <Animated.View
            entering={fadeIn(theme)}
            exiting={fadeOut(theme)}
            accessibilityLiveRegion="polite"
            style={[
              styles.bubble,
              {
                width: bubbleW,
                left,
                paddingHorizontal: theme.space[4],
                paddingVertical: theme.space[3],
                backgroundColor: c.inverse,
                borderRadius: theme.radius.md,
                boxShadow: theme.elevation.lg,
              },
              above ? { bottom: height - anchor.y + GAP } : { top: anchor.y + anchor.h + GAP },
            ]}
          >
            <View style={[styles.arrow, { left: arrowLeft, backgroundColor: c.inverse }, above ? { bottom: -6 } : { top: -6 }]} />
            {typeof content === "string" ? (
              <Text role="label" tone="inverse">
                {content}
              </Text>
            ) : (
              content
            )}
          </Animated.View>
        ) : null}
      </Modal>
    </>
  );
}

const styles = StyleSheet.create({
  bubble: { position: "absolute" },
  arrow: { position: "absolute", width: 12, height: 12, transform: [{ rotate: "45deg" }], borderRadius: 2 },
});
