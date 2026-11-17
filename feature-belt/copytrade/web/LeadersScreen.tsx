"use client";

import { useCopytradeConfig, useCopytradeLeader, useCopytradeLeaders, type CopytradeLeaderCard, type CopytradeLeaderSort } from "@scaffoldapp/shared";
import { Badge, Banner, Button, EmptyState, ErrorState, ListGroup, ListRow, SegmentedControl, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { Users } from "lucide-react";
import Link from "next/link";
import { PageHeader } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { PctText, PersonLine, useExchangeMoney } from "./parts";
import { StartCopySheet } from "./StartCopySheet";

const SORTS: { value: CopytradeLeaderSort; label: string }[] = [
  { value: "return_90d", label: "90 days" },
  { value: "return_30d", label: "30 days" },
  { value: "copiers", label: "Copiers" },
  { value: "aum", label: "Assets" },
];

/** The directory of traders who take copiers; `?leader=<user id>` opens one's start sheet. */
export function LeadersScreen() {
  const [sort, setSort] = useUrlState<CopytradeLeaderSort>("sort", "return_90d");
  const [leaderId, setLeaderId] = useUrlState<string>("leader", "");
  const leaders = useCopytradeLeaders(sort);
  const config = useCopytradeConfig();
  const selected = useCopytradeLeader(leaderId || null);
  const { money } = useExchangeMoney();
  const card: CopytradeLeaderCard | null = leaders.data?.items.find((l) => l.user.id === leaderId) ?? selected.data ?? null;

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <PageHeader
        title="Find traders"
        info="Returns are time-weighted from daily account values, so deposits and withdrawals don't count. Past results don't predict future ones."
        action={
          <Button asChild variant="secondary" size="auto">
            <Link href="/copy">My copies</Link>
          </Button>
        }
      />
      {config.data?.paused && <Banner tone="warning">Copy trading is paused right now: no new copies start and copied orders wait.</Banner>}
      <SegmentedControl options={SORTS} value={sort} onValueChange={(v) => setSort(v)} className="self-start" />
      {leaders.isPending ? (
        <SkeletonGroup label="Loading traders" className="flex flex-col gap-2">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-16 w-full" />
          ))}
        </SkeletonGroup>
      ) : leaders.isError ? (
        <ErrorState title="Couldn't load traders" info={leaders.error.message} onRetry={() => void leaders.refetch()} retrying={leaders.isFetching} />
      ) : leaders.data.items.length === 0 ? (
        <EmptyState
          icon={<Users />}
          title="No traders take copiers yet"
          info="Traders offer copy trading in Settings → Copy trading."
          action={
            <Button asChild variant="secondary">
              <Link href="/settings/copy-trading">Offer your trades</Link>
            </Button>
          }
        />
      ) : (
        <ListGroup>
          {leaders.data.items.map((l) => (
            <ListRow
              key={l.user.id}
              interactive
              onClick={() => setLeaderId(l.user.id)}
              title={
                <PersonLine person={l.user}>
                  <span className="flex flex-wrap gap-x-3 type-caption text-fg-muted">
                    <span>
                      {sort === "return_30d" ? "30 days" : "90 days"} <PctText pct={sort === "return_30d" ? l.return_30d_pct : l.return_90d_pct} />
                    </span>
                    <span>
                      Drawdown <PctText pct={l.max_drawdown_90d_pct} signed={false} />
                    </span>
                    <span>{sort === "aum" ? `${money(l.aum)} copying` : `${l.copiers} ${l.copiers === 1 ? "copier" : "copiers"}`}</span>
                    <span>{l.profit_share_pct}% share</span>
                  </span>
                </PersonLine>
              }
              trailing={
                l.is_me ? (
                  <Badge size="sm">You</Badge>
                ) : l.copying ? (
                  <Badge tone="success" size="sm">
                    Copying
                  </Badge>
                ) : !l.accepting ? (
                  <Badge size="sm">Full</Badge>
                ) : undefined
              }
            />
          ))}
        </ListGroup>
      )}
      <StartCopySheet leader={card} open={!!leaderId && !!card} onOpenChange={(open) => !open && setLeaderId(null)} />
    </div>
  );
}
