"use client";

import { useAdminLive, useMe } from "@scaffoldapp/shared";
import { Button, EmptyState, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { Lock } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { ScrollTabs, SideTabs } from "@/components/shell/SectionNav";
import { ADMIN_LINKS } from "./links";

/**
 * /admin: staff only (`me.admin_role` support or admin; the API enforces it too). Side
 * tabs at ≥ 1024 px, a tab row below. Compact density for pointer screens (tokens turn it
 * off on touch). Subscribes to the `admin:live` feed features publish to.
 */
export function AdminLayout({ children }: { children: ReactNode }) {
  const me = useMe();
  const pathname = usePathname() ?? "/admin";
  const staff = !!me.data?.admin_role;
  useAdminLive(staff);

  if (!me.data) {
    return (
      <SkeletonGroup className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-40" />
      </SkeletonGroup>
    );
  }
  if (!staff) {
    return (
      <div className="mx-auto w-full max-w-content px-gutter py-section">
        <EmptyState
          icon={<Lock />}
          title="You don't have access to this"
          action={
            <Button asChild variant="secondary" size="auto">
              <Link href="/">Go home</Link>
            </Button>
          }
        />
      </div>
    );
  }
  return (
    <div data-density="compact" className="mx-auto w-full max-w-content px-gutter py-section lg:grid lg:grid-cols-[14rem_minmax(0,1fr)] lg:gap-12">
      <div className="hidden lg:block">
        <SideTabs links={ADMIN_LINKS} pathname={pathname} label="Admin" className="sticky top-24" />
      </div>
      <div className="flex min-w-0 flex-col gap-6">
        <ScrollTabs links={ADMIN_LINKS} pathname={pathname} label="Admin" className="lg:hidden" />
        {children}
      </div>
    </div>
  );
}
