"use client";

import {
  CRM_SOURCE_ALL,
  CRM_SOURCE_APP,
  connectionFixPath,
  isConnectionErrorCode,
  type ApiError,
  crmSourceIdOf,
  isConnectedSource,
  type CrmFeatureSource,
  type CrmSkippedSource,
  type CrmSources,
  type SourceRef,
} from "@scaffoldapp/shared";
import { Banner, Button, Card, ConnectPrompt, ConnectedSection, EmptyState, ErrorState, SourceFilter, connectedSectionItems, sourceFilterOptions, sourcePlatformName, withReturnTo } from "@scaffoldapp/ui-web";
import { PlugZap } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useMemo } from "react";
import { APP_NAME } from "@/lib/app";
import { useUrlState } from "@/lib/url-state";
import { useCrm } from "./CrmFrame";

/**
 * Where a CRM screen reads from: the app's own records, a connected HubSpot or Salesforce
 * account (a connection id), or `all` (lists only). Kept in `?source=` so links and reloads
 * keep it. Boards, dashboards and tasks show one source at a time.
 */
export type CrmSourceScope = "list" | "board" | "dashboard" | "tasks";

export interface CrmSourceState {
  /** "app", "all" or a connection id. */
  value: string;
  setValue: (v: string) => void;
  /** The connection when value is one. */
  connection: CrmFeatureSource | null;
  /** True when the screen shows something from a connected account. */
  connected: boolean;
  sources: CrmSources | undefined;
  /** Where a new record from this screen goes ("app" or a writable connection id), or null when nowhere. */
  createIn: string | null;
}

function connectedSources(s: CrmSources | undefined): CrmFeatureSource[] {
  return (s?.sources ?? []).filter((x) => x.type === "connected");
}

/** The screen's source, defaulting to what the workspace's mode shows. */
export function useCrmSource(scope: CrmSourceScope): CrmSourceState {
  const { sources } = useCrm();
  const mode = sources?.mode ?? "app";
  const conns = connectedSources(sources);
  const fallback =
    mode === "app" || conns.length === 0
      ? CRM_SOURCE_APP
      : scope === "list"
        ? mode === "both" || conns.length > 1
          ? CRM_SOURCE_ALL
          : (conns[0]?.id ?? CRM_SOURCE_APP)
        : mode === "connected"
          ? (conns[0]?.id ?? CRM_SOURCE_APP)
          : CRM_SOURCE_APP;
  const [raw, setRaw] = useUrlState("source", "");
  let value = raw || fallback;
  // A source the workspace or person no longer has falls back quietly.
  if (value !== CRM_SOURCE_APP && value !== CRM_SOURCE_ALL && !conns.some((c) => c.id === value)) value = fallback;
  if (value === CRM_SOURCE_ALL && scope !== "list") value = fallback;
  if (value === CRM_SOURCE_APP && mode === "connected") value = fallback;
  const connection = conns.find((c) => c.id === value) ?? null;
  const writable = conns.filter((c) => c.writable && (c.status ?? "active") === "active");
  const createIn = connection
    ? connection.writable
      ? connection.id
      : null
    : mode !== "connected"
      ? CRM_SOURCE_APP
      : (writable[0]?.id ?? null);
  return {
    value,
    setValue: (v) => setRaw(v === fallback ? "" : v),
    connection,
    connected: value !== CRM_SOURCE_APP && (connection !== null || value === CRM_SOURCE_ALL),
    sources,
    createIn,
  };
}

/** Source chips (All · app · each connected account); hidden while only the app's own records exist. */
export function CrmSourceBar({ state, scope, className }: { state: CrmSourceState; scope: CrmSourceScope; className?: string }) {
  const options = useMemo(() => {
    const s = state.sources;
    if (!s || s.mode === "app") return [];
    let opts = sourceFilterOptions({ appLabel: APP_NAME, sources: s.sources });
    if (scope !== "list") opts = opts.filter((o) => o.id !== CRM_SOURCE_ALL);
    if (s.mode === "connected") opts = opts.filter((o) => o.id !== CRM_SOURCE_APP);
    if (s.mode === "connected" && scope === "list" && connectedSources(s).length < 2) opts = opts.filter((o) => o.id !== CRM_SOURCE_ALL);
    return opts;
  }, [state.sources, scope]);
  if (options.length < 2) return null;
  return <SourceFilter options={options} value={state.value} onChange={state.setValue} label="Records from" {...(className ? { className } : {})} />;
}

