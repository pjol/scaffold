"use client";

import { orgsNounA, orgsNounTitle, useActiveOrg } from "@scaffoldapp/shared";
import { Button, EmptyState, Skeleton, cn } from "@scaffoldapp/ui-web";
import { Building2 } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { Sidebar } from "./Sidebar";

/** Fills the viewport under the top bar (and above the phone tab bar) so each pane scrolls on its own. */
const PANE_HEIGHT =
  "h-[calc(100dvh_-_var(--app-size-topbar)_-_var(--spacing)*16_-_env(safe-area-inset-bottom))] lg:h-[calc(100dvh_-_var(--app-size-topbar))]";

/**
 * /docs: the active org's workspace. Page tree beside the page at ≥ 1024 px; below, /docs
 * is the tree and each page stacks with a back button.
 */
export function DocsLayout({ children }: { children: ReactNode }) {
  const pathname = usePathname() ?? "/docs";
  const { org, isPending } = useActiveOrg();
  const atHome = pathname === "/docs";
  if (isPending) {
    return (
      <div className={cn("flex w-full", PANE_HEIGHT)} aria-busy aria-label="Loading pages">
        <div className="hidden w-72 flex-col gap-3 border-r border-border p-4 lg:flex">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-6 w-3/4" />
          ))}
        </div>
        <div className="flex-1" />
      </div>
    );
  }
  if (!org) {
    return (
      <div className="flex flex-1 items-center justify-center px-gutter py-section">
        <EmptyState
          icon={<Building2 />}
          title={`Join ${orgsNounA()} to use docs`}
          action={
            <Button asChild variant="secondary" size="auto">
              <Link href="/settings/orgs">{orgsNounTitle("many")}</Link>
            </Button>
          }
        />
      </div>
    );
  }
  return (
    <div className={cn("flex w-full", PANE_HEIGHT)}>
      <aside aria-label="Pages" className={cn("min-h-0 w-full flex-col bg-surface lg:flex lg:w-72 lg:shrink-0 lg:border-r lg:border-border", atHome ? "flex" : "hidden")}>
        <Sidebar orgId={org.id} orgName={org.name} />
      </aside>
      <section className={cn("min-h-0 min-w-0 flex-1 flex-col bg-canvas", atHome ? "hidden lg:flex" : "flex")}>{children}</section>
    </div>
  );
}
