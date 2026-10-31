import { useUnreadNotificationCount } from "@scaffoldapp/shared";
import { Bell, BellRing } from "lucide-react-native";
import type { HomeEntry, SettingsSection } from "../registry";
import { usePushLifecycle } from "./lifecycle";

/** Session mount (registry `sessionMounts`): token sync, sign-out unregister, push taps, icon badge, foreground toasts. */
function PushLifecycle(): null {
  usePushLifecycle();
  return null;
}

export const notificationsSession = PushLifecycle;

export const notificationsHome: HomeEntry = { id: "notifications", title: "Inbox", icon: Bell, href: "/notifications", useBadge: useUnreadNotificationCount };

export const notificationsSettings: SettingsSection = {
  id: "notifications",
  items: [{ id: "notifications", title: "Notifications", icon: BellRing, href: "/settings/notifications" }],
};
