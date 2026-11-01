"use client";

import { useIdentityBadge } from "@scaffoldapp/shared";
import { Tooltip, cn } from "@scaffoldapp/ui-web";
import { BadgeCheck } from "lucide-react";

/**
 * The verified mark next to a person's name. Pass `verified` when you already know it
 * (lists: `identity.VerifiedSet` on the server), or `userId` to read the public badge.
 * Renders nothing for unverified users.
 */
export function VerifiedBadge({ userId, verified, size = "md", className }: { userId?: string | null; verified?: boolean; size?: "sm" | "md"; className?: string }) {
  const badge = useIdentityBadge(verified === undefined ? userId : null);
  const on = verified ?? badge.data ?? false;
  if (!on) return null;
  return (
    <Tooltip content="Verified identity">
      <span role="img" aria-label="Verified identity" tabIndex={0} className={cn("inline-flex shrink-0 rounded-pill text-primary-strong outline-none focus-visible:ring-2 focus-visible:ring-ring", className)}>
        <BadgeCheck className={size === "sm" ? "size-4" : "size-5"} aria-hidden />
      </span>
    </Tooltip>
  );
}
