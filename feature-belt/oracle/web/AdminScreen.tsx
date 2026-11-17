"use client";

import {
  ORACLE_STATUS_LABEL,
  ORACLE_VERDICT_LABEL,
  formatOraclePrice,
  oracleStatusTone,
  useMe,
  useNow,
  useOracleAdminFeeds,
  useOracleAdminObservations,
  useOracleSources,
  type OracleVerdict,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, InfoTip, SegmentedControl, Skeleton, Tab, TabPanel, Tabs, TabsList } from "@scaffoldapp/ui-web";
import { Activity, Plus, Radio, SearchX } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { OracleObservationList } from "./components";
import { oraclePaths } from "./nav";
import { OracleNewFeedDialog } from "./NewFeedDialog";

type TabId = "feeds" | "sources" | "observations";
type VerdictFilter = "all" | OracleVerdict;

function FeedsTab() {
  const router = useRouter();
  const me = useMe();
  const [q, setQ] = useUrlState("q", "");
  const [creating, setCreating] = useState(false);
  const feeds = useOracleAdminFeeds({ q: q || undefined, limit: 50 });
  const rows = feeds.data?.items ?? [];
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <SearchInput value={q} onSearch={setQ} placeholder="Key or name" label="Search feeds" className="sm:max-w-sm" />
        {me.data?.admin_role === "admin" && (
          <Button leftIcon={<Plus />} onClick={() => setCreating(true)} className="self-start">
            New feed
          </Button>
        )}
      </div>
      <Card padding="none">
        {feeds.isPending ? (
          <TableSkeleton rows={6} cols={5} />
        ) : feeds.isError ? (
          <ErrorState compact title="Couldn't load feeds" info={feeds.error.message} onRetry={() => void feeds.refetch()} retrying={feeds.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<SearchX />} title={q ? "No matching feeds" : "No feeds yet"} />
        ) : (
          <>
            <Table label="Feeds">
              <THead>
                <TH>Feed</TH>
                <TH className="hidden sm:table-cell">Key</TH>
                <TH className="hidden md:table-cell">Kind</TH>
                <TH>Status</TH>
                <TH align="right">Price</TH>
                <TH className="hidden lg:table-cell" info="Unlisted feeds are basket parts, not shown on their own.">
                  Listed
                </TH>
              </THead>
              <tbody>
                {rows.map((f) => (
                  <TR key={f.id} onActivate={() => router.push(oraclePaths.feed(f.key))}>
                    <TD className="w-full max-w-0">
                      <Link href={oraclePaths.feed(f.key)} className="block truncate font-semibold text-fg underline-offset-2 hover:underline">
                        {f.name}
                      </Link>
                    </TD>
                    <TD className="hidden whitespace-nowrap text-fg-muted sm:table-cell">{f.key}</TD>
                    <TD className="hidden whitespace-nowrap text-fg-muted md:table-cell">{f.kind === "basket" ? `Basket of ${f.part_count}` : `${f.venues.length} venues`}</TD>
                    <TD className="whitespace-nowrap">
                      <Badge tone={oracleStatusTone(f.status)} size="sm" dot>
                        {ORACLE_STATUS_LABEL[f.status]}
                      </Badge>
                    </TD>
                    <TD align="right" className="whitespace-nowrap tabular-nums">
                      {formatOraclePrice(f.price)} <span className="text-fg-muted">{f.quote}</span>
                    </TD>
                    <TD className="hidden text-fg-muted lg:table-cell">{f.listed ? "Yes" : "No"}</TD>
                  </TR>
                ))}
              </tbody>
            </Table>
            <LoadMore hasNextPage={feeds.hasNextPage} isFetchingNextPage={feeds.isFetchingNextPage} fetchNextPage={feeds.fetchNextPage} />
          </>
        )}
      </Card>
      <OracleNewFeedDialog open={creating} onOpenChange={setCreating} />
    </div>
  );
}

function budgetText(s: { min_gap_ms: number; cadence_s: number; quota: number; per_s: number }): string {
  const parts = [`${s.min_gap_ms >= 1000 ? `${(s.min_gap_ms / 1000).toFixed(s.min_gap_ms % 1000 ? 1 : 0)} s` : `${s.min_gap_ms} ms`} apart`, `every ${s.cadence_s} s`];
  if (s.quota > 0) parts.push(`${s.quota} per ${Math.round(s.per_s / 86_400)} d`);
  return parts.join(" · ");
}

