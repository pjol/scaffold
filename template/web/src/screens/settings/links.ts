"use client";

import { useMe } from "@scaffoldapp/shared";
import { LifeBuoy, Scale, ShieldCheck, SunMoon, UserCog, UserRound } from "lucide-react";
import type { SectionLink, SectionLinkGroup } from "@/components/shell/SectionNav";
import { settingsSections, type SettingsGroup } from "@/features/registry";

/** Settings headings in order. "You" also holds Profile, Appearance and Account. */
const GROUPS: readonly { id: SettingsGroup; label: string }[] = [
  { id: "you", label: "You" },
  { id: "workspace", label: "Workspace" },
  { id: "agents", label: "AI agents" },
  { id: "developer", label: "Developer" },
];

const PROFILE: SectionLink = { href: "/settings/profile", label: "Profile", icon: UserRound, active: (p) => p === "/settings" || p.startsWith("/settings/profile") };
const APPEARANCE: SectionLink = { href: "/settings/appearance", label: "Appearance", icon: SunMoon };
const ACCOUNT: SectionLink = { href: "/settings/account", label: "Account", icon: UserCog };
const LEGAL: SectionLink = { href: "/settings/legal", label: "Legal", icon: Scale };
const SUPPORT: SectionLink = { href: "/settings/support", label: "Support", icon: LifeBuoy };
const ADMIN: SectionLink = { href: "/admin", label: "Admin", icon: ShieldCheck };

/**
 * While no registry section has a `group`: one flat list, Profile · Appearance · (feature
 * sections) · Account · Legal · Support. Once any has one: You (Profile, Appearance, its
 * sections, Account) · Workspace · AI agents · Developer · Other (sections without a
 * group), then Legal and Support without a heading. Empty groups are left out.
 */
const GROUPED: readonly SectionLinkGroup[] = settingsSections.some((s) => s.group)
  ? [
      ...GROUPS.map(({ id, label }) => {
        const own = settingsSections.filter((s) => s.group === id);
        return { id, label, links: id === "you" ? [PROFILE, APPEARANCE, ...own, ACCOUNT] : own };
      }),
      { id: "other", label: "Other", links: settingsSections.filter((s) => !s.group) },
      { id: "help", links: [LEGAL, SUPPORT] },
    ].filter((g) => g.links.length > 0)
  : [{ id: "all", links: [PROFILE, APPEARANCE, ...settingsSections, ACCOUNT, LEGAL, SUPPORT] }];

const SECTIONS: readonly SectionLink[] = GROUPED.flatMap((g) => g.links);

/**
 * Settings sections in design.md order, as `groups` (headed when the registry uses groups)
 * and as one flat list (`sections`, same order). Admin is separate: staff only.
 */
export function useSettingsLinks(): { sections: readonly SectionLink[]; groups: readonly SectionLinkGroup[]; admin: SectionLink | null } {
  const me = useMe();
  return { sections: SECTIONS, groups: GROUPED, admin: me.data?.admin_role ? ADMIN : null };
}
