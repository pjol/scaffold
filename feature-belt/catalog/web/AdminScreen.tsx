"use client";

import {
  errorMessage,
  useCatalogAdminItems,
  useCatalogAdminStatus,
  useCatalogSyncRuns,
  useMe,
  useNow,
  useRestoreCatalogItem,
  useRetireCatalogItem,
  useRunCatalogSync,
  type CatalogAdminItemStatus,
  type CatalogItem,
  type CatalogSyncRun,
  type CatalogSyncStatus,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  InfoTip,
  SegmentedControl,
  Skeleton,
  Spinner,
  Tab,
  TabPanel,
  Tabs,
  TabsList,
  toast,
  type BadgeTone,
} from "@scaffoldapp/ui-web";
import { History, RefreshCw, SearchX } from "lucide-react";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen, Section } from "@/components/shell/PageHeader";
import { formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { CatalogRetiredBadge } from "./components";
import { catalogPaths } from "./nav";

type TabId = "sync" | "items";

const STATUS_LABEL: Record<CatalogSyncStatus, string> = { running: "Running", ok: "Done", failed: "Failed", refused: "Refused" };
const STATUS_TONE: Record<CatalogSyncStatus, BadgeTone> = { running: "info", ok: "success", failed: "danger", refused: "warning" };
const TRIGGER_LABEL: Record<CatalogSyncRun["trigger"], string> = { schedule: "Nightly", staff: "Staff", app: "App", seed: "Seed" };

const n = (v: number) => v.toLocaleString("en-US");

function took(run: CatalogSyncRun): string {
  if (!run.finished_at) return "—";
  const ms = Math.max(Date.parse(run.finished_at) - Date.parse(run.started_at), 0);
  return ms < 1000 ? `${ms} ms` : ms < 60_000 ? `${(ms / 1000).toFixed(1)} s` : `${Math.round(ms / 60_000)} min`;
}

function RunStatus({ run }: { run: CatalogSyncRun }) {
  const detail = [run.error, ...run.notes].filter(Boolean).join(" · ");
  return (
    <span className="inline-flex items-center gap-1">
      <Badge tone={STATUS_TONE[run.status]} size="sm" dot>
        {STATUS_LABEL[run.status]}
      </Badge>
      {run.forced ? <Badge size="sm">Forced</Badge> : null}
      {detail ? <InfoTip content={detail} label="Details" /> : null}
    </span>
  );
}

function Fact({ label, info, children }: { label: string; info?: string | undefined; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="flex items-center gap-1 type-label text-fg-muted">
        {label}
        {info ? <InfoTip content={info} /> : null}
      </dt>
      <dd className="type-body text-fg tabular-nums">{children}</dd>
    </div>
  );
}

function SyncTab() {
  const me = useMe();
  const now = useNow(15_000);
  const status = useCatalogAdminStatus();
  const runs = useCatalogSyncRuns();
  const sync = useRunCatalogSync();
  const [confirm, setConfirm] = useState<"sync" | "force" | null>(null);
  const isAdmin = me.data?.admin_role === "admin";

  if (status.isPending) return <Skeleton className="h-48" />;
  if (status.isError) {
    return <ErrorState title="Couldn't load the sync status" info={status.error.message} onRetry={() => void status.refetch()} retrying={status.isFetching} />;
  }
  const s = status.data;
  const refused = s.last?.status === "refused" && !s.running ? s.last : null;
  const rows = runs.data?.items ?? [];

  return (
    <div className="flex flex-col gap-section">
      {s.running ? (
        <Banner tone="info" icon={<Spinner />}>
          Syncing from {s.running.source}, started {formatRelative(s.running.started_at, now)}.
        </Banner>
      ) : refused ? (
        <Banner
          tone="warning"
          action={
            isAdmin ? (
              <Button size="sm" variant="secondary" onClick={() => setConfirm("force")}>
                Sync anyway
              </Button>
            ) : undefined
          }
        >
          {refused.error}
        </Banner>
      ) : null}

      <Section
        title="Status"
        action={
          isAdmin && s.source ? (
            <Button size="sm" leftIcon={<RefreshCw />} disabled={!!s.running} onClick={() => setConfirm("sync")}>
              Sync now
            </Button>
          ) : undefined
        }
      >
        <dl className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
          <Fact label="Source" info={s.source ? undefined : "No source is set: set CATALOG_SOURCE_URL, or register a source and set CATALOG_SOURCE."}>
            {s.source ? `${s.source.name}${s.source.detail ? ` · ${s.source.detail}` : ""}` : "None"}
          </Fact>
          <Fact label="Nightly" info="When the scheduled sync starts, in UTC (CATALOG_SYNC_HOUR).">
            {s.nightly_hour === null ? "Off" : `${String(s.nightly_hour).padStart(2, "0")}:00 UTC`}
          </Fact>
          <Fact label="Next sync">
            {s.next_sync_at ? (
              <time dateTime={s.next_sync_at} title={formatDateTime(s.next_sync_at)}>
                {Date.parse(s.next_sync_at) <= now ? "Within minutes" : formatRelative(s.next_sync_at, now)}
              </time>
            ) : (
              "—"
            )}
          </Fact>
          <Fact label="Active">{n(s.loaded.active)}</Fact>
          <Fact label="Retired" info={`${n(s.loaded.retired_by_staff)} of them by staff.`}>
            {n(s.loaded.retired)}
          </Fact>
          <Fact label="Safety valve" info="A sync that would retire more than this share of the active items is refused and changes nothing (CATALOG_MAX_RETIRE_PERCENT).">
            {s.max_retire_percent}%
          </Fact>
        </dl>
        {s.loaded.version !== s.version ? <p className="type-label text-fg-muted">This server is still loading version {s.version}.</p> : null}
      </Section>

      <Section title="Sync log">
        <Card padding="none">
          {runs.isPending ? (
            <TableSkeleton rows={5} cols={4} />
          ) : runs.isError ? (
            <ErrorState compact title="Couldn't load the log" info={runs.error.message} onRetry={() => void runs.refetch()} retrying={runs.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={<History />} title="No syncs yet" />
          ) : (
            <>
              <Table label="Sync log">
                <THead>
                  <TH>Started</TH>
                  <TH>Status</TH>
                  <TH className="hidden sm:table-cell">Trigger</TH>
                  <TH align="right" info="Items the source sent.">
                    Fetched
                  </TH>
                  <TH className="hidden md:table-cell" align="right">
                    New
                  </TH>
                  <TH className="hidden md:table-cell" align="right">
                    Updated
                  </TH>
                  <TH className="hidden md:table-cell" align="right" info="No longer listed by the source; restored when it lists them again. A refused run shows what it would have done.">
                    Retired
                  </TH>
                  <TH className="hidden lg:table-cell" align="right">
                    Restored
                  </TH>
                  <TH className="hidden lg:table-cell" align="right" info="Invalid items and repeated keys; the first few reasons are in the status ⓘ.">
                    Skipped
                  </TH>
                  <TH className="hidden lg:table-cell" align="right">
                    Took
                  </TH>
                </THead>
                <tbody>
                  {rows.map((r) => (
                    <TR key={r.id}>
                      <TD className="whitespace-nowrap">
                        <time dateTime={r.started_at} title={formatDateTime(r.started_at)}>
                          {formatRelative(r.started_at, now)}
                        </time>
                      </TD>
                      <TD className="whitespace-nowrap">
                        <RunStatus run={r} />
                      </TD>
                      <TD className="hidden whitespace-nowrap text-fg-muted sm:table-cell">
                        {TRIGGER_LABEL[r.trigger]} · {r.source}
                      </TD>
                      <TD align="right" className="tabular-nums">
                        {n(r.fetched)}
                      </TD>
                      <TD align="right" className="hidden tabular-nums md:table-cell">
                        {n(r.inserted)}
                      </TD>
                      <TD align="right" className="hidden tabular-nums md:table-cell">
                        {n(r.updated)}
                      </TD>
                      <TD align="right" className="hidden tabular-nums md:table-cell">
                        {n(r.retired)}
                      </TD>
                      <TD align="right" className="hidden tabular-nums lg:table-cell">
                        {n(r.restored)}
                      </TD>
                      <TD align="right" className="hidden tabular-nums lg:table-cell">
                        {n(r.skipped)}
                      </TD>
                      <TD align="right" className="hidden whitespace-nowrap text-fg-muted lg:table-cell">
                        {took(r)}
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={runs.hasNextPage} isFetchingNextPage={runs.isFetchingNextPage} fetchNextPage={runs.fetchNextPage} />
            </>
          )}
        </Card>
      </Section>

      {isAdmin ? (
        <ConfirmDialog
          open={confirm !== null}
          onOpenChange={(open) => !open && setConfirm(null)}
          title={confirm === "force" ? "Sync anyway?" : "Sync the catalog now?"}
          confirmLabel={confirm === "force" ? "Sync anyway" : "Sync now"}
          tone={confirm === "force" ? "danger" : "primary"}
          onConfirm={() =>
            sync.mutateAsync({ force: confirm === "force" }).then(
              () => toast.success("Sync started"),
              (e: unknown) => {
                throw new Error(errorMessage(e, "Couldn't start the sync."));
              },
            )
          }
        >
          {confirm === "force" ? (
            <p className="type-body text-fg">
              The safety valve is skipped once, so every item the source no longer lists is retired (about {n(refused?.retired ?? 0)}). Retired items stay stored and come back when the
              source lists them again.
            </p>
          ) : (
            <p className="type-body text-fg">
              Every item is read from {s.source?.name ?? "the source"}: new ones are added, changed ones updated, and ones it no longer lists retired. Nothing changes if it would retire
              more than {s.max_retire_percent}% of them.
            </p>
          )}
        </ConfirmDialog>
      ) : null}
    </div>
  );
}

const ITEM_STATUS: { value: CatalogAdminItemStatus; label: string }[] = [
  { value: "all", label: "All" },
  { value: "active", label: "Active" },
  { value: "retired", label: "Retired" },
];

function ItemsTab() {
  const me = useMe();
  const now = useNow(60_000);
  const [q, setQ] = useUrlState("q", "");
  const [status, setStatus] = useUrlState<CatalogAdminItemStatus>("status", "all");
  const items = useCatalogAdminItems({ q: q || undefined, status, limit: 50 });
  const retire = useRetireCatalogItem();
  const restore = useRestoreCatalogItem();
  const [target, setTarget] = useState<CatalogItem | null>(null);
  const isAdmin = me.data?.admin_role === "admin";
  const rows = items.data?.items ?? [];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <SearchInput value={q} onSearch={(v) => setQ(v || null)} placeholder="Name, key or other name" label="Look up items" className="sm:max-w-sm" />
        <SegmentedControl aria-label="Status" options={ITEM_STATUS} value={status} onValueChange={(v) => setStatus(v)} fullWidth={false} />
      </div>
      <Card padding="none">
        {items.isPending ? (
          <TableSkeleton rows={6} cols={3} />
        ) : items.isError ? (
          <ErrorState compact title="Couldn't load items" info={items.error.message} onRetry={() => void items.refetch()} retrying={items.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<SearchX />} title={q || status !== "all" ? "No matching items" : "No items yet"} />
        ) : (
          <>
            <Table label="Items">
              <THead>
                <TH>Item</TH>
                <TH className="hidden sm:table-cell">Key</TH>
                <TH className="hidden md:table-cell">Category</TH>
                <TH>Status</TH>
                <TH className="hidden lg:table-cell" align="right">
                  Updated
                </TH>
                {isAdmin ? <TH align="right">{null}</TH> : null}
              </THead>
              <tbody>
                {rows.map((it) => (
                  <TR key={it.key}>
                    <TD className="w-full max-w-0">
                      <Link href={catalogPaths.item(it.key)} className="block truncate font-semibold text-fg underline-offset-2 hover:underline">
                        {it.name}
                      </Link>
                    </TD>
                    <TD className="hidden whitespace-nowrap font-mono text-fg-muted sm:table-cell">{it.key}</TD>
                    <TD className="hidden whitespace-nowrap text-fg-muted md:table-cell">{it.category || "—"}</TD>
                    <TD className="whitespace-nowrap">{it.retired ? <CatalogRetiredBadge item={it} /> : <Badge size="sm">Active</Badge>}</TD>
                    <TD className="hidden whitespace-nowrap text-fg-muted lg:table-cell" align="right">
                      <time dateTime={it.updated_at} title={formatDateTime(it.updated_at)}>
                        {formatRelative(it.updated_at, now)}
                      </time>
                    </TD>
                    {isAdmin ? (
                      <TD align="right">
                        <Button size="sm" variant={it.retired ? "secondary" : "danger-ghost"} onClick={() => setTarget(it)}>
                          {it.retired ? "Restore" : "Retire"}
                        </Button>
                      </TD>
                    ) : null}
                  </TR>
                ))}
              </tbody>
            </Table>
            <LoadMore hasNextPage={items.hasNextPage} isFetchingNextPage={items.isFetchingNextPage} fetchNextPage={items.fetchNextPage} />
          </>
        )}
      </Card>
      {isAdmin && target ? (
        <ConfirmDialog
          open
          onOpenChange={(open) => !open && setTarget(null)}
          title={target.retired ? `Restore ${target.name}?` : `Retire ${target.name}?`}
          confirmLabel={target.retired ? "Restore" : "Retire"}
          tone={target.retired ? "primary" : "danger"}
          onConfirm={() =>
            (target.retired ? restore : retire).mutateAsync(target.key).then(
              () => toast.success(target.retired ? "Item restored" : "Item retired"),
              (e: unknown) => {
                throw new Error(errorMessage(e, "Couldn't change the item."));
              },
            )
          }
        >
          {target.retired ? (
            <p className="type-body text-fg">
              {target.retired_by === "source" ? "The source no longer lists it, so the next sync retires it again." : "It comes back to search and lists."}
            </p>
          ) : (
            <p className="type-body text-fg">It leaves search and lists, and stays retired through syncs until it is restored. Names still resolve to it.</p>
          )}
        </ConfirmDialog>
      ) : null}
    </div>
  );
}

/** Admin → Catalog: sync status, log and Sync now; items to look up, retire and restore. */
export function CatalogAdminScreen() {
  const [tab, setTab] = useUrlState<TabId>("tab", "sync");
  return (
    <Screen title="Catalog" info="Synced from its source every night; items are retired, never deleted. Changes here go to the audit log.">
      <Tabs value={tab} onValueChange={(v) => setTab(v as TabId)}>
        <TabsList aria-label="Catalog admin">
          <Tab value="sync">Sync</Tab>
          <Tab value="items">Items</Tab>
        </TabsList>
        <TabPanel value="sync" className="pt-6">
          <SyncTab />
        </TabPanel>
        <TabPanel value="items" className="pt-6">
          <ItemsTab />
        </TabPanel>
      </Tabs>
    </Screen>
  );
}
