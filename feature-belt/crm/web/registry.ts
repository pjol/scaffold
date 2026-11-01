import { useActiveOrg, useCrmTaskSummary } from "@scaffoldapp/shared";
import { Handshake } from "lucide-react";
import type { HomeTile, NavItem } from "../registry";

/** Your overdue and due-today CRM tasks (reads the cached summary). */
function useCrmBadge(): number | undefined {
  const { org } = useActiveOrg();
  const s = useCrmTaskSummary(org?.id);
  return s.data ? s.data.overdue + s.data.today || undefined : undefined;
}

/** Main nav: the CRM (badge: tasks overdue or due today). */
export const crmNav: NavItem = { href: "/crm", label: "CRM", icon: Handshake, useBadge: useCrmBadge };

/** Home tile: the same destination (a separate name: `scaffold add` skips a line already in the file). */
export const crmHomeTile: HomeTile = crmNav;
