"use client";

import { useMe } from "@scaffoldapp/shared";
import { LifeBuoy, Scale, ShieldCheck, SunMoon, UserCog, UserRound } from "lucide-react";
import type { SectionLink } from "@/components/shell/SectionNav";
import { settingsSections } from "@/features/registry";

/**
 * Settings sections in design.md order: Profile · Appearance · (feature sections, e.g.
 * Notifications) · Account · Legal · Support, then Admin for staff.
 */
export function useSettingsLinks(): { sections: SectionLink[]; admin: SectionLink | null } {
  const me = useMe();
  const sections: SectionLink[] = [
    { href: "/settings/profile", label: "Profile", icon: UserRound, active: (p) => p === "/settings" || p.startsWith("/settings/profile") },
    { href: "/settings/appearance", label: "Appearance", icon: SunMoon },
    ...settingsSections,
    { href: "/settings/account", label: "Account", icon: UserCog },
    { href: "/settings/legal", label: "Legal", icon: Scale },
    { href: "/settings/support", label: "Support", icon: LifeBuoy },
  ];
  const admin = me.data?.admin_role ? { href: "/admin", label: "Admin", icon: ShieldCheck } : null;
  return { sections, admin };
}
