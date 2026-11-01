import { LayoutDashboard, ScrollText, Users } from "lucide-react";
import type { SectionLink } from "@/components/shell/SectionNav";
import { adminNav } from "@/features/registry";

/** Admin tabs: Overview · Users · Audit log, then the registry's `adminNav`. */
export const ADMIN_LINKS: readonly SectionLink[] = [
  { href: "/admin", label: "Overview", icon: LayoutDashboard, active: (p) => p === "/admin" },
  { href: "/admin/users", label: "Users", icon: Users },
  { href: "/admin/audit", label: "Audit log", icon: ScrollText },
  ...adminNav.map(
    (item): SectionLink => ({
      href: item.href,
      label: item.label,
      icon: item.icon,
      ...(item.match === "exact" ? { active: (p: string) => p === item.href } : {}),
      ...(item.useBadge ? { useBadge: item.useBadge } : {}),
    }),
  ),
];
