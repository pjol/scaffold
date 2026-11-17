"use client";

import {
  ORACLE_VERDICT_LABEL,
  errorMessage,
  formatOracleDuration,
  formatOraclePct,
  formatOraclePrice,
  oracleBarsSeries,
  oracleChangePct,
  useMe,
  useNow,
  useOracleFeed,
  useOracleHistory,
  useUpdateOracleFeed,
  type OracleSeriesPoint,
  type OracleVerdict,
} from "@scaffoldapp/shared";
import { ConfirmDialog, EmptyState, ErrorState, InfoTip, SegmentedControl, Select, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { ChevronLeft, CirclePause, CirclePlay, PenLine, ScanSearch } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { Section } from "@/components/shell/PageHeader";
import type { ResourceMenuAction } from "@/features/registry";
import { formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { OracleObservationList, OraclePartsGroup, OraclePriceChart, OracleSourcesGroup, OracleStatusBadge } from "./components";
import { oraclePaths } from "./nav";
import { OraclePostPriceDialog } from "./PostPriceDialog";

type Range = "1d" | "30d" | "90d" | "1y";
const RANGES: { value: Range; label: string }[] = [
  { value: "1d", label: "1D" },
  { value: "30d", label: "30D" },
  { value: "90d", label: "90D" },
  { value: "1y", label: "1Y" },
];
const RANGE_DAYS: Record<Exclude<Range, "1d">, number> = { "30d": 30, "90d": 90, "1y": 365 };

type VerdictFilter = "all" | OracleVerdict;
const VERDICTS: { value: VerdictFilter; label: string }[] = [
  { value: "all", label: "All verdicts" },
  ...(["accepted", "downweighted", "rejected"] as const).map((v) => ({ value: v, label: ORACLE_VERDICT_LABEL[v] })),
];

function Back() {
  return (
    <Link href={oraclePaths.feeds} className="inline-flex h-tap-sm items-center gap-1 self-start type-label text-fg-muted hover:text-fg">
      <ChevronLeft className="size-4" aria-hidden />
      Prices
    </Link>
  );
}

function FeedSkeleton() {
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <SkeletonGroup label="Loading the feed" className="flex flex-col gap-6">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-12 w-64" />
        <Skeleton className="h-56" />
        <Skeleton className="h-40" />
      </SkeletonGroup>
    </div>
  );
}

/** /oracle/[key]: one feed's price, chart, venues or parts, and its screened prints, live. */
export function OracleFeedScreen({ feedKey }: { feedKey: string }) {
  const now = useNow(30_000);
  const me = useMe();
  const feed = useOracleFeed(feedKey);
  const [range, setRange] = useUrlState<Range>("range", "30d");
  const [verdict, setVerdict] = useUrlState<VerdictFilter>("verdict", "all");
  const history = useOracleHistory(range === "1d" ? null : feedKey, range === "1d" ? 30 : RANGE_DAYS[range]);
  const update = useUpdateOracleFeed(feedKey);
  const [posting, setPosting] = useState(false);
  const [toggling, setToggling] = useState(false);
  const isAdmin = me.data?.admin_role === "admin";
  const d = feed.data;

  const series: OracleSeriesPoint[] = useMemo(() => {
    if (!d) return [];
    if (range === "1d") return d.recent.map((p) => ({ t: Date.parse(p.at), v: Number(p.price) }));
    return oracleBarsSeries(history.data?.items ?? []);
  }, [d, range, history.data]);

  if (feed.isPending) return <FeedSkeleton />;
  if (feed.isError || !d) {
    return (
      <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
        <Back />
        {feed.error?.status === 404 ? (
          <EmptyState icon={<ScanSearch />} title="No such price" />
        ) : (
          <ErrorState title="Couldn't load this price" info={feed.error?.message} onRetry={() => void feed.refetch()} retrying={feed.isFetching} />
        )}
      </div>
    );
  }

  const f = d.feed;
  const manualVenues = d.sources.filter((s) => s.source === "manual").map((s) => s.venue);
  const actions: ResourceMenuAction[] = [];
  if (isAdmin && manualVenues.length > 0) actions.push({ id: "post", label: "Post a price…", icon: PenLine, onSelect: () => setPosting(true) });
  if (isAdmin) actions.push({ id: "toggle", label: f.enabled ? "Turn off…" : "Turn on", icon: f.enabled ? CirclePause : CirclePlay, destructive: f.enabled, onSelect: () => setToggling(true) });
  const first = series[0]?.v;
  const change = first ? oracleChangePct(String(first), f.price) : null;
  const observations = verdict === "all" ? d.observations : d.observations.filter((o) => o.verdict === verdict);

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <div className="flex flex-col gap-6">
        <Back />
        <div className="flex items-start justify-between gap-4">
          <div className="flex min-w-0 flex-col gap-2">
            <h1 className="flex flex-wrap items-center gap-x-3 gap-y-1 type-title text-fg lg:type-display">
              <span className="truncate">{f.name}</span>
              <span className="type-label font-medium text-fg-muted">{f.key}</span>
            </h1>
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-2">
              <span className="type-headline text-fg tabular-nums">{formatOraclePrice(f.price)}</span>
              <span className="type-label text-fg-muted">{f.quote}</span>
              <OracleStatusBadge status={f.status} />
            </div>
          </div>
          <ResourceMenu feature="oracle" kind="oracle.feed" id={f.id} title={f.name} actions={actions} />
        </div>
        <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <div className="flex flex-col gap-1">
            <dt className="flex items-center gap-1 type-label text-fg-muted">
              Target
              <InfoTip content="Where the published price is heading: the recency-weighted median of screened prints." />
            </dt>
            <dd className="type-body text-fg tabular-nums">{formatOraclePrice(f.target)}</dd>
          </div>
          <div className="flex flex-col gap-1">
            <dt className="flex items-center gap-1 type-label text-fg-muted">
              Max move
              <InfoTip content="The published price moves toward its target at most this fast, so one forced print can only nudge it." />
            </dt>
            <dd className="type-body text-fg tabular-nums">{d.params.max_move_bps_per_min} bps/min</dd>
          </div>
          <div className="flex flex-col gap-1">
            <dt className="flex items-center gap-1 type-label text-fg-muted">
              Stale after
              <InfoTip content={`Half-life ${formatOracleDuration(d.params.half_life_s)}; prints older than ${formatOracleDuration(d.params.lookback_s)} don't count.`} />
            </dt>
            <dd className="type-body text-fg">{formatOracleDuration(d.params.stale_after_s)}</dd>
          </div>
          <div className="flex flex-col gap-1">
            <dt className="type-label text-fg-muted">Updated</dt>
            <dd className="type-body text-fg">
              {f.updated_at ? (
                <time dateTime={f.updated_at} title={formatDateTime(f.updated_at)}>
                  {formatRelative(f.updated_at, now)}
                </time>
              ) : (
                "—"
              )}
            </dd>
          </div>
        </dl>
      </div>

      <Section
        title="Chart"
        action={<SegmentedControl aria-label="Range" options={RANGES} value={range} onValueChange={(v) => setRange(v)} fullWidth={false} />}
      >
        {range !== "1d" && history.isPending ? (
          <Skeleton className="h-56" />
        ) : range !== "1d" && history.isError ? (
          <ErrorState compact title="Couldn't load the history" info={history.error.message} onRetry={() => void history.refetch()} retrying={history.isFetching} />
        ) : series.length < 2 ? (
          <EmptyState compact title={range === "1d" ? "No prices in the last day" : "No history yet"} />
        ) : (
          <div className="flex flex-col gap-2">
            <p className="type-label text-fg-muted tabular-nums">
              {formatOraclePct(change)} over {RANGES.find((r) => r.value === range)?.label}
            </p>
            <OraclePriceChart points={series} label={`${f.name} price, ${RANGES.find((r) => r.value === range)?.label}, ${formatOraclePct(change)}`} />
          </div>
        )}
      </Section>

      {f.kind === "basket" ? (
        d.parts.length > 0 ? <OraclePartsGroup parts={d.parts} /> : <EmptyState compact title="No parts yet" />
      ) : d.sources.length > 0 ? (
        <OracleSourcesGroup sources={d.sources} />
      ) : (
        <EmptyState compact title="No venues yet" />
      )}

      <Section
        title="Observations"
        info="Every print from every venue, scored for manipulation. Accepted prints count fully, downweighted ones partly, rejected ones not at all."
        action={<Select aria-label="Verdict" options={VERDICTS} value={verdict} onValueChange={(v) => setVerdict(v as VerdictFilter)} className="w-40" />}
      >
        {observations.length === 0 ? (
          <EmptyState compact title={verdict === "all" ? "No observations yet" : `No ${ORACLE_VERDICT_LABEL[verdict as OracleVerdict].toLowerCase()} prints`} />
        ) : (
          <OracleObservationList items={observations} quote={f.quote} withFeed={f.kind === "basket"} />
        )}
      </Section>

      {isAdmin && manualVenues.length > 0 && (
        <OraclePostPriceDialog feedKey={f.key} quote={f.quote} venues={manualVenues} open={posting} onOpenChange={setPosting} />
      )}
      {isAdmin && (
        <ConfirmDialog
          open={toggling}
          onOpenChange={setToggling}
          title={f.enabled ? `Turn off ${f.name}?` : `Turn on ${f.name}?`}
          confirmLabel={f.enabled ? "Turn off" : "Turn on"}
          tone={f.enabled ? "danger" : "primary"}
          onConfirm={() =>
            update.mutateAsync({ enabled: !f.enabled }).then(
              () => toast.success(f.enabled ? "Feed turned off" : "Feed turned on"),
              (e: unknown) => {
                throw new Error(errorMessage(e, "Couldn't change the feed."));
              },
            )
          }
        >
          {f.enabled ? (
            <p className="type-body text-fg">Polling stops, the status shows off and baskets that use it go stale. Its history and prints are kept.</p>
          ) : (
            <p className="type-body text-fg">Polling resumes and the price picks up from its last state.</p>
          )}
        </ConfirmDialog>
      )}
    </div>
  );
}
