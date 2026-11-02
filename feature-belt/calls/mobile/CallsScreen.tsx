import {
  callsHistoryTitle,
  errorMessage,
  formatCallsDuration,
  formatCallsWhen,
  parseCallsCode,
  useActiveOrg,
  useCallsHistory,
  useCallsPlatformMeetings,
  useCallsTimeZone,
  useCallsUpcoming,
  useCreateCallsMeeting,
  useMe,
  type CallsHistoryItem,
  type CallsMeeting,
  type CallsPlatformMeeting,
} from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, IconButton, Input, ListGroup, ListRow, ProviderIcon, Sheet, SkeletonRow, SourceBadge, Spinner, Text, TitleSm, toast, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, type Href } from "expo-router";
import { CalendarClock, CalendarPlus, ExternalLink, LogIn, Phone, PhoneIncoming, PhoneMissed, Plus, Share2, Video } from "lucide-react-native";
import { useState, type ReactElement } from "react";
import { FlatList, Linking, RefreshControl, Share, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { callsScheduleAction, callsSourceLink } from "./seams";

/** Calls: Start call, New meeting (a link to share, or scheduled), join by code, upcoming meetings and your history. */
export function CallsScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const insets = useSafeAreaInsets();
  const history = useCallsHistory();
  const [creating, setCreating] = useState(false);
  const s = theme.space;
  const pad = { paddingLeft: insets.left + layout.gutter, paddingRight: insets.right + layout.gutter };

  let body: ReactElement | undefined;
  if (history.isPending) {
    body = (
      <View style={{ gap: s[2] }}>
        {Array.from({ length: 5 }, (_, i) => (
          <SkeletonRow key={i} />
        ))}
      </View>
    );
  } else if (history.isError) {
    body = <ErrorState title="Couldn't load your calls" info={history.error.message} onRetry={() => void history.refetch()} retrying={history.isFetching} />;
  }

  return (
    <View style={{ flex: 1, backgroundColor: theme.colors.canvas }}>
      <Stack.Screen
        options={{
          title: "Calls",
          headerRight: () => (
            <View style={{ flexDirection: "row", gap: s[1] }}>
              <IconButton label="Start call" icon={Phone} onPress={() => router.push("/calls/new")} />
              <IconButton label="New meeting" icon={Plus} variant="soft" onPress={() => setCreating(true)} />
              <ResourceMenu feature="calls" title="Calls" />
            </View>
          ),
        }}
      />
      <FlatList
        data={history.data?.items ?? []}
        keyExtractor={(h) => `${h.type}:${h.id}`}
        renderItem={({ item }) => <HistoryRow item={item} />}
        ItemSeparatorComponent={() => <View style={{ height: 1, backgroundColor: theme.colors.border }} />}
        contentContainerStyle={{ ...pad, paddingTop: s[4], paddingBottom: insets.bottom + s[8], gap: 0 }}
        ListHeaderComponent={
          <View style={{ gap: s[8], paddingBottom: s[3] }}>
            <JoinByCode />
            <Upcoming />
            <TitleSm>Recent</TitleSm>
          </View>
        }
        ListEmptyComponent={body ?? <EmptyState icon={Video} title="No calls yet" compact />}
        onEndReached={() => history.hasNextPage && !history.isFetchingNextPage && void history.fetchNextPage()}
        onEndReachedThreshold={0.5}
        ListFooterComponent={history.isFetchingNextPage ? <Spinner label="Loading more" /> : null}
        refreshControl={<RefreshControl refreshing={history.isRefetching && !history.isFetchingNextPage} onRefresh={() => void history.refetch()} tintColor={theme.colors.textMuted} />}
      />
      <NewMeetingSheet open={creating} onClose={() => setCreating(false)} />
    </View>
  );
}

