"use client";

import { orgsNounA, orgsNounTitle, useActiveOrg, useChannelsWorkspace } from "@scaffoldapp/shared";
import { Button, EmptyState, Skeleton, cn } from "@scaffoldapp/ui-web";
import { Building2, Hash } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";
import { Sidebar } from "./Sidebar";

/** Fills the viewport under the top bar (and above the phone tab bar) so each pane scrolls on its own. */
const PANE_HEIGHT =
  "h-[calc(100dvh_-_var(--app-size-topbar)_-_var(--spacing)*16_-_env(safe-area-inset-bottom))] lg:h-[calc(100dvh_-_var(--app-size-topbar))]";

const LAST_KEY = "scaffoldapp.channels.last";

function remember(orgId: string, channelId: string) {
  try {
    window.localStorage.setItem(`${LAST_KEY}.${orgId}`, channelId);
  } catch {
    // Storage blocked: the workspace opens on the first channel instead.
  }
}

function recall(orgId: string): string | null {
  try {
    return window.localStorage.getItem(`${LAST_KEY}.${orgId}`);
  } catch {
    return null;
  }
}

/**
 * /channels: the workspace of the active org. Sidebar beside the content at ≥ 1024 px;
 * below, /channels is the sidebar and every other page stacks with a back button.
 */
export function WorkspaceLayout({ children }: { children: ReactNode }) {
  const pathname = usePathname() ?? "/channels";
  const { org, isPending } = useActiveOrg();
  const atHome = pathname === "/channels";
  const channelId = /^\/channels\/([0-9a-f-]{36})$/.exec(pathname)?.[1];
  useEffect(() => {
    if (org && channelId) remember(org.id, channelId);
  }, [org, channelId]);

  if (isPending) {
    return (
      <div className={cn("flex w-full", PANE_HEIGHT)} aria-busy aria-label="Loading workspace">
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
          title={`Join ${orgsNounA()} to use channels`}
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
      <aside aria-label="Workspace" className={cn("min-h-0 w-full flex-col bg-surface lg:flex lg:w-72 lg:shrink-0 lg:border-r lg:border-border", atHome ? "flex" : "hidden")}>
        <Sidebar orgId={org.id} orgName={org.name} />
      </aside>
      <section className={cn("min-h-0 min-w-0 flex-1 flex-col bg-canvas", atHome ? "hidden lg:flex" : "flex")}>{children}</section>
    </div>
  );
}

/** /channels beside the sidebar at ≥ 1024 px: opens the last channel you read, else the default one. */
export function WorkspaceHome() {
  const router = useRouter();
  const { org } = useActiveOrg();
  const ws = useChannelsWorkspace(org?.id);
  useEffect(() => {
    if (!org || !ws.data || typeof window.matchMedia !== "function" || !window.matchMedia("(min-width: 1024px)").matches) return;
    const last = recall(org.id);
    const target = ws.data.channels.find((c) => c.id === last) ?? ws.data.channels.find((c) => c.is_default) ?? ws.data.channels[0];
    if (target) router.replace(`/channels/${target.id}`);
  }, [org, ws.data, router]);
  return (
    <div className="flex flex-1 items-center justify-center px-gutter py-section">
      <EmptyState icon={<Hash />} title="Choose a channel" />
    </div>
  );
}
