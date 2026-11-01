import { CircleAlert } from "lucide-react-native";
import { useEffect, useState, type ReactNode } from "react";
import { KeyboardAvoidingView, Modal, Pressable, StyleSheet, View, useWindowDimensions } from "react-native";
import Animated, { useAnimatedStyle, useSharedValue, withSpring, withTiming } from "react-native-reanimated";
import { scheduleOnRN } from "react-native-worklets";
import { haptic } from "../lib/haptics";
import { enterUp, fadeOut } from "../lib/motion";
import { useTheme } from "../theme/ThemeProvider";
import { Button } from "./Button";
import { InfoTip } from "./InfoTip";
import { Text } from "./Text";

export interface DialogProps {
  open: boolean;
  onClose: () => void;
  /** Short title. No description: use `info` or the body. */
  title: string;
  info?: ReactNode;
  children?: ReactNode;
  /** Actions, stacked full width (primary first). */
  footer?: ReactNode;
  /** Tap outside / back button closes. @default true */
  dismissible?: boolean;
}

/** Centered modal card (max 448) with a spring scale-in. Padding 24 (20 under 400 wide); title→body 16, body→actions 24. */
export function Dialog({ open, onClose, title, info, children, footer, dismissible = true }: DialogProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const { width } = useWindowDimensions();
  const [rendered, setRendered] = useState(open);
  const p = useSharedValue(0);
  useEffect(() => {
    if (open) {
      setRendered(true);
      p.value = withSpring(1, theme.motion.spring.gentle);
    } else if (rendered) {
      p.value = withTiming(0, { duration: theme.motion.duration.fast }, (done) => {
        if (done) scheduleOnRN(setRendered, false);
      });
    }
    // Only open/close drive the animation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const backdrop = useAnimatedStyle(() => ({ opacity: Math.min(1, p.value) }));
  const card = useAnimatedStyle(() => ({ opacity: Math.min(1, p.value * 1.4), transform: [{ scale: 0.96 + 0.04 * p.value }, { translateY: (1 - p.value) * 8 }] }));
  if (!rendered) return null;
  const pad = width < 400 ? s[5] : s[6];
  return (
    <Modal visible transparent animationType="none" statusBarTranslucent navigationBarTranslucent onRequestClose={() => dismissible && onClose()}>
      <Animated.View style={[StyleSheet.absoluteFill, { backgroundColor: c.overlay }, backdrop]}>
        <Pressable style={StyleSheet.absoluteFill} onPress={() => dismissible && onClose()} accessibilityLabel="Close" />
      </Animated.View>
      {/* Lifts the card above the keyboard when it holds a field (type-to-confirm). */}
      <KeyboardAvoidingView behavior="padding" style={[styles.center, { padding: s[4] }]} pointerEvents="box-none">
        <Animated.View accessibilityViewIsModal style={[{ width: "100%", maxWidth: 448, padding: pad, backgroundColor: c.surfaceRaised, borderRadius: theme.radius.xl, boxShadow: theme.elevation.xl }, card]}>
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], flexWrap: "wrap" }}>
            <Text role="titleSm" accessibilityRole="header" style={{ flexShrink: 1 }}>
              {title}
            </Text>
            {info ? <InfoTip content={info} size="md" /> : null}
          </View>
          {children ? <View style={{ marginTop: s[4] }}>{children}</View> : null}
          {footer ? <View style={{ marginTop: s[6], gap: s[3] }}>{footer}</View> : null}
        </Animated.View>
      </KeyboardAvoidingView>
    </Modal>
  );
}

export interface ConfirmDialogProps {
  open: boolean;
  onClose: () => void;
  /** The question, naming the object: "Delete this post?" */
  title: string;
  info?: ReactNode;
  /** The consequence, plainly ("It can't be undone."), or a type-to-confirm field. */
  children?: ReactNode;
  /** A verb naming the outcome. @default "Confirm" */
  confirmLabel?: string;
  /** @default "Cancel" */
  cancelLabel?: string;
  tone?: "primary" | "danger";
  /** Keep the confirm button disabled (e.g. until DELETE is typed). */
  confirmDisabled?: boolean;
  /**
   * May return a promise: the button spins until it settles and the dialog closes on
   * success. A throw keeps the dialog open and shows the error above the actions.
   */
  onConfirm: () => void | Promise<unknown>;
  /** Copy for a failed `onConfirm`. @default the error's message */
  errorText?: string | ((error: unknown) => string);
  onError?: (error: unknown) => void;
}

function confirmErrorText(error: unknown, errorText: ConfirmDialogProps["errorText"]): string {
  if (typeof errorText === "function") return errorText(error);
  if (errorText !== undefined) return errorText;
  if (typeof error === "object" && error && "message" in error && typeof (error as { message: unknown }).message === "string" && (error as { message: string }).message) {
    return (error as { message: string }).message;
  }
  return "That didn't go through. Try again.";
}

/** Confirmation with an async-aware confirm button. Destructive confirmations use `tone="danger"`. */
export function ConfirmDialog({ open, onClose, title, info, children, confirmLabel = "Confirm", cancelLabel = "Cancel", tone = "primary", confirmDisabled, onConfirm, errorText, onError }: ConfirmDialogProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (open) setError(null);
  }, [open]);
  return (
    <Dialog
      open={open}
      onClose={() => !busy && onClose()}
      title={title}
      info={info}
      dismissible={!busy}
      footer={
        <>
          <Button
            variant={tone}
            fullWidth
            loading={busy}
            disabled={confirmDisabled}
            onPress={async () => {
              setError(null);
              setBusy(true);
              try {
                await onConfirm();
              } catch (e) {
                setBusy(false);
                haptic.error();
                setError(confirmErrorText(e, errorText));
                onError?.(e);
                return;
              }
              setBusy(false);
              onClose();
            }}
          >
            {confirmLabel}
          </Button>
          <Button variant="ghost" fullWidth disabled={busy} onPress={onClose}>
            {cancelLabel}
          </Button>
        </>
      }
    >
      {children || error ? (
        <View style={{ gap: s[3] }}>
          {children}
          {error ? (
            <Animated.View
              entering={enterUp(theme)}
              exiting={fadeOut(theme)}
              accessibilityRole="alert"
              accessibilityLiveRegion="assertive"
              style={{ flexDirection: "row", alignItems: "flex-start", gap: s[2], paddingHorizontal: s[3], paddingVertical: s[3], borderRadius: theme.radius.md, backgroundColor: c.dangerSoft }}
            >
              <CircleAlert size={20} color={c.dangerStrong} strokeWidth={2.25} />
              <Text role="label" weight={600} tone="danger" style={{ flex: 1 }}>
                {error}
              </Text>
            </Animated.View>
          ) : null}
        </View>
      ) : null}
    </Dialog>
  );
}

const styles = StyleSheet.create({
  center: { flex: 1, alignItems: "center", justifyContent: "center" },
});
