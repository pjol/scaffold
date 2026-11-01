import type { NotificationsEndpoints } from "@scaffoldapp/shared";
import Constants from "expo-constants";
import * as Notifications from "expo-notifications";
import { Platform } from "react-native";
import { userStorage } from "@/lib/storage";

/** Where push stands on this device. `unavailable`: no EAS project id or no push support (simulator). */
export type PushPermission = "granted" | "undetermined" | "denied" | "unavailable";

const TOKEN_KEY = "notifications.push-token";
const ANDROID_CHANNEL = "default";

/** The EAS project id Expo push tokens are issued for (`eas init` writes it to app config). */
export function easProjectId(): string | null {
  const extra = Constants.expoConfig?.extra as { eas?: { projectId?: string } } | undefined;
  return extra?.eas?.projectId ?? Constants.easConfig?.projectId ?? null;
}

/** Android needs a channel before the permission prompt (Android 13+) and before any push shows. */
async function ensureChannel(): Promise<void> {
  if (Platform.OS !== "android") return;
  await Notifications.setNotificationChannelAsync(ANDROID_CHANNEL, {
    name: "Notifications",
    importance: Notifications.AndroidImportance.DEFAULT,
  });
}

/** Current permission without asking. */
export async function pushPermission(): Promise<PushPermission> {
  if (!easProjectId()) return "unavailable";
  try {
    const p = await Notifications.getPermissionsAsync();
    if (p.granted) return "granted";
    return p.canAskAgain ? "undetermined" : "denied";
  } catch {
    return "unavailable";
  }
}

/** Ask the OS (call only after the person chose to turn notifications on). */
export async function requestPushPermission(): Promise<PushPermission> {
  if (!easProjectId()) return "unavailable";
  await ensureChannel();
  const p = await Notifications.requestPermissionsAsync();
  if (p.granted) return "granted";
  return p.canAskAgain ? "undetermined" : "denied";
}

/**
 * With permission granted, fetch this device's Expo push token and register it for the
 * signed-in user (`POST /v1/devices`). Safe to call on every launch: the server upserts and
 * moves the token between accounts. Returns false when push isn't available here.
 */
export async function syncPushToken(api: NotificationsEndpoints): Promise<boolean> {
  const projectId = easProjectId();
  if (!projectId || Platform.OS === "web") return false;
  await ensureChannel();
  let token: string;
  try {
    token = (await Notifications.getExpoPushTokenAsync({ projectId })).data;
  } catch {
    return false; // simulators and builds without push credentials
  }
  await api.registerDevice({ token, platform: Platform.OS === "ios" ? "ios" : "android" });
  await userStorage.set(TOKEN_KEY, token);
  return true;
}

/** Unregister this device's token (sign-out and account deletion run this while the session still works). */
export async function unregisterPushToken(api: NotificationsEndpoints): Promise<void> {
  const token = await userStorage.get<string>(TOKEN_KEY);
  if (!token) return;
  try {
    await api.unregisterDevice(token);
  } finally {
    await userStorage.remove(TOKEN_KEY);
  }
}

/** The app path a tapped push opens (its `data.link`), or null. Only same-app paths. */
export function linkOf(response: Notifications.NotificationResponse): { link: string | null; id: string | null } {
  const data = response.notification.request.content.data as Record<string, unknown> | null | undefined;
  const link = typeof data?.["link"] === "string" ? data["link"] : null;
  const id = typeof data?.["notification_id"] === "string" ? data["notification_id"] : null;
  return { link: link && link.startsWith("/") && !link.startsWith("//") ? link : null, id };
}
