"use client";

import { ScrollText, Tags, Undo2, Users } from "lucide-react";
import { usePathname } from "next/navigation";
import { ScrollTabs, type SectionLink } from "@/components/shell/SectionNav";

const LINKS: readonly SectionLink[] = [
  { href: "/admin/payments", label: "Customers", icon: Users, active: (p) => p === "/admin/payments" || p.startsWith("/admin/payments/customers") },
  { href: "/admin/payments/ledger", label: "Ledger", icon: ScrollText },
  { href: "/admin/payments/refunds", label: "Refunds", icon: Undo2 },
  { href: "/admin/payments/prices", label: "Prices", icon: Tags },
];

/** Customers · Ledger · Refunds · Prices, under the Payments admin tab. */
export function PaymentsAdminTabs() {
  const pathname = usePathname() ?? "/admin/payments";
  return <ScrollTabs links={LINKS} pathname={pathname} label="Payments" />;
}

