"use client";

import { isPresenceDND, presenceLabel, useUserPresence, type Presence } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";

export interface PresenceDotProps {
  presence: Presence | null | undefined;
  /** `sm` 8 px (lists, avatars up to 32 px) · `md` 12 px. */
  size?: "sm" | "md";
  /** Draw offline as a hollow dot instead of nothing. */
  showOffline?: boolean;
  /** A ring in the surface colour, for a dot over an avatar. */
  ring?: boolean;
  className?: string;
}

/**
 * Online (filled), away (hollow), do not disturb (filled with a bar), offline (hidden, or
 * hollow and faint with `showOffline`). Shape tells the states apart, not only colour.
 */
export function PresenceDot({ presence, size = "sm", showOffline = false, ring = false, className }: PresenceDotProps) {
  const status = presence?.status ?? "offline";
  const dnd = isPresenceDND(presence);
  if (status === "offline" && !showOffline && !dnd) return null;
  const box = size === "md" ? "size-3" : "size-2";
  return (
    <span
      role="img"
      aria-label={presenceLabel(presence)}
      className={cn(
        "relative inline-flex shrink-0 items-center justify-center rounded-pill",
        box,
        ring && "ring-2 ring-surface",
        dnd ? "bg-danger" : status === "online" ? "bg-success" : status === "away" ? "border-2 border-fg-muted bg-surface" : "border-2 border-fg-subtle bg-surface",
        className,
      )}
    >
      {dnd && <span aria-hidden className={cn("h-px rounded-pill bg-danger-fg", size === "md" ? "w-1.5" : "w-1")} />}
    </span>
  );
}

/** `PresenceDot` for one user id (batched with every other one on screen). */
export function UserPresenceDot({ userId, ...props }: Omit<PresenceDotProps, "presence"> & { userId: string | null | undefined }) {
  const presence = useUserPresence(userId);
  return <PresenceDot presence={presence} {...props} />;
}
