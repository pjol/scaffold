import {
  errorMessage,
  MODERATION_CATEGORY_LABELS,
  REPORT_REASONS,
  useBlockedUsers,
  useModerationContent,
  useModerationStanding,
  useMyReports,
  useUnblockUser,
  type ModerationContentItem,
  type MyReport,
} from "@scaffoldapp/shared";
import { Avatar, Badge, Banner, Body, Button, Caption, EmptyState, ErrorState, Label, ListGroup, ListRow, Screen, SkeletonRow, Spinner, toast, useTheme, type BadgeTone } from "@scaffoldapp/ui-native";
import { router, Stack, type Href } from "expo-router";
import { Flag, ShieldCheck, Sparkles } from "lucide-react-native";
import { Pressable, RefreshControl, View } from "react-native";

const KIND_LABEL: Record<string, string> = {
  user: "Person",
  "chat.message": "Message",
  "feed.post": "Post",
  "feed.comment": "Comment",
  "forum.post": "Forum post",
  "forum.comment": "Forum comment",
  "assistant.message": "AI reply",
};
const CONTENT_STATE: Record<ModerationContentItem["state"], { label: string; tone: BadgeTone }> = {
  in_review: { label: "In review", tone: "warning" },
  removed: { label: "Removed", tone: "danger" },
  approved: { label: "Approved", tone: "success" },
};

function dateLabel(iso: string, withTime = false): string {
  const d = new Date(iso);
  return withTime ? d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }) : d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}
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
  const standing = useModerationStanding();
  const content = useModerationContent();
  const unblock = useUnblockUser();
  const blocked = blocks.data?.items ?? [];
  const sent = reports.data?.items ?? [];
  const held = content.data?.items ?? [];
  const st = standing.data;
  const refreshing = (blocks.isRefetching || reports.isRefetching || content.isRefetching) && !blocks.isFetchingNextPage && !reports.isFetchingNextPage && !content.isFetchingNextPage;

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
            void standing.refetch();
            void content.refetch();
          }}
        />
      }
    >
      <Stack.Screen options={{ title: "Safety" }} />
      <ListGroup title="Strikes" info={`A removal or ban by a moderator adds a strike. Strikes count for ${st?.window_days ?? 90} days; enough of them stop you posting for a while.`}>
        {standing.isPending ? (
          <Loading />
        ) : standing.isError ? (
          <ErrorState compact title="Couldn't load your strikes" info={errorMessage(standing.error)} onRetry={() => void standing.refetch()} retrying={standing.isRefetching} />
        ) : (
          <>
            {st?.restricted_until ? <Banner tone="warning" title={`You can post again on ${dateLabel(st.restricted_until, true)}.`} /> : null}
            {st && st.strikes.length > 0 ? (
              st.strikes.map((k) => (
                <ListRow
                  key={k.id}
                  title={k.action === "ban" ? "Account banned" : k.category ? `Removed: ${MODERATION_CATEGORY_LABELS[k.category] ?? k.category}` : "Content removed"}
                  trailing={<Caption tone="muted">{`Until ${dateLabel(k.expires_at)}`}</Caption>}
                />
              ))
            ) : (
              <EmptyState compact icon={ShieldCheck} title="No strikes" />
            )}
          </>
        )}
      </ListGroup>

      <ListGroup title="Held and removed" info="What you posted that screening held for review or a moderator removed. Held items are visible only to you until a moderator approves them.">
        {content.isPending ? (
          <Loading />
        ) : content.isError ? (
          <ErrorState compact title="Couldn't load your content" info={errorMessage(content.error)} onRetry={() => void content.refetch()} retrying={content.isRefetching} />
        ) : held.length === 0 ? (
          <EmptyState compact icon={Sparkles} title="Nothing held or removed" />
        ) : (
          held.map((c) => {
            const open = c.link && c.state !== "removed" ? c.link : null;
            const row = (
              <View style={{ gap: s[1], paddingVertical: s[3] }}>
                <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], flexWrap: "wrap" }}>
                  <Label weight={600}>{KIND_LABEL[c.target_kind] ?? c.target_kind}</Label>
                  <Badge tone={CONTENT_STATE[c.state].tone}>{CONTENT_STATE[c.state].label}</Badge>
                  <Caption tone="muted" style={{ marginLeft: "auto" }}>
                    {dateLabel(c.created_at)}
                  </Caption>
                </View>
                {c.text ? <Body numberOfLines={3}>{c.text}</Body> : null}
                {c.reasons.length > 0 ? <Label tone="muted">{c.reasons.join(". ")}</Label> : null}
              </View>
            );
            return open ? (
              <Pressable key={c.case_id} accessibilityRole="link" accessibilityLabel={`Open ${KIND_LABEL[c.target_kind] ?? "item"}`} onPress={() => router.push(open as Href)}>
                {row}
              </Pressable>
            ) : (
              <View key={c.case_id}>{row}</View>
            );
          })
        )}
        {content.hasNextPage ? (
          content.isFetchingNextPage ? (
            <Spinner />
          ) : (
            <Button variant="ghost" onPress={() => void content.fetchNextPage()}>
              Load more
            </Button>
          )
        ) : null}
      </ListGroup>

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
