import { CircleCheck, CircleX, Info, TriangleAlert, X } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Pressable, View, type StyleProp, type ViewStyle } from "react-native";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import Animated, { FadeOutUp, LinearTransition, SlideInUp, useAnimatedStyle, useSharedValue, withSpring } from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { scheduleOnRN } from "react-native-worklets";
import { haptic } from "../lib/haptics";
import { renderIcon, type IconLike } from "../lib/icon";
import { layoutSpring } from "../lib/motion";
import { useLayout, useTheme } from "../theme/ThemeProvider";
import { Text } from "./Text";

export type ToastType = "success" | "error" | "warning" | "info" | "message";

export interface ToastOptions {
  /** Replaces a toast with the same id. */
  id?: string;
  type?: ToastType;
  /** ms; 0 = until dismissed. @default 4000 */
  duration?: number;
  icon?: IconLike;
  action?: { label: string; onPress: () => void };
}

interface ToastItem {
  id: string;
  title: string;
  type: ToastType;
  duration: number;
  icon?: IconLike;
  action?: { label: string; onPress: () => void };
}

type Listener = (items: ToastItem[]) => void;
let items: ToastItem[] = [];
const listeners = new Set<Listener>();
let seq = 0;
const emit = () => listeners.forEach((l) => l(items));

function show(title: string, opts: ToastOptions = {}): string {
  const id = opts.id ?? `t${++seq}`;
  const item: ToastItem = { id, title, type: opts.type ?? "message", duration: opts.duration ?? 4000, ...(opts.icon ? { icon: opts.icon } : {}), ...(opts.action ? { action: opts.action } : {}) };
  items = [...items.filter((t) => t.id !== id), item].slice(-3);
  emit();
  return id;
}

/**
 * Imperative toasts, callable from anywhere (mutations, sockets): `toast.success("Saved")`,
 * `toast.error(errorMessage(e))`, `toast.dismiss(id)`. One sentence, meaning first.
 * Needs one `<ToastHost />` near the root.
 */
export const toast = Object.assign((title: string, opts?: ToastOptions) => show(title, opts), {
  success: (title: string, opts?: Omit<ToastOptions, "type">) => show(title, { ...opts, type: "success" }),
  error: (title: string, opts?: Omit<ToastOptions, "type">) => show(title, { ...opts, type: "error" }),
  warning: (title: string, opts?: Omit<ToastOptions, "type">) => show(title, { ...opts, type: "warning" }),
  info: (title: string, opts?: Omit<ToastOptions, "type">) => show(title, { ...opts, type: "info" }),
  dismiss: (id?: string) => {
    items = id ? items.filter((t) => t.id !== id) : [];
    emit();
  },
});