function SourcesTab() {
  const now = useNow(15_000);
  const sources = useOracleSources();
  if (sources.isPending) return <Skeleton className="h-48" />;
  if (sources.isError) {
    return <ErrorState title="Couldn't load sources" info={sources.error.message} onRetry={() => void sources.refetch()} retrying={sources.isFetching} />;
  }
  const s = sources.data;
  return (
    <div className="flex flex-col gap-section">
      <Card padding="none">
        <Table label="Sources">
          <THead>
            <TH>Source</TH>
            <TH>Health</TH>
            <TH className="hidden sm:table-cell" align="right">
              Rows
            </TH>
            <TH className="hidden md:table-cell" info="Spacing between requests to one API host, how often each row is polled, and any quota.">
              Budget
            </TH>
            <TH className="hidden lg:table-cell" align="right">
              Last success
            </TH>
          </THead>
          <tbody>
            {s.items.map((it) => (
              <TR key={it.name}>
                <TD className="whitespace-nowrap font-semibold text-fg">
                  {it.name} <span className="font-normal text-fg-muted">{it.kind === "poll" ? "polled" : "pushed"}</span>
                </TD>
                <TD className="max-w-0">
                  <span className="flex items-center gap-2">
                    <Badge tone={!it.enabled ? "neutral" : it.ok ? "success" : "warning"} size="sm" dot>
                      {!it.enabled ? "Off" : it.ok ? "OK" : "Failing"}
                    </Badge>
                    {(it.note || it.last_error || it.lanes.length > 0) && (
                      <InfoTip
                        label="Details"
                        content={
                          <div className="flex flex-col gap-1">
                            {it.note && <span>{it.note}</span>}
                            {it.last_error && <span>Last error: {it.last_error}</span>}
                            {it.lanes.map((l) => (
                              <span key={l.lane}>
                                {l.lane || "all"}: {l.ok ? "OK" : l.note}
                              </span>
                            ))}
                          </div>
                        }
                      />
                    )}
                  </span>
                </TD>
                <TD className="hidden tabular-nums sm:table-cell" align="right">
                  {it.failing > 0 ? `${it.failing} of ${it.rows} failing` : it.rows}
                </TD>
                <TD className="hidden whitespace-nowrap text-fg-muted md:table-cell">{it.kind === "poll" ? budgetText(it) : "—"}</TD>
                <TD className="hidden whitespace-nowrap text-fg-muted lg:table-cell" align="right">
                  {it.last_ok_at ? (
                    <time dateTime={it.last_ok_at} title={formatDateTime(it.last_ok_at)}>
                      {formatRelative(it.last_ok_at, now)}
                    </time>
                  ) : (
                    "—"
                  )}
                </TD>
              </TR>
            ))}
          </tbody>
        </Table>
      </Card>
      <dl className="grid gap-4 sm:grid-cols-3">
        <div className="flex flex-col gap-1">
          <dt className="flex items-center gap-1 type-label text-fg-muted">
            Decision model
            <InfoTip content="An optional second opinion on every live print. It can only raise a print's risk, and any failure keeps the statistical verdict." />
          </dt>
          <dd className="type-body text-fg">{s.decision.enabled ? s.decision.name : (s.decision.note ?? "Off")}</dd>
        </div>
        <div className="flex flex-col gap-1">
          <dt className="type-label text-fg-muted">Model calls</dt>
          <dd className="type-body text-fg tabular-nums">
            {s.decision.calls} · {s.decision.failures} failed · {s.decision.queued} queued · ${s.decision.cost_usd.toFixed(4)}
          </dd>
        </div>
        <div className="flex flex-col gap-1">
          <dt className="flex items-center gap-1 type-label text-fg-muted">
            Pipeline
            <InfoTip content="One API instance runs the oracle at a time; the others take over within 25 seconds if it stops." />
          </dt>
          <dd className="type-body text-fg">{!s.polling ? "Polling off" : s.leader.instance ? (s.leader.this ? "Running here" : "Running on another instance") : "Not running"}</dd>
        </div>
      </dl>
    </div>
  );
}

function ObservationsTab() {
  const [verdict, setVerdict] = useUrlState<VerdictFilter>("verdict", "all");
  const obs = useOracleAdminObservations({ verdict: verdict === "all" ? undefined : verdict, limit: 50 });
  const rows = obs.data?.items ?? [];
  return (
    <div className="flex flex-col gap-4">
      <SegmentedControl
        aria-label="Verdict"
        fullWidth={false}
        className="self-start"
        options={[{ value: "all", label: "All" }, ...(["accepted", "downweighted", "rejected"] as const).map((v) => ({ value: v, label: ORACLE_VERDICT_LABEL[v] }))]}
        value={verdict}
        onValueChange={(v) => setVerdict(v as VerdictFilter)}
      />
      {obs.isPending ? (
        <TableSkeleton rows={8} cols={6} />
      ) : obs.isError ? (
        <ErrorState compact title="Couldn't load observations" info={obs.error.message} onRetry={() => void obs.refetch()} retrying={obs.isFetching} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={<Radio />} title="No observations yet" />
      ) : (
        <>
          <OracleObservationList items={rows} withFeed />
          <LoadMore hasNextPage={obs.hasNextPage} isFetchingNextPage={obs.isFetchingNextPage} fetchNextPage={obs.fetchNextPage} />
        </>
      )}
    </div>
  );
}

/** Admin → Oracle: feeds (create, open), source health and the live observation stream. */
export function OracleAdminScreen() {
  const [tab, setTab] = useUrlState<TabId>("tab", "feeds");
  return (
    <Screen title="Oracle" info="Feeds, the health of every source, and every screened print as it arrives.">
      <Tabs value={tab} onValueChange={(v) => setTab(v as TabId)}>
        <TabsList aria-label="Oracle">
          <Tab value="feeds">Feeds</Tab>
          <Tab value="sources" icon={<Activity />}>
            Sources
          </Tab>
          <Tab value="observations">Observations</Tab>
        </TabsList>
        <TabPanel value="feeds" className="pt-6">
          <FeedsTab />
        </TabPanel>
        <TabPanel value="sources" className="pt-6">
          <SourcesTab />
        </TabPanel>
        <TabPanel value="observations" className="pt-6">
          <ObservationsTab />
        </TabPanel>
      </Tabs>
    </Screen>
  );
}
