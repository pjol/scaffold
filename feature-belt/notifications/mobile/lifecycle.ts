import { notificationsEndpoints, useEndpoints, useMarkNotificationsRead, useRealtimeEvent, useUnreadNotificationCount } from "@scaffoldapp/shared";
import { toast } from "@scaffoldapp/ui-native";
import * as Notifications from "expo-notifications";
import { router, usePathname, type Href } from "expo-router";
import { useCallback, useEffect, useRef, useState } from "react";
import { AppState } from "react-native";
import { registerSignOutHook } from "@/lib/session";
import { linkOf, pushPermission, requestPushPermission, syncPushToken, unregisterPushToken, type PushPermission } from "./push";

// In the foreground the app shows its own toast (from the realtime event); the OS banner is
// for when the app is in the background.
Notifications.setNotificationHandler({
  handleNotification: async () => {
    const foreground = AppState.currentState === "active";
    return { shouldShowBanner: !foreground, shouldShowList: true, shouldPlaySound: !foreground, shouldSetBadge: true };
  },
});

let handledResponse: string | null = null;

/**
 * Push for the signed-in user, mounted once per session (registry `sessionMounts`): keeps
 * this device's token registered when permission is already granted (never prompts),
 * unregisters it on sign-out, opens tapped pushes, mirrors the unread count on the app
 * icon, and toasts new notifications while the app is open.
 */
export function usePushLifecycle(): void {
  const api = useEndpoints(notificationsEndpoints);
  const markRead = useMarkNotificationsRead();
  const unread = useUnreadNotificationCount();
  const pathname = usePathname();
  const markReadRef = useRef(markRead.mutate);
  markReadRef.current = markRead.mutate;

  useEffect(() => {
    void pushPermission().then((p) => (p === "granted" ? syncPushToken(api).catch(() => false) : false));
    return registerSignOutHook(() => unregisterPushToken(api));
  }, [api]);

  useEffect(() => {
    const open = (response: Notifications.NotificationResponse) => {
      const key = response.notification.request.identifier;
      if (handledResponse === key) return;
      handledResponse = key;
      const { link, id } = linkOf(response);
      if (id) markReadRef.current({ ids: [id] });
      if (link) router.push(link as Href);
    };
    const last = Notifications.getLastNotificationResponse();
    if (last) open(last);
    const sub = Notifications.addNotificationResponseReceivedListener(open);
    return () => sub.remove();
  }, []);

  useEffect(() => {
    if (unread !== undefined) void Notifications.setBadgeCountAsync(unread).catch(() => undefined);
  }, [unread]);

  useRealtimeEvent("notification", (e) => {
    if (pathname === "/notifications" || AppState.currentState !== "active") return;
    const data = e.data as { id?: string; title: string; link?: string };
    const link = data.link && data.link.startsWith("/") && !data.link.startsWith("//") ? data.link : null;
    toast.info(data.title, {
      ...(link
        ? {
            action: {
              label: "Open",
              onPress: () => {
                if (data.id) markReadRef.current({ ids: [data.id] });
                router.push(link as Href);
              },
            },
          }
        : {}),
    });
  });
}

/** Permission state, refreshed when the app returns to the foreground (people change it in Settings). */
export function usePushPermission(): { permission: PushPermission | null; enable: () => Promise<PushPermission>; busy: boolean } {
  const api = useEndpoints(notificationsEndpoints);
  const [permission, setPermission] = useState<PushPermission | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    const refresh = () => void pushPermission().then((p) => alive && setPermission(p));
    refresh();
    const sub = AppState.addEventListener("change", (s) => s === "active" && refresh());
    return () => {
      alive = false;
      sub.remove();
    };
  }, []);

  const enable = useCallback(async () => {
    setBusy(true);
    try {
      const p = await requestPushPermission();
      if (p === "granted" && !(await syncPushToken(api))) {
        setPermission("unavailable");
        return "unavailable";
      }
      setPermission(p);
      return p;
    } finally {
      setBusy(false);
    }
  }, [api]);

  return { permission, enable, busy };
}
