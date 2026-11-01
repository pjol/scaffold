import { useSupportStats } from "@scaffoldapp/shared";
import { LifeBuoy, MessagesSquare } from "lucide-react";
import type { NavItem, SettingsSection, SupportLink } from "../registry";

/** Open requests, for the Support admin tab badge (cached 30 s; realtime refreshes it). */
function useOpenTicketCount(): number | undefined {
  return useSupportStats().data?.open;
}

/** Settings → Support requests: the user's tickets, a thread per ticket, and the new-request form. */
export const supportSettings: SettingsSection = { href: "/settings/support-requests", label: "Support requests", icon: MessagesSquare };

/** Admin → Support: the ticket queue, badged with open requests. */
export const supportAdminNav: NavItem = { href: "/admin/support", label: "Support", icon: LifeBuoy, useBadge: useOpenTicketCount };

/** Shown on the consent screen, the suspended banner, the blocked screen and Settings → Support. */
export const supportRequestsLink: SupportLink = { id: "support-requests", href: "/settings/support-requests", label: "Support requests", icon: MessagesSquare };

/** App paths (the backend links to the same ones in email and notifications). */
export const supportPaths = {
  list: "/settings/support-requests",
  create: "/settings/support-requests/new",
  ticket: (id: string) => `/settings/support-requests/${id}`,
  admin: "/admin/support",
  adminTicket: (id: string) => `/admin/support/${id}`,
} as const;
