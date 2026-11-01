"use client";

import { useUserPresence } from "@scaffoldapp/shared";
import type { AvatarProps } from "@scaffoldapp/ui-web";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { PresenceDot } from "./PresenceDot";

export interface PresenceAvatarProps extends Omit<AvatarProps, "renderImage"> {
  userId: string | null | undefined;
  eager?: boolean;
  /** Show a hollow dot while offline. */
  showOffline?: boolean;
}

/** `AppAvatar` with the person's presence dot at the bottom right. */
export function PresenceAvatar({ userId, size = "md", showOffline, eager, ...props }: PresenceAvatarProps) {
  const presence = useUserPresence(userId);
  const big = size === "lg" || size === "xl" || size === "2xl";
  return (
    <span className="relative inline-flex shrink-0">
      <AppAvatar size={size} eager={eager} {...props} />
      <span className="absolute -bottom-0.5 -right-0.5 flex">
        <PresenceDot presence={presence} size={big ? "md" : "sm"} ring showOffline={showOffline} />
      </span>
    </span>
  );
}
