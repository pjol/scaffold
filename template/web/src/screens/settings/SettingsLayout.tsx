"use client";

import { ChevronLeft } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { SideTabs } from "@/components/shell/SectionNav";
import { useSettingsLinks } from "./links";

/** Side tabs at ≥ 1024 px (under group headings when the registry uses groups); on phones /settings is a list and each section has a back link. */
export function SettingsLayout({ children }: { children: ReactNode }) {
  const pathname = usePathname() ?? "/settings";
  const { groups, admin } = useSettingsLinks();
  const isIndex = pathname === "/settings";
  // Admin (staff) closes the last group: after Support.
  const tabs = admin ? groups.map((g, i) => (i === groups.length - 1 ? { ...g, links: [...g.links, admin] } : g)) : groups;
  return (
    <div className="mx-auto w-full max-w-content px-gutter py-section lg:grid lg:grid-cols-[14rem_minmax(0,1fr)] lg:gap-12">
      <div className="hidden lg:block">
        <SideTabs
          groups={tabs}
          pathname={pathname}
          label="Settings"
          className="sticky top-24 -m-1 max-h-[calc(100dvh-7rem)] overflow-y-auto p-1"
        />
      </div>
      <div className="flex min-w-0 flex-col gap-2">
        {!isIndex && (
          <Link href="/settings" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg lg:hidden pointer-coarse:min-h-tap">
            <ChevronLeft className="size-4" aria-hidden />
            Settings
          </Link>
        )}
        {children}
      </div>
    </div>
  );
}
