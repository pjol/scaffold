import { formatLeaderboardScore, useLeaderboardBoards, useLeaderboardEntries, useLeaderboardStanding, type LeaderboardBoard, type LeaderboardEntry } from "@scaffoldapp/shared";
import { Avatar, Button, EmptyState, ErrorState, Screen, SegmentedControl, Skeleton, Spinner, Text, TitleSm, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { Trophy, Users } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, View } from "react-native";

function money(amount: string, currency: string): string {
  const neg = amount.startsWith("-");
  const [int = "0", frac = ""] = (neg ? amount.slice(1) : amount).split(".");
  const grouped = `${int.replace(/\B(?=(\d{3})+(?!\d))/g, ",")}.${frac.padEnd(2, "0")}`;
  const sym: Record<string, string> = { USD: "$", EUR: "€", GBP: "£", JPY: "¥" };
  return `${neg ? "-" : ""}${sym[currency] ? `${sym[currency]}${grouped}` : `${grouped} ${currency}`}`;
}

function Row({ e, board }: { e: LeaderboardEntry; board: LeaderboardBoard }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View
      accessibilityLabel={`Rank ${e.rank}, ${e.name}${e.is_me ? ", you" : ""}, ${e.score}`}
      style={{ flexDirection: "row", alignItems: "center", gap: s[3], paddingVertical: s[3], borderBottomWidth: 1, borderBottomColor: theme.colors.border }}
    >
      <Text role="label" tabular tone={e.rank <= 3 ? "primary" : "muted"} style={{ width: s[8], textAlign: "right" }}>
        {e.rank}
      </Text>
      <Avatar name={e.name} src={e.avatar_url} size="sm" />
      <Text role="body" weight={e.is_me ? 600 : 400} numberOfLines={1} style={{ flex: 1 }}>
        {e.name}
        {e.is_me ? " · you" : ""}
      </Text>
      <Text role="label" tabular>
        {formatLeaderboardScore(board, e.score, money)}
      </Text>
    </View>
  );
}

function Board({ board }: { board: LeaderboardBoard }) {
  const theme = useTheme();
  const s = theme.space;
  const entries = useLeaderboardEntries(board.id);
  const me = useLeaderboardStanding(board.id);
  return (
    <View style={{ gap: s[6] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[3] }}>
        <View style={{ flex: 1, gap: s[0.5] }}>
          <Text role="caption" tone="muted">
            Your rank
          </Text>
          <Text role="titleSm" tabular>
            {me.data?.visibility === "hidden" ? "Hidden" : me.data?.rank ? `#${me.data.rank} of ${me.data.ranked}` : me.isPending ? "…" : "Not ranked yet"}
          </Text>
        </View>
        <Button variant="ghost" onPress={() => router.push("/settings/leaderboard")}>
          How you appear
        </Button>
      </View>
      <View style={{ gap: s[3] }}>
        <TitleSm>{board.title}</TitleSm>
        {entries.isPending ? (
          <View style={{ gap: s[2] }} accessibilityLabel="Loading the leaderboard" accessibilityRole="progressbar">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} height={theme.tap.base} />
            ))}
          </View>
        ) : entries.isError ? (
          <ErrorState compact title="Couldn't load the leaderboard" info={entries.error.message} onRetry={() => void entries.refetch()} />
        ) : entries.data.items.length === 0 ? (
          <EmptyState compact icon={Users} title="Nobody is ranked yet" />
        ) : (
          <View>
            {entries.data.items.map((e) => (
              <Row key={`${e.rank}-${e.name}`} e={e} board={board} />
            ))}
            {entries.hasNextPage &&
              (entries.isFetchingNextPage ? (
                <Spinner />
              ) : (
                <Button variant="ghost" onPress={() => void entries.fetchNextPage()}>
                  Load more
                </Button>
              ))}
          </View>
        )}
      </View>
    </View>
  );
}

/** Leaderboards: a switcher when there are several, the caller's rank and the ranked list. */
export function LeaderboardScreen() {
  const theme = useTheme();
  const boards = useLeaderboardBoards();
  const list = boards.data?.items ?? [];
  const [id, setId] = useState<string | null>(null);
  const board = list.find((b) => b.id === id) ?? list[0];
  return (
    <Screen edges={["bottom"]} gap={theme.space[6]} refreshControl={<RefreshControl refreshing={boards.isRefetching} onRefresh={() => void boards.refetch()} />}>
      {boards.isPending ? (
        <Skeleton height={theme.tap.xl * 3} />
      ) : boards.isError ? (
        <ErrorState title="Couldn't load leaderboards" info={boards.error.message} onRetry={() => void boards.refetch()} retrying={boards.isFetching} />
      ) : !board ? (
        <EmptyState icon={Trophy} title="No leaderboards yet" />
      ) : (
        <>
          {list.length > 1 && (
            <SegmentedControl accessibilityLabel="Leaderboard" options={list.map((b) => ({ value: b.id, label: b.title }))} value={board.id} onValueChange={setId} />
          )}
          <Board key={board.id} board={board} />
        </>
      )}
    </Screen>
  );
}
