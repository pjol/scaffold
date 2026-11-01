"use client";

import { useUnreadNotificationCount } from "@scaffoldapp/shared";
import { Badge, Tooltip, badgeText, cn } from "@scaffoldapp/ui-web";
import { Bell } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

/** Top-bar bell (registry `headerActions`): opens the inbox; the unread count sits on it. */
export function NotificationBell() {
  const unread = useUnreadNotificationCount() ?? 0;
  const active = usePathname() === "/notifications";
  const label = unread > 0 ? `Inbox, ${unread} unread` : "Inbox";
  return (
    <Tooltip content="Inbox">
      <Link
        href="/notifications"
        aria-label={label}
        aria-current={active ? "page" : undefined}
        className={cn(
          "relative inline-flex size-control shrink-0 items-center justify-center rounded-pill text-fg transition-colors hover:bg-sunken [&_svg]:size-5",
          "before:absolute before:inset-0 before:content-[''] pointer-coarse:before:-inset-0.5",
          active && "bg-sunken",
        )}
      >
        <Bell aria-hidden />
        {unread > 0 && (
          <Badge tone="danger" variant="solid" pop key={unread} className="pointer-events-none absolute -top-1 -right-1" aria-hidden>
            {badgeText(unread)}
          </Badge>
        )}
      </Link>
    </Tooltip>
  );
}
