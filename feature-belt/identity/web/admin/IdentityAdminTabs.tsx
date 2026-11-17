"use client";

import { Award, FileCheck2, IdCard } from "lucide-react";
import { usePathname } from "next/navigation";
import { ScrollTabs, type SectionLink } from "@/components/shell/SectionNav";

const isBadges = (p: string) => p.startsWith("/admin/identity/badges");
const isApplications = (p: string) => p.startsWith("/admin/identity/applications");

const LINKS: readonly SectionLink[] = [
  { href: "/admin/identity", label: "ID checks", icon: IdCard, active: (p) => p.startsWith("/admin/identity") && !isBadges(p) && !isApplications(p) },
  { href: "/admin/identity/applications", label: "Badge applications", icon: FileCheck2 },
  { href: "/admin/identity/badges", label: "Badges", icon: Award },
];

/** ID checks · Badge applications · Badges, under the Verification admin tab. */
export function IdentityAdminTabs() {
  const pathname = usePathname() ?? "/admin/identity";
  return <ScrollTabs links={LINKS} pathname={pathname} label="Verification" />;
}
