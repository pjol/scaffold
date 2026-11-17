"use client";

import { useIdentityUserBadge, type IdentityHeldBadge } from "@scaffoldapp/shared";
import { Tooltip, cn } from "@scaffoldapp/ui-web";
import { BadgeCheck } from "lucide-react";
import { IDENTITY_BADGE_MEANING, IdentityBadgeMark, identityBadgeName } from "./BadgeMark";

/**
 * The mark next to a person's name: their highest verification badge (official, org, notable
 * or paid, each with its own mark), else the ID-verified check. Pass what you already know
 * (lists: `identity.BadgeSet` and `identity.VerifiedSet` on the server): `badge` and/or
 * `verified`; or `userId` to read the public badge. Renders nothing when there's nothing to show.
 */
export function VerifiedBadge({
  userId,
  verified,
  badge,
  size = "md",
  showLabel = false,
  className,
}: {
  userId?: string | null;
  verified?: boolean;
  badge?: IdentityHeldBadge | null;
  size?: "sm" | "md";
  /** Show the badge's label after the mark ("Riverside Bikes"). */
  showLabel?: boolean;
  className?: string;
}) {
  const known = verified !== undefined || badge !== undefined;
  const pub = useIdentityUserBadge(known ? null : userId);
  const isVerified = verified ?? pub.data?.verified ?? false;
  const held = badge !== undefined ? badge : (pub.data?.badge ?? null);
  if (!held && !isVerified) return null;
  const px = size === "sm" ? "size-4" : "size-5";
  const name = held ? identityBadgeName(held) : "Verified identity";
  return (
    <Tooltip
      content={
        <span className="flex max-w-64 flex-col gap-1">
          <span className="font-semibold">{name}</span>
          {held ? <span>{IDENTITY_BADGE_MEANING[held.kind]}</span> : null}
          {held && isVerified ? <span>Identity verified.</span> : null}
        </span>
      }
    >
      <span role="img" aria-label={name} tabIndex={0} className={cn("inline-flex shrink-0 items-center gap-1 rounded-pill outline-none focus-visible:ring-2 focus-visible:ring-ring", className)}>
        {held ? <IdentityBadgeMark kind={held.kind} className={px} /> : <BadgeCheck className={cn(px, "text-primary-strong")} aria-hidden />}
        {showLabel && held ? <span className="max-w-40 truncate type-caption text-fg-muted">{held.label}</span> : null}
      </span>
    </Tooltip>
  );
}
