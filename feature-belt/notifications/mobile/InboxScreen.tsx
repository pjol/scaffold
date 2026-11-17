import {
  errorMessage,
  groupLabel,
  notificationSnoozeOptions,
  useInbox,
  useMarkNotificationsRead,
  useMarkNotificationsUnread,
  useMuteNotificationResource,
  useNotificationGroups,
  useNotificationKinds,
  useNow,
  useSnoozeNotifications,
  useUnreadNotificationCount,
  useUnsnoozeNotifications,
  type NotificationItem,
  type NotificationTargets,
} from "@scaffoldapp/shared";
import {
  Button,
  Caption,
  Chip,
  Divider,
  EmptyState,
  ErrorState,
  IconButton,
  ListGroup,
  ListRow,
  Screen,
  SegmentedControl,
  Sheet,
  SkeletonRow,
  Switch,
  Text,
  toast,
  useTheme,
} from "@scaffoldapp/ui-native";
import { router, Stack, type Href } from "expo-router";
import { AlarmClock, Bell, BellOff, CheckCheck, MoreVertical } from "lucide-react-native";
import { useState } from "react";
import { Pressable, RefreshControl, ScrollView, View } from "react-native";
import { PushBanner } from "./PushBanner";

type InboxView = "all" | "unread" | "snoozed";

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
const untilFmt = new Intl.DateTimeFormat(undefined, { weekday: "short", hour: "numeric", minute: "2-digit" });

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

/** A row on screen and what its actions apply to. */
interface Entry {
  key: string;
  item: NotificationItem;
  target: NotificationTargets;
  unread: boolean;
  count: number;
}

function Row({ e, now, onPress, onMore }: { e: Entry; now: number; onPress: () => void; onMore: () => void }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const { item, unread, count } = e;
  return (
    <View style={{ flexDirection: "row", alignItems: "flex-start" }}>
      <Pressable
        onPress={onPress}
        onLongPress={onMore}
        accessibilityRole="button"
        accessibilityLabel={`${unread ? "Unread. " : ""}${item.title}${count > 1 ? `, ${count} notifications` : ""}`}
        accessibilityHint="Long press for more actions"
        style={({ pressed }) => ({
          flex: 1,
          flexDirection: "row",
          gap: s[3],
          paddingLeft: s[4],
          paddingVertical: s[3],
          minHeight: theme.tap.base,
          backgroundColor: pressed ? c.sunken : "transparent",
        })}
      >
        <View style={{ width: s[2], height: s[2], borderRadius: theme.radius.pill, marginTop: s[2], backgroundColor: unread ? c.primary : "transparent" }} />
        <View style={{ flex: 1, gap: s[1] }}>
          <View style={{ flexDirection: "row", gap: s[2], alignItems: "baseline" }}>
            <Text weight={unread ? 600 : 400} style={{ flex: 1 }}>
              {item.title}
            </Text>
            {count > 1 ? <Caption tone="muted">{String(count)}</Caption> : null}
            <Caption tone="muted">{ago(item.sort_at, now)}</Caption>
          </View>
          {item.body ? (
            <Text role="label" tone="muted" numberOfLines={2}>
              {item.body}
            </Text>
          ) : null}
          {item.snoozed_until ? <Caption tone="muted">{`Back ${untilFmt.format(new Date(item.snoozed_until))}`}</Caption> : null}
        </View>
      </Pressable>
      <IconButton label={`Actions for ${item.title}`} icon={MoreVertical} variant="ghost" onPress={onMore} />
    </View>
  );
}

/** Actions for one row or group: read/unread, snooze or bring back, mute. */
function ActionsSheet({ entry, onClose }: { entry: Entry | null; onClose: () => void }) {
  const markRead = useMarkNotificationsRead();
  const markUnread = useMarkNotificationsUnread();
  const snooze = useSnoozeNotifications();
  const unsnooze = useUnsnoozeNotifications();
  const mute = useMuteNotificationResource();
  const [snoozing, setSnoozing] = useState(false);
  const fail = (err: unknown) => toast.error(errorMessage(err, "Couldn't change it. Try again."));
  const close = () => {
    setSnoozing(false);
    onClose();
  };
  const e = entry;
  return (
    <Sheet open={!!e} onClose={close} title={e ? e.item.title : ""} snapPoints={["50%"]}>
      {e && snoozing ? (
        <ListGroup title="Snooze until">
          {notificationSnoozeOptions().map((o) => (
            <ListRow
              key={o.label}
              title={o.label}
              chevron={false}
              trailing={<Caption tone="muted">{untilFmt.format(new Date(o.until))}</Caption>}
              onPress={() => {
                snooze.mutate({ ...e.target, until: o.until }, { onSuccess: () => toast.success(`Snoozed until ${untilFmt.format(new Date(o.until))}.`), onError: fail });
                close();
              }}
            />
          ))}
        </ListGroup>
      ) : e ? (
        <ListGroup>
          <ListRow
            title={e.unread ? "Mark read" : "Mark unread"}
            chevron={false}
            onPress={() => {
              (e.unread ? markRead : markUnread).mutate(e.target, { onError: fail });
              close();
            }}
          />
          {e.item.snoozed_until ? (
            <ListRow
              title="Unsnooze"
              chevron={false}
              onPress={() => {
                unsnooze.mutate(e.target, { onSuccess: () => toast.success("Back in your inbox."), onError: fail });
                close();
              }}
            />
          ) : (
            <ListRow title="Snooze" onPress={() => setSnoozing(true)} />
          )}
          {e.item.mutable && e.item.resource ? (
            <ListRow
              title={e.item.resource.includes("/thread:") ? "Mute this thread" : "Mute notifications about this"}
              chevron={false}
              onPress={() => {
                mute.mutate({ resource: e.item.resource, label: e.item.title }, { onSuccess: () => toast.success("Muted. Undo it in notification settings."), onError: fail });
                close();
              }}
            />
          ) : null}
        </ListGroup>
      ) : null}
    </Sheet>
  );
}

