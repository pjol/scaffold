"use client";

import {
  FILES_SECTIONS,
  connectionFixPath,
  filesConnectedPath,
  filesFormatSize,
  orgsNounA,
  orgsNounTitle,
  useActiveOrg,
  useFilesConnectedDrives,
  useFilesDrive,
  useFilesSources,
  type FilesSection,
} from "@scaffoldapp/shared";
import { Button, Card, ConnectPrompt, ConnectedSection, EmptyState, Menu, MenuItem, MenuSeparator, ProviderIcon, Skeleton, cn, connectedSectionItems } from "@scaffoldapp/ui-web";
import { Building2, ChevronDown, Clock, HardDrive, Star, Trash, UserPlus, Users, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { SideTabs, type SectionLink } from "@/components/shell/SectionNav";
import { FilesActionsProvider } from "./actions";
import { connectedDriveIcon } from "./ConnectedScreens";
import { FilesConnectedProvider } from "./connected";
import { FilesConnectedDetailsSheet, FilesDetailsSheet } from "./DetailsPanel";
import { FilesUploadsProvider } from "./uploads";

/** App paths of the drive's sections. */
export const FILES_SECTION_PATH: Record<FilesSection, string> = {
  my: "/files",
  shared: "/files/shared",
  shared_with_me: "/files/shared-with-me",
  recent: "/files/recent",
  starred: "/files/starred",
  trash: "/files/trash",
};

const SECTION_ICON: Record<FilesSection, LucideIcon> = { my: HardDrive, shared: Users, shared_with_me: UserPlus, recent: Clock, starred: Star, trash: Trash };

const LINKS: SectionLink[] = FILES_SECTIONS.map((s) => ({
  href: FILES_SECTION_PATH[s.id],
  label: s.label,
  icon: SECTION_ICON[s.id],
  ...(s.id === "my" ? { active: (p: string) => p === "/files" } : {}),
}));

/**
 * The drive's frame (web/app/(app)/files/layout.tsx): the upload queue and the dialogs
 * outlive navigation between folders; side sections at ≥ 1024 px with the storage meter.
 */
export function FilesLayout({ children }: { children: ReactNode }) {
  const { org, isPending } = useActiveOrg();
  const pathname = usePathname() ?? "/files";
  if (!isPending && !org) {
    return (
      <div className="mx-auto w-full max-w-content px-gutter py-section">
        <Card padding="compact">
          <EmptyState
            compact
            icon={<Building2 />}
            title={`Join or create ${orgsNounA()} first`}
            action={
              <Button asChild size="auto">
                <Link href="/settings/orgs">{orgsNounTitle("many")}</Link>
              </Button>
            }
          />
        </Card>
      </div>
    );
  }
  return (
    <FilesUploadsProvider>
      <FilesActionsProvider orgId={org?.id ?? null}>
        <FilesConnectedProvider orgId={org?.id ?? null}>
          <div className="mx-auto w-full max-w-content px-gutter py-section lg:grid lg:grid-cols-[13rem_minmax(0,1fr)] lg:gap-10">
            <aside className="hidden lg:block">
              <div className="sticky top-24 flex flex-col gap-6">
                <SideTabs links={LINKS} pathname={pathname} label="Drive" />
                <StorageMeter orgId={org?.id ?? null} />
                <ConnectedNav orgId={org?.id ?? null} pathname={pathname} />
              </div>
            </aside>
            <div className="flex min-w-0 flex-col">{children}</div>
          </div>
          <FilesConnectedDetailsSheet />
        </FilesConnectedProvider>
        <FilesDetailsSheet />
      </FilesActionsProvider>
    </FilesUploadsProvider>
  );
}

/** The connection a path opens (/files/c/<id>/…), if any. */
export function connectedIdFrom(pathname: string): string | null {
  const m = /^\/files\/c\/([^/]+)/.exec(pathname);
  return m?.[1] ? decodeURIComponent(m[1]) : null;
}

const DISMISS_KEY = "scaffoldapp.files.connect-prompt";

/**
 * The sidebar's "Connected" section: each connected account (with Reconnect when it needs it),
 * and the open account's drives under it. Nothing connected: a quiet prompt, dismissible.
 */
function ConnectedNav({ orgId, pathname }: { orgId: string | null; pathname: string }) {
  const sources = useFilesSources(orgId);
  const active = connectedIdFrom(pathname);
  const [dismissed, setDismissed] = useState(true);
  useEffect(() => {
    try {
      setDismissed(window.localStorage.getItem(DISMISS_KEY) === "1");
    } catch {
      setDismissed(false);
    }
  }, []);
  const items = connectedSectionItems(sources.data?.sources, (id) => filesConnectedPath(id));
  const offers = sources.data?.offers ?? [];
  if (!sources.isPending && items.length === 0) {
    if (dismissed || offers.length === 0) return null;
    return (
      <ConnectPrompt
        offers={offers}
        returnTo={pathname}
        linkAs={Link}
        title="Connect your drives"
        info="Browse Google Drive, OneDrive and SharePoint here with your own account."
        onDismiss={() => {
          setDismissed(true);
          try {
            window.localStorage.setItem(DISMISS_KEY, "1");
          } catch {
            /* storage unavailable */
          }
        }}
      />
    );
  }
  return (
    <div className="flex flex-col gap-1">
      <ConnectedSection items={items} activeId={active} linkAs={Link} loading={sources.isPending} {...(offers[0] ? { addHref: connectionFixPath(offers[0].capability, pathname) } : {})} />
      {active && items.some((i) => i.id === active && i.status !== "blocked") ? <ConnectedDrivesNav connId={active} orgId={orgId} pathname={pathname} /> : null}
    </div>
  );
}

/** The open account's drives, indented under its row. */
function ConnectedDrivesNav({ connId, orgId, pathname }: { connId: string; orgId: string | null; pathname: string }) {
  const drives = useFilesConnectedDrives(connId, orgId);
  if (drives.isPending) {
    return (
      <div className="flex flex-col gap-1 pl-7" aria-busy>
        <Skeleton className="h-6" />
      </div>
    );
  }
  if (drives.isError) return null;
  return (
    <ul className="flex flex-col pl-5" aria-label="Drives">
      {(drives.data?.items ?? []).map((d) => {
        const Icon = connectedDriveIcon(d.kind);
        const current = pathname === d.link || decodeURIComponent(pathname) === decodeURIComponent(d.link);
        return (
          <li key={d.ref}>
            <Link
              href={d.link}
              aria-current={current ? "page" : undefined}
              title={d.site ? `${d.site} · ${d.name}` : d.name}
              className={cn(
                "flex min-h-8 items-center gap-2 rounded-sm px-2 py-1 type-label focus-visible:outline-2 focus-visible:outline-ring",
                current ? "bg-sunken font-semibold text-fg" : "text-fg-muted hover:bg-sunken hover:text-fg",
              )}
            >
              <Icon className="size-4 shrink-0" aria-hidden />
              <span className="min-w-0 truncate">{d.site ? `${d.site} · ${d.name}` : d.name}</span>
            </Link>
          </li>
        );
      })}
    </ul>
  );
}

/** Storage used against the org's quota. */
export function StorageMeter({ orgId, className }: { orgId: string | null; className?: string }) {
  const drive = useFilesDrive(orgId);
  const d = drive.data;
  if (!d) return null;
  const pct = d.quota_bytes > 0 ? Math.min(100, (d.used_bytes / d.quota_bytes) * 100) : 0;
  return (
    <div className={cn("flex flex-col gap-2 px-3", className)}>
      {d.quota_bytes > 0 ? (
        <div className="h-1.5 overflow-hidden rounded-pill bg-sunken" role="meter" aria-label="Storage used" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(pct)}>
          <div className={cn("h-full rounded-pill", pct > 90 ? "bg-danger" : pct > 75 ? "bg-warning" : "bg-primary")} style={{ width: `${Math.max(pct, 1)}%` }} />
        </div>
      ) : null}
      <p className="type-caption text-fg-muted">
        {d.quota_bytes > 0 ? `${filesFormatSize(d.used_bytes)} of ${filesFormatSize(d.quota_bytes)} used` : `${filesFormatSize(d.used_bytes)} used`}
      </p>
    </div>
  );
}

