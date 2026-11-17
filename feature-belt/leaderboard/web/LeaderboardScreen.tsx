"use client";

import {
  formatLeaderboardScore,
  useLeaderboardBoards,
  useLeaderboardEntries,
  useLeaderboardStanding,
  type LeaderboardBoard,
  type LeaderboardEntry,
} from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, Skeleton, SkeletonGroup, Tab, Tabs, TabsList, cn } from "@scaffoldapp/ui-web";
import { Trophy, Users } from "lucide-react";
import Link from "next/link";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";

/** Money scores: grouped, two decimals at least, with the currency's symbol where it has a common one. */
function money(amount: string, currency: string): string {
  const neg = amount.startsWith("-");
  const [int = "0", frac = ""] = (neg ? amount.slice(1) : amount).split(".");
  const grouped = `${int.replace(/\B(?=(\d{3})+(?!\d))/g, ",")}.${frac.padEnd(2, "0")}`;
  const sym: Record<string, string> = { USD: "$", EUR: "€", GBP: "£", JPY: "¥" };
  const s = sym[currency];
  return `${neg ? "-" : ""}${s ? `${s}${grouped}` : `${grouped} ${currency}`}`;
}

export function scoreText(board: Pick<LeaderboardBoard, "format" | "currency">, score: string): string {
  return formatLeaderboardScore(board, score, money);
}

function EntryRow({ e, board }: { e: LeaderboardEntry; board: LeaderboardBoard }) {
  return (
    <li className={cn("flex items-center gap-3 border-b border-border py-3", e.is_me && "font-semibold")} aria-current={e.is_me ? "true" : undefined}>
      <span className={cn("w-10 shrink-0 text-right type-label tabular-nums", e.rank <= 3 ? "text-primary-strong" : "text-fg-muted")}>{e.rank}</span>
      <AppAvatar name={e.name} src={e.avatar_url} size="sm" />
      <span className="min-w-0 flex-1 truncate type-body text-fg">
        {e.name}
        {e.is_me && <span className="type-caption text-fg-muted"> · you</span>}
      </span>
      <span className="type-label tabular-nums text-fg">{scoreText(board, e.score)}</span>
    </li>
  );
}

function Board({ board }: { board: LeaderboardBoard }) {
  const entries = useLeaderboardEntries(board.id);
  const me = useLeaderboardStanding(board.id);
  return (
    <div className="flex flex-col gap-section">
      <Card padding="compact" className="flex flex-wrap items-center gap-x-6 gap-y-2">
        <span className="type-label text-fg-muted">Your rank</span>
        {me.isPending ? (
          <Skeleton shape="text" className="h-5 w-24" />
        ) : me.isError ? (
          <span className="type-label text-fg-muted">Unavailable</span>
        ) : me.data.visibility === "hidden" ? (
          <span className="type-label text-fg">Hidden</span>
        ) : me.data.rank ? (
          <span className="type-title-sm tabular-nums text-fg">
            #{me.data.rank} <span className="type-label text-fg-muted">of {me.data.ranked}</span>
          </span>
        ) : (
          <span className="type-label text-fg">Not ranked yet</span>
        )}
        {me.data?.score && <span className="type-label tabular-nums text-fg">{scoreText(board, me.data.score)}</span>}
        <Button asChild variant="ghost" size="sm" className="ml-auto">
          <Link href="/settings/leaderboard">How you appear</Link>
        </Button>
      </Card>
      <Section title={board.title}>
        {entries.isPending ? (
          <SkeletonGroup label="Loading the leaderboard" className="flex flex-col gap-3">
            {[0, 1, 2, 3, 4].map((i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </SkeletonGroup>
        ) : entries.isError ? (
          <ErrorState compact title="Couldn't load the leaderboard" info={entries.error.message} onRetry={() => void entries.refetch()} />
        ) : entries.data.items.length === 0 ? (
          <EmptyState compact icon={<Users />} title="Nobody is ranked yet" />
        ) : (
          <>
            <ol aria-label={board.title}>
              {entries.data.items.map((e) => (
                <EntryRow key={`${e.rank}-${e.name}`} e={e} board={board} />
              ))}
            </ol>
            <LoadMore hasNextPage={entries.hasNextPage} isFetchingNextPage={entries.isFetchingNextPage} fetchNextPage={entries.fetchNextPage} />
          </>
        )}
      </Section>
    </div>
  );
}

/** Leaderboards: one tab per board (in the URL), the caller's rank, and the ranked list. */
export function LeaderboardScreen() {
  const boards = useLeaderboardBoards();
  const list = boards.data?.items ?? [];
  const [id, setId] = useUrlState("board", "");
  const board = list.find((b) => b.id === id) ?? list[0];

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <PageHeader title="Leaderboard" info="Scores refresh every minute. You appear under an alias unless you choose to show your name." />
      {boards.isPending ? (
        <SkeletonGroup label="Loading leaderboards" className="flex flex-col gap-3">
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-64 w-full" />
        </SkeletonGroup>
      ) : boards.isError ? (
        <ErrorState title="Couldn't load leaderboards" info={boards.error.message} onRetry={() => void boards.refetch()} retrying={boards.isFetching} />
      ) : !board ? (
        <EmptyState icon={<Trophy />} title="No leaderboards yet" />
      ) : (
        <>
          {list.length > 1 && (
            <Tabs value={board.id} onValueChange={setId}>
              <TabsList aria-label="Leaderboards">
                {list.map((b) => (
                  <Tab key={b.id} value={b.id}>
                    {b.title}
                  </Tab>
                ))}
              </TabsList>
            </Tabs>
          )}
          <Board key={board.id} board={board} />
        </>
      )}
    </div>
  );
}