function JoinByCode() {
  const theme = useTheme();
  const [value, setValue] = useState("");
  const [bad, setBad] = useState(false);
  const join = () => {
    const code = parseCallsCode(value);
    if (!code) return setBad(true);
    setValue("");
    router.push({ pathname: "/meet/[code]", params: { code } });
  };
  return (
    <View style={{ gap: theme.space[2] }}>
      <View style={{ flexDirection: "row", gap: theme.space[2], alignItems: "center" }}>
        <Input
          value={value}
          onChangeText={(t) => (setValue(t), setBad(false))}
          placeholder="Enter a code or link"
          accessibilityLabel="Meeting code or link"
          autoCapitalize="none"
          autoCorrect={false}
          returnKeyType="go"
          onSubmitEditing={join}
          leadingIcon={LogIn}
          invalid={bad}
          containerStyle={{ flex: 1 }}
        />
        <Button variant="secondary" onPress={join} disabled={!value.trim()}>
          Join
        </Button>
      </View>
      {bad && (
        <Text role="label" tone="danger">
          Enter a code like abc-defg-hij, or paste the link.
        </Text>
      )}
    </View>
  );
}

function Upcoming() {
  const theme = useTheme();
  const upcoming = useCallsUpcoming();
  // Meet, Teams and Zoom meetings on the person's calendars, in time order with the app's own.
  const platform = useCallsPlatformMeetings("upcoming");
  if (upcoming.isPending) return <SkeletonRow />;
  if (upcoming.isError) return <ErrorState compact title="Couldn't load upcoming meetings" onRetry={() => void upcoming.refetch()} retrying={upcoming.isFetching} />;
  const rows = [
    ...upcoming.data.items.slice(0, 5).map((m) => ({ at: m.starts_at ?? "", node: <UpcomingRow key={`app:${m.id}`} meeting={m} /> })),
    ...(platform.data?.items ?? []).slice(0, 8).map((m) => ({ at: m.starts_at, node: <PlatformMeetingRow key={`ext:${m.id}`} meeting={m} /> })),
  ].sort((a, b) => a.at.localeCompare(b.at));
  return (
    <View style={{ gap: theme.space[3] }}>
      <TitleSm>Upcoming</TitleSm>
      {rows.length === 0 ? (
        <EmptyState icon={CalendarClock} title="Nothing scheduled" compact />
      ) : (
        <ListGroup>{rows.map((r) => r.node)}</ListGroup>
      )}
    </View>
  );
}

const PLATFORMS: Record<string, { label: string; platform: string; product: string | null }> = {
  google_meet: { label: "Google Meet", platform: "google", product: "meet" },
  teams: { label: "Microsoft Teams", platform: "microsoft", product: "teams" },
  zoom: { label: "Zoom", platform: "zoom", product: null },
};

/** A Meet, Teams or Zoom meeting from the person's calendars: tap joins in the platform's own app. */
function PlatformMeetingRow({ meeting: m }: { meeting: CallsPlatformMeeting }) {
  const theme = useTheme();
  const zone = useCallsTimeZone();
  const p = PLATFORMS[m.provider];
  const title = m.title || "Meeting";
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
      <View style={{ flex: 1 }}>
        <ListRow
          title={title}
          leading={p ? <ProviderIcon provider={p.platform} product={p.product} size={20} /> : <Video size={20} color={theme.colors.textMuted} />}
          trailing={
            <View style={{ alignItems: "flex-end", gap: theme.space[1] }}>
              <Text role="label" tone="muted">
                {formatCallsWhen(m.starts_at, zone)}
              </Text>
              <SourceBadge source={m.source} iconOnly />
            </View>
          }
          onPress={() => void Linking.openURL(m.join_url)}
          accessibilityHint={`Joins on ${p?.label ?? "the platform"}`}
        />
      </View>
      <ResourceMenu
        feature={m.source?.type === "connected" ? `connectors:${m.source.connection_id}` : "calendar"}
        {...(m.source?.type === "connected" ? {} : { kind: "calendar.event", id: m.id })}
        title={title}
        actions={[
          { id: "join", label: `Join on ${p?.label ?? "the platform"}`, icon: ExternalLink, onSelect: () => void Linking.openURL(m.join_url) },
          { id: "share", label: "Share link", icon: Share2, onSelect: () => void Share.share({ message: m.join_url, url: m.join_url }) },
        ]}
      />
    </View>
  );
}

