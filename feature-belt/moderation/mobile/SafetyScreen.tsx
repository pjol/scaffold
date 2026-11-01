import { errorMessage, REPORT_REASONS, useBlockedUsers, useMyReports, useUnblockUser, type MyReport } from "@scaffoldapp/shared";
import { Avatar, Badge, Button, EmptyState, ErrorState, ListGroup, ListRow, Screen, SkeletonRow, Spinner, toast, useTheme, type BadgeTone } from "@scaffoldapp/ui-native";
import { Stack } from "expo-router";
import { Flag, ShieldCheck } from "lucide-react-native";
import { RefreshControl, View } from "react-native";

const KIND_LABEL: Record<string, string> = { user: "Person", "chat.message": "Message", "feed.post": "Post", "feed.comment": "Comment", "assistant.message": "AI reply" };
const OUTCOME: Record<MyReport["outcome"], { label: string; tone: BadgeTone }> = {
  open: { label: "In review", tone: "info" },
  actioned: { label: "Action taken", tone: "success" },
  closed: { label: "Closed", tone: "neutral" },
};

function kindLabel(kind: string): string {
  return KIND_LABEL[kind] ?? (kind.split(".").pop() ?? kind).replace(/_/g, " ");
}

function Loading() {
  return (
    <View accessibilityLabel="Loading" accessibilityRole="progressbar">
      <SkeletonRow />
      <SkeletonRow />
    </View>
  );
}

/** Settings → Safety: people you blocked (unblock here) and the reports you sent. */
export function SafetyScreen() {
  const theme = useTheme();
  const s = theme.space;
  const blocks = useBlockedUsers();
  const reports = useMyReports();
  const unblock = useUnblockUser();
  const blocked = blocks.data?.items ?? [];
  const sent = reports.data?.items ?? [];
  const refreshing = (blocks.isRefetching || reports.isRefetching) && !blocks.isFetchingNextPage && !reports.isFetchingNextPage;

  return (
    <Screen
      edges={["bottom"]}
      gap={s[8]}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={() => {
            void blocks.refetch();
            void reports.refetch();
          }}
        />
      }
    >
      <Stack.Screen options={{ title: "Safety" }} />
      <ListGroup title="Blocked people" info="Blocked people can't message you, and you don't see each other's content. They aren't told.">
        {blocks.isPending ? (
          <Loading />
        ) : blocks.isError ? (
          <ErrorState compact title="Couldn't load blocked people" info={errorMessage(blocks.error)} onRetry={() => void blocks.refetch()} retrying={blocks.isRefetching} />
        ) : blocked.length === 0 ? (
          <EmptyState compact icon={ShieldCheck} title="No one blocked" />
        ) : (
          blocked.map((b) => (
            <ListRow
              key={b.user_id}
              title={b.display_name || "Unnamed"}
              leading={<Avatar name={b.display_name || "?"} src={b.avatar_url} size="md" />}
              trailing={
                <Button
                  variant="secondary"
                  size="md"
                  loading={unblock.isPending && unblock.variables === b.user_id}
                  onPress={() =>
                    unblock.mutate(b.user_id, {
                      onSuccess: () => toast.success(`${b.display_name || "They"} unblocked`),
                      onError: (e) => toast.error(errorMessage(e, "Couldn't unblock. Try again.")),
                    })
                  }
                >
                  Unblock
                </Button>
              }
            />
          ))
        )}
        {blocks.hasNextPage ? (
          blocks.isFetchingNextPage ? (
            <Spinner />
          ) : (
            <Button variant="ghost" onPress={() => void blocks.fetchNextPage()}>
              Load more
            </Button>
          )
        ) : null}
      </ListGroup>

      <ListGroup title="Your reports" info="Moderators review every report. You hear when one is closed, but not what action was taken.">
        {reports.isPending ? (
          <Loading />
        ) : reports.isError ? (
          <ErrorState compact title="Couldn't load your reports" info={errorMessage(reports.error)} onRetry={() => void reports.refetch()} retrying={reports.isRefetching} />
        ) : sent.length === 0 ? (
          <EmptyState compact icon={Flag} title="No reports yet" />
        ) : (
          sent.map((r) => (
            <ListRow
              key={r.id}
              title={`${kindLabel(r.target_kind)} · ${REPORT_REASONS.find((x) => x.value === r.reason)?.label ?? r.reason}`}
              trailing={<Badge tone={OUTCOME[r.outcome].tone}>{OUTCOME[r.outcome].label}</Badge>}
            />
          ))
        )}
        {reports.hasNextPage ? (
          reports.isFetchingNextPage ? (
            <Spinner />
          ) : (
            <Button variant="ghost" onPress={() => void reports.fetchNextPage()}>
              Load more
            </Button>
          )
        ) : null}
      </ListGroup>
    </Screen>
  );
}