/** The section's title; below 1024 px it opens the section switcher (the side tabs show from 1024 px). */
export function SectionTitle({ current }: { current: FilesSection }) {
  const label = FILES_SECTIONS.find((s) => s.id === current)?.label ?? "Drive";
  const { org } = useActiveOrg();
  const sources = useFilesSources(org?.id);
  const connected = (sources.data?.sources ?? []).filter((s) => s.type === "connected" && s.status !== "blocked");
  return (
    <>
      <span className="hidden truncate lg:inline">{label}</span>
      <span className="lg:hidden">
        <Menu
          align="start"
          trigger={
            <button type="button" className="-ml-1 flex min-h-control items-center gap-1 rounded-md px-1 hover:bg-sunken" aria-label={`${label}, change section`}>
              {label}
              <ChevronDown className="size-5 text-fg-muted" aria-hidden />
            </button>
          }
        >
          {FILES_SECTIONS.map((s) => {
            const Icon = SECTION_ICON[s.id];
            return (
              <MenuItem key={s.id} asChild icon={<Icon />}>
                <Link href={FILES_SECTION_PATH[s.id]} aria-current={s.id === current ? "page" : undefined}>
                  {s.label}
                </Link>
              </MenuItem>
            );
          })}
          {connected.length > 0 ? <MenuSeparator /> : null}
          {connected.map((s) => (
            <MenuItem key={s.id} asChild icon={s.provider ? <ProviderIcon provider={s.provider} product={s.product} size={16} /> : <HardDrive />}>
              <Link href={filesConnectedPath(s.id)}>{s.label}</Link>
            </MenuItem>
          ))}
        </Menu>
      </span>
    </>
  );
}
