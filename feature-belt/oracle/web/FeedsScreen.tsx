"use client";

import { ORACLE_STATUS_LABEL, formatOraclePrice, oracleStatusTone, useMe, useNow, useOracleFeeds, type OracleFeedKind } from "@scaffoldapp/shared";
import { Badge, Button, EmptyState, ErrorState, SegmentedControl, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { LineChart, SearchX, Settings2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { oraclePaths } from "./nav";

type KindFilter = "all" | OracleFeedKind;

const KIND_OPTIONS: { value: KindFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "single", label: "Singles" },
  { value: "basket", label: "Baskets" },
];

/** /oracle: every listed feed with its published price, live over realtime. */
export function OracleFeedsScreen() {
  const router = useRouter();
  const now = useNow(30_000);
  const me = useMe();
  const [q, setQ] = useUrlState("q", "");
  const [kind, setKind] = useUrlState<KindFilter>("kind", "all");
  const feeds = useOracleFeeds({ q: q || undefined, kind: kind === "all" ? undefined : kind, limit: 50 });
  const rows = feeds.data?.items ?? [];
  const filtered = !!q || kind !== "all";
  const staff = !!me.data?.admin_role;

  return (
    <Screen
      title="Prices"
      info="Each price comes from several venues. Every print is screened for manipulation before it counts, and the published price moves toward its target at a capped rate."
      action={
        staff ? (
          <Button asChild variant="secondary" size="sm" leftIcon={<Settings2 />}>
            <Link href={oraclePaths.admin}>Manage</Link>
          </Button>
        ) : undefined
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <SearchInput value={q} onSearch={setQ} placeholder="Key or name" label="Search prices" className="sm:max-w-sm" />
          <SegmentedControl aria-label="Kind" options={KIND_OPTIONS} value={kind} onValueChange={(v) => setKind(v)} fullWidth={false} />
        </div>
        {feeds.isPending ? (
          <SkeletonGroup label="Loading prices" className="flex flex-col gap-3">
            {Array.from({ length: 6 }, (_, i) => (
              <Skeleton key={i} className="h-12" />
            ))}
          </SkeletonGroup>
        ) : feeds.isError ? (
          <ErrorState title="Couldn't load prices" info={feeds.error.message} onRetry={() => void feeds.refetch()} retrying={feeds.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState icon={filtered ? <SearchX /> : <LineChart />} title={filtered ? "No matching prices" : "No prices yet"} />
        ) : (
          <>
            <Table label="Prices">
              <THead>
                <TH>Feed</TH>
                <TH className="hidden sm:table-cell">Key</TH>
                <TH align="right">Price</TH>
                <TH className="hidden sm:table-cell">Status</TH>
                <TH className="hidden lg:table-cell">Venues</TH>
                <TH className="hidden md:table-cell" align="right">
                  Updated
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
                    <TD align="right" className="whitespace-nowrap tabular-nums">
                      <span className="text-fg">{formatOraclePrice(f.price)}</span> <span className="text-fg-muted">{f.quote}</span>
                    </TD>
                    <TD className="hidden whitespace-nowrap sm:table-cell">
                      <Badge tone={oracleStatusTone(f.status)} size="sm" dot>
                        {ORACLE_STATUS_LABEL[f.status]}
                      </Badge>
                    </TD>
                    <TD className="hidden max-w-48 truncate text-fg-muted lg:table-cell">
                      {f.kind === "basket" ? `${f.part_count} parts` : f.venues.join(", ") || "—"}
                    </TD>
                    <TD className="hidden whitespace-nowrap text-fg-muted md:table-cell" align="right">
                      {f.updated_at ? (
                        <time dateTime={f.updated_at} title={formatDateTime(f.updated_at)}>
                          {formatRelative(f.updated_at, now)}
                        </time>
                      ) : (
                        "—"
                      )}
                    </TD>
                  </TR>
                ))}
              </tbody>
            </Table>
            <LoadMore hasNextPage={feeds.hasNextPage} isFetchingNextPage={feeds.isFetchingNextPage} fetchNextPage={feeds.fetchNextPage} />
          </>
        )}
      </div>
    </Screen>
  );
}
