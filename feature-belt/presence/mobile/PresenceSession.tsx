import { generateId, useMe, usePresenceSession, type PresenceSubscribe } from "@scaffoldapp/shared";
import { useState } from "react";
import { AppState, Platform } from "react-native";

const isVisible = () => AppState.currentState === "active";

const onVisibilityChange: PresenceSubscribe = (listener) => {
  const sub = AppState.addEventListener("change", listener);
  return () => sub.remove();
};

/**
 * Session mount: heartbeats while the app is in the foreground (once consent is
 * accepted). A backgrounded app can't beat, so it reads as offline after the TTL; signing
 * out sends offline at once.
 */
export function PresenceSession() {
  const me = useMe();
  const [deviceId] = useState(() => `app-${generateId().replace(/-/g, "")}`.slice(0, 40));
  usePresenceSession({
    enabled: !!me.data && !me.data.consent.required,
    platform: Platform.OS === "android" ? "android" : "ios",
    deviceId,
    isVisible,
    onVisibilityChange,
    beatWhileHidden: false,
  });
  return null;
}
