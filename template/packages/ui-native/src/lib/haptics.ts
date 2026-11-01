import * as Haptics from "expo-haptics";

function safe(fn: () => Promise<unknown>): void {
  try {
    void fn().catch(() => undefined);
  } catch {
    // Unsupported platform (simulator without a Taptic Engine): nothing to do.
  }
}

/** Fire-and-forget haptics that never throw. */
export const haptic = {
  /** Button press. */
  light: () => safe(() => Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light)),
  /** Heavier confirm (thresholds, long press). */
  medium: () => safe(() => Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium)),
  /** Tick for selection changes (tabs, segments, toggles, InfoTip). */
  selection: () => safe(() => Haptics.selectionAsync()),
  success: () => safe(() => Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success)),
  warning: () => safe(() => Haptics.notificationAsync(Haptics.NotificationFeedbackType.Warning)),
  error: () => safe(() => Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error)),
};
