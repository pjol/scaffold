import { errorMessage, useInbox, useMarkNotificationsRead, useNow, useUnreadNotificationCount, type NotificationItem } from "@scaffoldapp/shared";
import { Button, Caption, Divider, EmptyState, ErrorState, IconButton, Screen, SegmentedControl, SkeletonRow, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, type Href } from "expo-router";
import { Bell, BellOff, CheckCheck } from "lucide-react-native";
import { useState } from "react";
import { Pressable, RefreshControl, View } from "react-native";
import { PushBanner } from "./PushBanner";

type Filter = "all" | "unread";

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

/** "5 min ago", "yesterday", then the date after 30 days. */
function ago(iso: string, now: number): string {
  const t = new Date(iso).getTime();
  const s = Math.round((t - now) / 1000);
  const abs = Math.abs(s);
  if (Number.isNaN(t)) return "";
  if (abs < 45) return "just now";
  if (abs < 3600) return relative.format(Math.round(s / 60), "minute");
  if (abs < 86_400) return relative.format(Math.round(s / 3600), "hour");
  if (abs < 30 * 86_400) return relative.format(Math.round(s / 86_400), "day");
  return new Date(iso).toLocaleDateString();
}

function Row({ item, now, onPress }: { item: NotificationItem; now: number; onPress: () => void }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const unread = !item.read_at;
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`${unread ? "Unread. " : ""}${item.title}`}
      style={({ pressed }) => ({
        flexDirection: "row",
        gap: s[3],
        paddingHorizontal: s[4],
        paddingVertical: s[3],
        minHeight: theme.tap.base,
        backgroundColor: pressed ? c.sunken : "transparent",
      })}
    >
      <View style={{ width: s[2], height: s[2], borderRadius: theme.radius.pill, marginTop: s[2], backgroundColor: unread ? c.primary : "transparent" }} />
      <View style={{ flex: 1, gap: s[1] }}>
        <View style={{ flexDirection: "row", gap: s[3], alignItems: "baseline" }}>
          <Text weight={unread ? 600 : 400} style={{ flex: 1 }}>
            {item.title}
          </Text>
          <Caption tone="muted">{ago(item.created_at, now)}</Caption>
        </View>
        {item.body ? (
          <Text role="label" tone="muted" numberOfLines={3}>
            {item.body}
          </Text>
        ) : null}
      </View>
    </Pressable>
  );
}

/** Inbox: newest first, All or Unread; tapping marks read and opens the link. Offers push on this phone. */
export function InboxScreen() {
  const theme = useTheme();
  const s = theme.space;
  const [filter, setFilter] = useState<Filter>("all");
  const inbox = useInbox(filter === "unread");
  const unread = useUnreadNotificationCount() ?? 0;
  const markRead = useMarkNotificationsRead();
  const now = useNow(60_000);
  const rows = inbox.data?.items ?? [];

  const open = (item: NotificationItem) => {
    if (!item.read_at) markRead.mutate({ ids: [item.id] });
    if (item.link.startsWith("/") && !item.link.startsWith("//")) router.push(item.link as Href);
  };

  return (
    <Screen edges={["bottom"]} gap={s[4]} refreshControl={<RefreshControl refreshing={inbox.isRefetching && !inbox.isFetchingNextPage} onRefresh={() => void inbox.refetch()} />}>
      <Stack.Screen
        options={{
          title: "Inbox",
          headerRight: () => (
            <IconButton
              label="Mark all read"
              icon={CheckCheck}
              variant="ghost"
              disabled={unread === 0}
              onPress={() => markRead.mutate({ all: true }, { onError: (e) => toast.error(errorMessage(e, "Couldn't mark them read. Try again.")) })}
            />
          ),
        }}
      />
      <PushBanner />
      <SegmentedControl<Filter>
        accessibilityLabel="Show"
        value={filter}
        onValueChange={setFilter}
        options={[
          { value: "all", label: "All" },
          { value: "unread", label: unread > 0 ? `Unread (${unread})` : "Unread" },
        ]}
      />
      {inbox.isPending ? (
        <View>
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </View>
      ) : inbox.isError ? (
        <ErrorState title="Couldn't load notifications" info={errorMessage(inbox.error)} onRetry={() => void inbox.refetch()} retrying={inbox.isRefetching} />
      ) : rows.length === 0 ? (
        <EmptyState
          icon={filter === "unread" ? BellOff : Bell}
          title={filter === "unread" ? "Nothing unread" : "No notifications yet"}
          action={
            <Button variant="secondary" onPress={() => router.push("/settings/notifications")}>
              Notification settings
            </Button>
          }
        />
      ) : (
        <View style={{ backgroundColor: theme.colors.surface, borderRadius: theme.radius.lg, borderWidth: 1, borderColor: theme.colors.border, overflow: "hidden" }}>
          {rows.map((item, i) => (
            <View key={item.id}>
              {i > 0 ? <Divider inset={s[4]} /> : null}
              <Row item={item} now={now} onPress={() => open(item)} />
            </View>
          ))}
        </View>
      )}
      {inbox.hasNextPage ? (
        <Button variant="ghost" loading={inbox.isFetchingNextPage} onPress={() => void inbox.fetchNextPage()}>
          Load more
        </Button>
      ) : null}
    </Screen>
  );
}