/** Inbox: All / Unread / Snoozed, a feature filter, grouped by thread on request; tap opens, long press or ⋮ for actions. */
export function InboxScreen() {
  const theme = useTheme();
  const s = theme.space;
  const [view, setView] = useState<InboxView>("all");
  const [kind, setKind] = useState("");
  const [grouped, setGrouped] = useState(false);
  const [acting, setActing] = useState<Entry | null>(null);
  const filter = { unread: view === "unread", snoozed: view === "snoozed", kind };
  const inbox = useInbox(filter, { enabled: !grouped });
  const groups = useNotificationGroups(filter, { enabled: grouped });
  const kinds = useNotificationKinds(view === "snoozed");
  const unread = useUnreadNotificationCount() ?? 0;
  const markRead = useMarkNotificationsRead();
  const now = useNow(60_000);
  const list = grouped ? groups : inbox;
  const entries: Entry[] = grouped
    ? (groups.data?.items ?? []).map((g) => ({ key: g.key, item: g.latest, target: { resources: [g.key] }, unread: g.unread > 0, count: g.count }))
    : (inbox.data?.items ?? []).map((i) => ({ key: i.id, item: i, target: { ids: [i.id] }, unread: !i.read_at, count: 1 }));
  const chips = kinds.data?.items ?? [];

  const open = (e: Entry) => {
    if (e.unread) markRead.mutate(e.target);
    if (e.item.link.startsWith("/") && !e.item.link.startsWith("//")) router.push(e.item.link as Href);
  };

  return (
    <Screen edges={["bottom"]} gap={s[4]} refreshControl={<RefreshControl refreshing={list.isRefetching && !list.isFetchingNextPage} onRefresh={() => void list.refetch()} />}>
      <Stack.Screen
        options={{
          title: "Inbox",
          headerRight: () => (
            <IconButton
              label="Mark all read"
              icon={CheckCheck}
              variant="ghost"
              disabled={unread === 0 || view === "snoozed"}
              onPress={() => markRead.mutate({ all: true }, { onError: (err) => toast.error(errorMessage(err, "Couldn't mark them read. Try again.")) })}
            />
          ),
        }}
      />
      <PushBanner />
      <SegmentedControl<InboxView>
        accessibilityLabel="Show"
        value={view}
        onValueChange={setView}
        options={[
          { value: "all", label: "All" },
          { value: "unread", label: unread > 0 ? `Unread (${unread})` : "Unread" },
          { value: "snoozed", label: kinds.data?.snoozed ? `Snoozed (${kinds.data.snoozed})` : "Snoozed" },
        ]}
      />
      {chips.length > 1 || kind ? (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2] }} accessibilityLabel="Filter by feature">
          <Chip size="sm" selected={!kind} onPress={() => setKind("")}>
            Everything
          </Chip>
          {chips.map((c) => (
            <Chip key={c.group} size="sm" selected={kind === c.group} count={view === "unread" ? c.unread : c.count} onPress={() => setKind(kind === c.group ? "" : c.group)}>
              {groupLabel(c.group)}
            </Chip>
          ))}
        </ScrollView>
      ) : null}
      <Switch label="Group by thread" value={grouped} onValueChange={setGrouped} />
      {list.isPending ? (
        <View>
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </View>
      ) : list.isError ? (
        <ErrorState title="Couldn't load notifications" info={errorMessage(list.error)} onRetry={() => void list.refetch()} retrying={list.isRefetching} />
      ) : entries.length === 0 ? (
        <EmptyState
          icon={view === "unread" ? BellOff : view === "snoozed" ? AlarmClock : Bell}
          title={view === "unread" ? "Nothing unread" : view === "snoozed" ? "Nothing snoozed" : kind ? `No ${groupLabel(kind)} notifications` : "No notifications yet"}
          action={
            <Button variant="secondary" onPress={() => router.push("/settings/notifications")}>
              Notification settings
            </Button>
          }
        />
      ) : (
        <View style={{ backgroundColor: theme.colors.surface, borderRadius: theme.radius.lg, borderWidth: 1, borderColor: theme.colors.border, overflow: "hidden" }}>
          {entries.map((e, i) => (
            <View key={e.key}>
              {i > 0 ? <Divider inset={s[4]} /> : null}
              <Row e={e} now={now} onPress={() => open(e)} onMore={() => setActing(e)} />
            </View>
          ))}
        </View>
      )}
      {list.hasNextPage ? (
        <Button variant="ghost" loading={list.isFetchingNextPage} onPress={() => void list.fetchNextPage()}>
          Load more
        </Button>
      ) : null}
      <ActionsSheet entry={acting} onClose={() => setActing(null)} />
    </Screen>
  );
}
