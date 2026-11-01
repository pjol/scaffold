"use client";

import { generateId, useMe, usePresenceSession, type PresenceSubscribe } from "@scaffoldapp/shared";
import { useState } from "react";
import { PresenceDialogs } from "./StatusDialogs";

const isVisible = () => document.visibilityState === "visible";

const onVisibilityChange: PresenceSubscribe = (listener) => {
  document.addEventListener("visibilitychange", listener);
  return () => document.removeEventListener("visibilitychange", listener);
};

const ACTIVITY_EVENTS = ["pointerdown", "pointermove", "keydown", "wheel", "touchstart", "focus"] as const;

/** Input on the page, throttled to one call every 5 seconds. */
const onActivity: PresenceSubscribe = (listener) => {
  let last = 0;
  const handler = () => {
    const now = Date.now();
    if (now - last < 5_000) return;
    last = now;
    listener();
  };
  for (const e of ACTIVITY_EVENTS) window.addEventListener(e, handler, { passive: true });
  return () => {
    for (const e of ACTIVITY_EVENTS) window.removeEventListener(e, handler);
  };
};

/**
 * Session mount (registry `sessionMounts`): heartbeats for this tab once consent is
 * accepted (a hidden tab or 10 idle minutes reads as away; closing it as offline after the
 * TTL), live presence for everyone you can see, and the status dialogs.
 */
export function PresenceSession() {
  const me = useMe();
  const [deviceId] = useState(() => `web-${generateId().replace(/-/g, "")}`.slice(0, 40));
  usePresenceSession({
    enabled: !!me.data && !me.data.consent.required,
    platform: "web",
    deviceId,
    isVisible,
    onVisibilityChange,
    onActivity,
  });
  return <PresenceDialogs />;
}