/** Reconnect notice for the chosen connection, and notes about sources a merged list left out. */
export function CrmSourceNotices({ state, skipped }: { state: CrmSourceState; skipped?: CrmSkippedSource[] | undefined }) {
  const pathname = usePathname() ?? "/crm";
  const c = state.connection;
  return (
    <>
      {c && c.status === "needs_reauth" ? (
        <Banner
          tone="warning"
          action={
            <Button asChild size="sm" variant="secondary">
              <Link href={c.fix_url ?? connectionFixPath(c.capability, pathname)}>Reconnect</Link>
            </Button>
          }
        >
          {sourcePlatformName(c.provider ?? "")} needs to be reconnected before its records show here.
        </Banner>
      ) : null}
      {skipped && skipped.length > 0 ? (
        <Banner tone="info">
          {skipped.map((s) => s.message).join(" ")}
        </Banner>
      ) : null}
    </>
  );
}

/**
 * A screen's load error: for a connected account's failure (expired, scopes missing, rate
 * limited) the platform's message in plain sight and the fix; otherwise the usual error state.
 */
export function CrmLoadError({
  error,
  title,
  onRetry,
  retrying,
  state,
  inCard,
}: {
  error: ApiError;
  title: string;
  onRetry: () => void;
  retrying?: boolean;
  state?: CrmSourceState;
  /** Already inside a Card (a list): no card of its own. */
  inCard?: boolean;
}) {
  const pathname = usePathname() ?? "/crm";
  if (!isConnectionErrorCode(error.code)) return <ErrorState title={title} info={error.message} onRetry={onRetry} {...(retrying !== undefined ? { retrying } : {})} />;
  const fixable = error.code === "connection_needs_auth" || error.code === "connection_scope_missing";
  const body = (
      <EmptyState
        icon={<PlugZap />}
        title={error.message}
        action={
          fixable ? (
            <Button asChild size="auto">
              <Link href={connectionFixPath(state?.connection?.capability, pathname)}>{error.code === "connection_needs_auth" ? "Reconnect" : "Turn it on"}</Link>
            </Button>
          ) : (
            <Button variant="secondary" size="auto" loading={!!retrying} onClick={onRetry}>
              Retry
            </Button>
          )
        }
      />
  );
  return inCard ? body : <Card padding="compact">{body}</Card>;
}

/** Dashboard aside: your connected CRM accounts (with status), or a quiet offer to connect one. */
export function CrmConnectedCard({ onSelect, activeId }: { onSelect?: (id: string) => void; activeId?: string }) {
  const { sources } = useCrm();
  const pathname = usePathname() ?? "/crm";
  if (!sources || sources.mode === "app") return null;
  const items = connectedSectionItems(sources.sources).map((i) => {
    const s = sources.sources.find((x) => x.id === i.id);
    return { ...i, ...(s?.status === "needs_reauth" ? { fixUrl: s.fix_url ?? connectionFixPath(s.capability, pathname) } : {}) };
  });
  return (
    <div className="flex flex-col gap-3">
      {items.length > 0 ? (
        <ConnectedSection
          items={items}
          title="Connected CRMs"
          linkAs={Link}
          {...(activeId ? { activeId } : {})}
          {...(onSelect ? { onSelect } : {})}
          addHref={sources.offers.length > 0 ? withReturnTo(sources.offers[0]!.connect_url, pathname) : "/settings/connections"}
        />
      ) : null}
      <ConnectPrompt
        offers={sources.offers}
        returnTo={pathname}
        linkAs={Link}
        title={items.length > 0 ? "Connect another CRM" : "Work with HubSpot or Salesforce here"}
        info="Records stay on the platform and are read with your own account, so you see exactly what it can see there."
      />
    </div>
  );
}

/** "Open in HubSpot" for a connected row, or null. */
export function openInLabel(source: SourceRef | null | undefined): string | null {
  if (!source || source.type !== "connected" || !source.url) return null;
  return `Open in ${sourcePlatformName(source.provider)}`;
}

/** The connection a record or row belongs to ("app" for the app's own). */
export function sourceIdOf(row: { id: string; source?: SourceRef | null }): string {
  return crmSourceIdOf(row);
}

export { isConnectedSource };