/** A scheduled meeting: tap to open it; ⋮ shares the link and, for one another feature made, opens it there. */
function UpcomingRow({ meeting: m }: { meeting: CallsMeeting }) {
  const theme = useTheme();
  const zone = useCallsTimeZone();
  const source = callsSourceLink(m.source);
  const title = m.title || "Meeting";
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
      <View style={{ flex: 1 }}>
        <ListRow
          title={title}
          leading={<CalendarClock size={20} color={theme.colors.primaryStrong} />}
          trailing={<Text role="label" tone={m.live_since ? "success" : "muted"}>{m.live_since ? "Live" : m.starts_at ? formatCallsWhen(m.starts_at, zone) : ""}</Text>}
          onPress={() => router.push({ pathname: "/meet/[code]", params: { code: m.code } })}
          accessibilityHint="Opens the meeting"
        />
      </View>
      <ResourceMenu
        feature="calls"
        kind="calls.meeting"
        id={m.id}
        title={title}
        {...(m.org_id ? { orgId: m.org_id } : {})}
        actions={[
          { id: "share", label: "Share link", icon: Share2, onSelect: () => void Share.share({ message: m.join_url, url: m.join_url }) },
          ...(source ? [{ id: "source", label: source.label, icon: ExternalLink, onSelect: () => router.push(source.href as Href) }] : []),
        ]}
      />
    </View>
  );
}

function HistoryRow({ item }: { item: CallsHistoryItem }) {
  const theme = useTheme();
  const me = useMe();
  const zone = useCallsTimeZone();
  const missed = item.type === "missed";
  const title = callsHistoryTitle(item, me.data?.id);
  const Icon = missed ? PhoneMissed : item.meeting.kind === "call" ? PhoneIncoming : Video;
  const when = formatCallsWhen(item.started_at, zone);
  const meta = missed ? `${item.ring_status === "declined" ? "Declined" : "Missed"} · ${when}` : item.live ? `Live now · ${when}` : `${when} · ${formatCallsDuration(item.duration_seconds)}`;
  const open = () => (missed ? router.push({ pathname: "/meet/[code]", params: { code: item.meeting.code } }) : router.push({ pathname: "/calls/[id]", params: { id: item.id } }));
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
      <View style={{ flex: 1 }}>
        <ListRow
          title={title}
          leading={<Icon size={20} color={missed ? theme.colors.dangerStrong : theme.colors.textMuted} />}
          trailing={<Text role="caption" tone="muted">{meta}</Text>}
          onPress={open}
          destructive={missed}
        />
      </View>
      <ResourceMenu
        feature="calls"
        kind="calls.meeting"
        id={item.meeting.id}
        title={title}
        actions={[{ id: "open", label: item.live ? "Join" : "Open meeting", icon: Video, onSelect: () => router.push({ pathname: "/meet/[code]", params: { code: item.meeting.code } }) }]}
      />
    </View>
  );
}

/** New meeting: a link to share, or (when the app sets an action) a scheduled one. Instant calls are Start call. */
function NewMeetingSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const { org } = useActiveOrg();
  const create = useCreateCallsMeeting();
  const [next, setNext] = useState<(() => void) | null>(null);
  const scheduleAction = callsScheduleAction();
  const schedule = () => {
    if (!scheduleAction) return;
    setNext(() => () => scheduleAction({ navigate: (href) => router.push(href as Href), orgId: org?.id ?? null }));
    onClose();
  };
  const makeLink = () =>
    create.mutate(
      { access: "open", org_id: org?.id ?? null },
      {
        onSuccess: (m) => {
          setNext(() => () => void Share.share({ message: m.join_url, url: m.join_url }));
          onClose();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't create a meeting. Try again.")),
      },
    );
  return (
    <Sheet
      open={open}
      onClose={onClose}
      onDismissed={() => {
        next?.();
        setNext(null);
      }}
      title="New meeting"
    >
      <View style={{ gap: theme.space[3] }}>
        <ListGroup>
          <ListRow title="Get a link to share" leading={<Share2 size={20} color={theme.colors.text} />} onPress={makeLink} disabled={create.isPending} />
          {scheduleAction ? <ListRow title="Schedule a meeting" leading={<CalendarPlus size={20} color={theme.colors.text} />} onPress={schedule} disabled={create.isPending} /> : null}
        </ListGroup>
        {create.isPending && <Spinner label="Creating" />}
      </View>
    </Sheet>
  );
}
