"use client";

import { useMarkNotificationsRead, useRealtimeEvent } from "@scaffoldapp/shared";
import { toast } from "@scaffoldapp/ui-web";
import { usePathname, useRouter } from "next/navigation";

/** In-app links only: an app path, never another origin. */
export function safeAppPath(link: string | undefined | null): string | null {
  return link && link.startsWith("/") && !link.startsWith("//") ? link : null;
}

/** Registry `sessionMounts`: foreground toasts for the signed-in session. Renders nothing. */
export function NotificationToasts(): null {
  useNotificationToasts();
  return null;
}

/**
 * A toast for each new notification while the app is open (not on the inbox, which shows
 * it already). "Open" marks it read and follows its link.
 */
export function useNotificationToasts(): void {
  const pathname = usePathname();
  const router = useRouter();
  const markRead = useMarkNotificationsRead();
  useRealtimeEvent("notification", (e) => {
    if (pathname === "/notifications" || document.visibilityState !== "visible") return;
    const data = e.data as { id?: string; title: string; link?: string };
    const path = safeAppPath(data.link);
    toast(data.title, {
      id: data.id,
      action: path
        ? {
            label: "Open",
            onClick: () => {
              if (data.id) markRead.mutate({ ids: [data.id] });
              router.push(path);
            },
          }
        : undefined,
    });
  });
}