function ToastCard({ item }: { item: ToastItem }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const y = useSharedValue(0);
  const dismiss = () => toast.dismiss(item.id);
  useEffect(() => {
    if (item.type === "success") haptic.success();
    else if (item.type === "error") haptic.error();
    else if (item.type === "warning") haptic.warning();
    if (!item.duration) return undefined;
    const t = setTimeout(dismiss, item.duration);
    return () => clearTimeout(t);
    // One timer per toast id.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item.id]);
  const pan = Gesture.Pan()
    .onUpdate((e) => {
      y.value = Math.min(0, e.translationY);
    })
    .onEnd((e) => {
      if (e.translationY < -32 || e.velocityY < -500) scheduleOnRN(dismiss);
      else y.value = withSpring(0, theme.motion.spring.snappy);
    });
  const drag = useAnimatedStyle(() => ({ transform: [{ translateY: y.value }] }));
  const meta: Record<ToastType, { icon: IconLike; color: string }> = {
    success: { icon: CircleCheck, color: c.successStrong },
    error: { icon: CircleX, color: c.dangerStrong },
    warning: { icon: TriangleAlert, color: c.warningStrong },
    info: { icon: Info, color: c.infoStrong },
    message: { icon: null, color: c.text },
  };
  const m = meta[item.type];
  const sp = theme.motion.spring.snappy;
  return (
    <GestureDetector gesture={pan}>
      <Animated.View
        entering={SlideInUp.springify().stiffness(sp.stiffness).damping(sp.damping).mass(sp.mass)}
        exiting={FadeOutUp.duration(theme.motion.duration.fast)}
        layout={layoutSpring(theme)}
        accessibilityLiveRegion="polite"
        accessibilityRole="alert"
        style={[
          {
            flexDirection: "row",
            alignItems: "center",
            gap: s[3],
            minHeight: theme.tap.lg,
            paddingHorizontal: s[4],
            paddingVertical: s[3],
            borderRadius: theme.radius.lg,
            backgroundColor: c.surfaceRaised,
            borderWidth: 1,
            borderColor: c.border,
            boxShadow: theme.elevation.lg,
          },
          drag,
        ]}
      >
        {renderIcon(item.icon ?? m.icon, { size: 22, color: m.color })}
        <Text weight={500} style={{ flex: 1 }}>
          {item.title}
        </Text>
        {item.action ? (
          <Pressable
            accessibilityRole="button"
            onPress={() => {
              item.action?.onPress();
              dismiss();
            }}
            hitSlop={s[2]}
            style={{ paddingHorizontal: s[3], paddingVertical: s[2], borderRadius: theme.radius.md, backgroundColor: c.primarySoft }}
          >
            <Text role="label" weight={600} tone="primary">
              {item.action.label}
            </Text>
          </Pressable>
        ) : null}
      </Animated.View>
    </GestureDetector>
  );
}

/** Renders toasts at the top, under the status bar. Mount once inside GestureHandlerRootView + ThemeProvider. */
export function ToastHost() {
  const insets = useSafeAreaInsets();
  const layout = useLayout();
  const theme = useTheme();
  const [list, setList] = useState<ToastItem[]>(items);
  useEffect(() => {
    listeners.add(setList);
    return () => {
      listeners.delete(setList);
    };
  }, []);
  return (
    <View pointerEvents="box-none" style={{ position: "absolute", top: insets.top + theme.space[2], left: layout.gutter, right: layout.gutter, gap: theme.space[2], zIndex: 1400 }}>
      {list.map((t) => (
        <ToastCard key={t.id} item={t} />
      ))}
    </View>
  );
}

export interface BannerProps {
  tone?: "info" | "success" | "warning" | "danger" | "primary";
  title: string;
  icon?: IconLike;
  /** One action ("Update"). */
  action?: { label: string; onPress: () => void };
  onClose?: () => void;
  style?: StyleProp<ViewStyle>;
}

/** Inline status line on a tinted surface ("Update the app to read the latest terms"). */
export function Banner({ tone = "info", title, icon, action, onClose, style }: BannerProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const t = {
    info: { bg: c.infoSoft, fg: c.infoStrong, icon: Info },
    success: { bg: c.successSoft, fg: c.successStrong, icon: CircleCheck },
    warning: { bg: c.warningSoft, fg: c.warningStrong, icon: TriangleAlert },
    danger: { bg: c.dangerSoft, fg: c.dangerStrong, icon: CircleX },
    primary: { bg: c.primarySoft, fg: c.primaryStrong, icon: Info },
  }[tone];
  return (
    <Animated.View
      layout={LinearTransition.springify().stiffness(theme.motion.spring.gentle.stiffness).damping(theme.motion.spring.gentle.damping).mass(theme.motion.spring.gentle.mass)}
      accessibilityRole="alert"
      style={[{ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.base, paddingHorizontal: s[4], paddingVertical: s[3], borderRadius: theme.radius.lg, backgroundColor: t.bg }, style]}
    >
      {renderIcon(icon ?? t.icon, { size: 20, color: t.fg })}
      <Text weight={500} style={{ flex: 1 }}>
        {title}
      </Text>
      {action ? (
        <Pressable accessibilityRole="button" onPress={action.onPress} hitSlop={s[2]} style={{ paddingHorizontal: s[3], paddingVertical: s[2], borderRadius: theme.radius.md, backgroundColor: c.surface }}>
          <Text role="label" weight={600} style={{ color: t.fg }}>
            {action.label}
          </Text>
        </Pressable>
      ) : null}
      {onClose ? (
        <Pressable onPress={onClose} hitSlop={s[3]} accessibilityRole="button" accessibilityLabel="Dismiss">
          <X size={20} color={t.fg} />
        </Pressable>
      ) : null}
    </Animated.View>
  );
}
