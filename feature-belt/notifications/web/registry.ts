import { useUnreadNotificationCount } from "@scaffoldapp/shared";
import { Bell, BellRing } from "lucide-react";
import type { HeaderAction, HomeTile, NavItem, SessionMount, SettingsSection } from "../registry";
import { NotificationBell } from "./NotificationBell";
import { NotificationToasts } from "./toasts";

/** Top-bar bell with the unread badge; opens /notifications. */
export const notificationsBell: HeaderAction = { id: "notifications", Component: NotificationBell };

/** Foreground toasts while signed in. */
export const notificationsSession: SessionMount = NotificationToasts;

/** Home tile for the inbox, badged with the unread count (a cache read). */
export const notificationsHome: HomeTile = { href: "/notifications", label: "Inbox", icon: Bell, useBadge: useUnreadNotificationCount };

/** Settings → Notifications (/settings/notifications; notification emails link here). */
export const notificationsSettings: SettingsSection = { href: "/settings/notifications", label: "Notifications", icon: BellRing };

/** Admin → Notifications: delivery stats and test sends. */
export const notificationsAdminNav: NavItem = { href: "/admin/notifications", label: "Notifications", icon: BellRing };
