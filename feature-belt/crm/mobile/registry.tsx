import { useActiveOrg, useCrmTaskSummary } from "@scaffoldapp/shared";
import { Handshake } from "lucide-react-native";
import type { HomeEntry } from "../registry";

/** Your overdue and due-today CRM tasks (reads the cached summary). */
function useCrmBadge(): number | undefined {
  const { org } = useActiveOrg();
  const s = useCrmTaskSummary(org?.id);
  return s.data ? s.data.overdue + s.data.today || undefined : undefined;
}

/** Home → CRM (search, tasks, the deals board, records). */
export const crmHome: HomeEntry = { id: "crm", title: "CRM", icon: Handshake, href: "/crm", useBadge: useCrmBadge };
