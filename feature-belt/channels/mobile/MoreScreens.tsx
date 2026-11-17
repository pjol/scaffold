import {
  activeCustomStatus,
  channelsAuthorName,
  channelsDisplayName,
  channelsLocalTime,
  channelsPlainText,
  channelsShortTime,
  channelsWhen,
  errorMessage,
  presenceLabel,
  presenceStatusText,
  useActiveOrg,
  useChannelsActivity,
  useChannelsDrafts,
  useChannelsForward,
  useChannelsMarkRead,
  useChannelsMessage,
  useChannelsProfile,
  useChannelsScheduledList,
  useChannelsSent,
  useChannelsUnreads,
  useChannelsWorkspace,
  useDeleteChannelsDraft,
  useDeleteChannelsScheduled,
  useMarkChannelsActivityRead,
  useMe,
  useOpenChannelsDM,
  useSendChannelsScheduledNow,
  useUpdateChannelsScheduled,
  useUserPresence,
  type ChannelsActivityKind,
  type ChannelsSidebarChannel,
  type ChannelsUnreadChannel,
} from "@scaffoldapp/shared";
import { Badge, Button, EmptyState, ErrorState, Field, IconButton, Input, ListGroup, ListRow, SegmentedControl, Skeleton, Spinner, Text, toast, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams, type Href } from "expo-router";
import { AtSign, CalendarClock, Check, Inbox, MessageSquareText, Pencil, Send, SendHorizontal, Trash2 } from "lucide-react-native";
import { useEffect, useMemo, useState } from "react";
import { FlatList, RefreshControl, ScrollView, View } from "react-native";
import { PresenceAvatar } from "@/features/presence";
import { EmojiGlyph } from "./emoji";
import { ListSkeleton, MessageListRow, NoOrg, messageHref } from "./ListScreens";
import { ChannelsMarkdown } from "./Markdown";

// ───────────────────────────────────────────────────────────── activity ──

/** channels/activity: mentions of you, reactions to your messages, replies in threads you follow. */
export function ActivityScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const col = theme.colors;
  const s = theme.space;
  const { org } = useActiveOrg();
  const [filter, setFilter] = useState<ChannelsActivityKind | "">("");
  const list = useChannelsActivity(org?.id, filter);
  const markRead = useMarkChannelsActivityRead(org?.id);
  const loaded = list.data !== undefined;
  useEffect(() => {
    if (loaded) markRead();
  }, [loaded, markRead]);
  if (!org) return <NoOrg title="Activity" />;
  return (
    <View style={{ flex: 1, backgroundColor: col.canvas }}>
      <Stack.Screen options={{ title: "Activity" }} />
      <View style={{ paddingHorizontal: layout.gutter, paddingVertical: s[3] }}>
        <SegmentedControl<ChannelsActivityKind | "">
          accessibilityLabel="Show"
          value={filter}
          onValueChange={setFilter}
          options={[
            { value: "", label: "All" },
            { value: "mention", label: "Mentions" },
            { value: "reaction", label: "Reactions" },
            { value: "thread_reply", label: "Threads" },
          ]}
        />
      </View>
      {list.isPending ? (
        <ListSkeleton />
      ) : list.isError ? (
        <ErrorState title="Couldn't load activity" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
      ) : (
        <FlatList
          data={list.data.items}
          keyExtractor={(a) => `${a.kind}-${a.message.id}-${a.actor?.id ?? ""}-${a.emoji}`}
          renderItem={({ item: a }) => (
            <View>
              <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], paddingTop: s[3] }}>
                <Text role="caption" weight={a.unread ? 700 : 400} tone={a.unread ? "default" : "muted"} numberOfLines={1} style={{ flexShrink: 1 }}>
                  {`${a.actor?.display_name ?? channelsAuthorName(a.message)} ${a.kind === "reaction" ? "reacted" : a.kind === "mention" ? "mentioned you" : "replied in a thread"}`}
                </Text>
                {a.kind === "reaction" ? <EmojiGlyph value={a.emoji} size={16} role="caption" /> : null}
                <Text role="caption" tone="muted">
                  {channelsShortTime(a.at)}
                </Text>
                {a.unread ? (
                  <Badge tone="danger" size="sm">
                    New
                  </Badge>
                ) : null}
              </View>
              <MessageListRow m={a.message} channel={a.channel} onPress={() => router.push(messageHref(a.message))} />
            </View>
          )}
          contentContainerStyle={{ paddingHorizontal: layout.gutter, paddingBottom: s[8], flexGrow: 1 }}
          onEndReached={() => list.hasNextPage && !list.isFetchingNextPage && void list.fetchNextPage()}
          onEndReachedThreshold={0.5}
          refreshControl={<RefreshControl refreshing={list.isRefetching && !list.isFetchingNextPage} onRefresh={() => void list.refetch()} tintColor={col.textMuted} />}
          ListFooterComponent={list.isFetchingNextPage ? <Spinner label="Loading more" /> : null}
          ListEmptyComponent={<EmptyState icon={AtSign} title="Nothing yet" />}
        />
      )}
    </View>
  );
}

// ───────────────────────────────────────────────────────────── all unread ──

function UnreadGroup({ group, meId }: { group: ChannelsUnreadChannel; meId: string | undefined }) {
  const theme = useTheme();
  const col = theme.colors;
  const s = theme.space;
  const c = group.channel;
  const markRead = useChannelsMarkRead(c.id, c.org_id);
  const last = group.messages[group.messages.length - 1];
  return (
    <View style={{ gap: s[2], padding: s[4], marginBottom: s[3], borderRadius: theme.radius.lg, borderWidth: 1, borderColor: col.border, backgroundColor: col.surface }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <Text role="label" weight={700} numberOfLines={1} style={{ flex: 1 }} onPress={() => router.push(`/channels/${c.id}`)}>
          {channelsDisplayName(c)}
        </Text>
        {c.mentions > 0 ? (
          <Badge tone="danger" variant="solid" size="sm">
            {String(c.mentions)}
          </Badge>
        ) : null}
        <Button size="md" variant="ghost" leftIcon={Check} onPress={() => last && markRead(last)}>
          Mark read
        </Button>
      </View>
      {group.messages.map((m) => (
        <View key={m.id} style={{ gap: s[1] }}>
          <Text role="caption" weight={600}>
            {`${channelsAuthorName(m, meId)} · ${channelsShortTime(m.created_at)}`}
          </Text>
          {m.deleted ? (
            <Text role="body" tone="muted" style={{ fontStyle: "italic" }}>
              Message deleted
            </Text>
          ) : (
            <ChannelsMarkdown body={m.body || (m.attachments.length ? "📎" : " ")} mentions={m.mentions} meId={meId} numberOfLines={4} />
          )}
        </View>
      ))}
      {group.has_more ? (
        <Button size="md" variant="ghost" onPress={() => router.push(`/channels/${c.id}`)}>
          See all unread
        </Button>
      ) : null}
    </View>
  );
}

/** channels/unreads: every conversation's unread messages on one page. */
export function UnreadsScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const col = theme.colors;
  const { org } = useActiveOrg();
  const me = useMe();
  const list = useChannelsUnreads(org?.id);
  if (!org) return <NoOrg title="All unread" />;
  return (
    <View style={{ flex: 1, backgroundColor: col.canvas }}>
      <Stack.Screen options={{ title: "All unread" }} />
      {list.isPending ? (
        <ListSkeleton />
      ) : list.isError ? (
        <ErrorState title="Couldn't load unread messages" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
      ) : (
        <FlatList
          data={list.data.items}
          keyExtractor={(g) => g.channel.id}
          renderItem={({ item }) => <UnreadGroup group={item} meId={me.data?.id} />}
          contentContainerStyle={{ paddingHorizontal: layout.gutter, paddingVertical: theme.space[4], flexGrow: 1 }}
          refreshControl={<RefreshControl refreshing={list.isRefetching} onRefresh={() => void list.refetch()} tintColor={col.textMuted} />}
          ListEmptyComponent={<EmptyState icon={Inbox} title="You're all caught up" />}
        />
      )}
    </View>
  );
}

// ───────────────────────────────────────────────────────────── drafts & sent ──

/** channels/drafts: drafts, scheduled messages and what you sent. */
export function DraftsScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const col = theme.colors;
  const s = theme.space;
  const params = useLocalSearchParams<{ tab?: string }>();
  const { org } = useActiveOrg();
  const [tab, setTab] = useState<"drafts" | "scheduled" | "sent">(params.tab === "scheduled" || params.tab === "sent" ? params.tab : "drafts");
  const drafts = useChannelsDrafts(tab === "drafts" ? org?.id : null);
  const scheduled = useChannelsScheduledList(tab === "scheduled" ? org?.id : null);
  const sent = useChannelsSent(tab === "sent" ? org?.id : null);
  const removeDraft = useDeleteChannelsDraft(org?.id);
  const update = useUpdateChannelsScheduled(org?.id);
  const remove = useDeleteChannelsScheduled(org?.id);
  const sendNow = useSendChannelsScheduledNow(org?.id);
  const err = (e: unknown) => toast.error(errorMessage(e));
  if (!org) return <NoOrg title="Drafts & sent" />;
  const listProps = { contentContainerStyle: { paddingHorizontal: layout.gutter, paddingBottom: s[8], flexGrow: 1 } };
  let body;
  if (tab === "drafts") {
    body = drafts.isPending ? (
      <ListSkeleton />
    ) : drafts.isError ? (
      <ErrorState title="Couldn't load drafts" info={drafts.error.message} onRetry={() => void drafts.refetch()} retrying={drafts.isFetching} />
    ) : (
      <FlatList
        {...listProps}
        data={drafts.data.items}
        keyExtractor={(d) => d.id}
        renderItem={({ item: d }) => (
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], paddingVertical: s[3], borderBottomWidth: 1, borderBottomColor: col.border }}>
            <Pencil color={col.textMuted} size={18} />
            <View style={{ flex: 1, minWidth: 0, gap: s[1] }}>
              <Text role="caption" tone="muted" numberOfLines={1}>
                {`${channelsDisplayName(d.channel)}${d.thread_root_id ? " · in a thread" : ""} · ${channelsShortTime(d.updated_at)}`}
              </Text>
              <Text role="body" numberOfLines={2} onPress={() => router.push((d.thread_root_id ? `/channels/thread/${d.thread_root_id}` : `/channels/${d.channel.id}`) as Href)}>
                {channelsPlainText(d.body, d.mentions)}
              </Text>
            </View>
            <IconButton label="Delete draft" icon={Trash2} variant="ghost" onPress={() => removeDraft.mutate(d, { onError: err })} />
          </View>
        )}
        ListEmptyComponent={<EmptyState icon={Pencil} title="No drafts" />}
      />
    );
  } else if (tab === "scheduled") {
    body = scheduled.isPending ? (
      <ListSkeleton />
    ) : scheduled.isError ? (
      <ErrorState title="Couldn't load scheduled messages" info={scheduled.error.message} onRetry={() => void scheduled.refetch()} retrying={scheduled.isFetching} />
    ) : (
      <FlatList
        {...listProps}
        data={scheduled.data.items}
        keyExtractor={(x) => x.id}
        renderItem={({ item: x }) => (
          <View style={{ gap: s[2], paddingVertical: s[3], borderBottomWidth: 1, borderBottomColor: col.border }}>
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
              <CalendarClock color={x.status === "failed" ? col.dangerStrong : col.textMuted} size={18} />
              <Text role="caption" tone={x.status === "failed" ? "danger" : "muted"} numberOfLines={1} style={{ flex: 1 }}>
                {`${channelsDisplayName(x.channel)} · ${x.status === "failed" ? "Not sent" : `Sends ${channelsWhen(x.send_at)}`}`}
              </Text>
            </View>
            {x.body ? <ChannelsMarkdown body={x.body} mentions={x.mentions} numberOfLines={3} /> : null}
            {x.status === "failed" ? (
              <Text role="label" tone="danger">
                {x.error}
              </Text>
            ) : null}
            <View style={{ flexDirection: "row", gap: s[1], flexWrap: "wrap" }}>
              <Button size="md" variant="secondary" leftIcon={Send} loading={sendNow.isPending} onPress={() => sendNow.mutate(x.id, { onSuccess: () => toast.success("Sent."), onError: err })}>
                Send now
              </Button>
              <Button size="md" variant="ghost" onPress={() => update.mutate({ id: x.id, when: "tomorrow at 9am" }, { onSuccess: () => toast.success("Moved to tomorrow at 9:00 AM."), onError: err })}>
                Tomorrow 9:00
              </Button>
              <IconButton label="Delete scheduled message" icon={Trash2} variant="ghost" onPress={() => remove.mutate(x.id, { onError: err })} />
            </View>
          </View>
        )}
        ListEmptyComponent={<EmptyState icon={CalendarClock} title="Nothing scheduled" />}
      />
    );
  } else {
    body = sent.isPending ? (
      <ListSkeleton />
    ) : sent.isError ? (
      <ErrorState title="Couldn't load sent messages" info={sent.error.message} onRetry={() => void sent.refetch()} retrying={sent.isFetching} />
    ) : (
      <FlatList
        {...listProps}
        data={sent.data.items}
        keyExtractor={(i) => i.message.id}
        renderItem={({ item }) => <MessageListRow m={item.message} channel={item.channel} onPress={() => router.push(messageHref(item.message))} />}
        onEndReached={() => sent.hasNextPage && !sent.isFetchingNextPage && void sent.fetchNextPage()}
        onEndReachedThreshold={0.5}
        ListEmptyComponent={<EmptyState icon={SendHorizontal} title="Nothing sent yet" />}
      />
    );
  }
  return (
    <View style={{ flex: 1, backgroundColor: col.canvas }}>
      <Stack.Screen options={{ title: "Drafts & sent" }} />
      <View style={{ paddingHorizontal: layout.gutter, paddingVertical: s[3] }}>
        <SegmentedControl<"drafts" | "scheduled" | "sent">
          accessibilityLabel="Show"
          value={tab}
          onValueChange={setTab}
          options={[
            { value: "drafts", label: "Drafts" },
            { value: "scheduled", label: "Scheduled" },
            { value: "sent", label: "Sent" },
          ]}
        />
      </View>
      <View style={{ flex: 1 }}>{body}</View>
    </View>
  );
}

// ───────────────────────────────────────────────────────────── profile ──

/** channels/person/[id]?org=: a person's card — title, local time, status, teams; message them. */
export function PersonScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const s = theme.space;
  const { id, org: orgParam } = useLocalSearchParams<{ id: string; org?: string }>();
  const { org } = useActiveOrg();
  const orgId = orgParam || org?.id;
  const card = useChannelsProfile(orgId, id);
  const presence = useUserPresence(id ?? null);
  const status = activeCustomStatus(presence);
  const openDM = useOpenChannelsDM(orgId ?? "");
  const local = card.data ? channelsLocalTime(card.data.timezone) : null;
  return (
    <ScrollView style={{ flex: 1, backgroundColor: theme.colors.canvas }} contentContainerStyle={{ padding: layout.gutter, gap: s[6] }}>
      <Stack.Screen options={{ title: card.data?.display_name ?? "Profile" }} />
      {card.isPending ? (
        <View style={{ gap: s[3] }}>
          <Skeleton width={72} height={72} shape="circle" />
          <Skeleton width="60%" height={20} />
        </View>
      ) : card.isError ? (
        <ErrorState title="Couldn't load this profile" info={card.error.message} onRetry={() => void card.refetch()} retrying={card.isFetching} />
      ) : (
        <>
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[4] }}>
            <PresenceAvatar userId={card.data.deleted ? null : card.data.id} name={card.data.display_name} src={card.data.avatar_url} size="lg" />
            <View style={{ flex: 1, gap: s[1] }}>
              <Text role="title">{card.data.display_name}</Text>
              {card.data.title ? <Text role="body">{card.data.title}</Text> : null}
              <Text role="label" tone="muted">
                {presenceLabel(presence)}
              </Text>
            </View>
          </View>
          <ListGroup>
            {status ? <ListRow title="Status" trailing={<Text tone="muted">{presenceStatusText(status)}</Text>} /> : null}
            {local ? <ListRow title="Local time" trailing={<Text tone="muted">{local}</Text>} /> : null}
            {card.data.org_role !== "member" ? <ListRow title="Role" trailing={<Text tone="muted">{card.data.org_role === "owner" ? "Owner" : "Admin"}</Text>} /> : null}
            {card.data.teams.length ? <ListRow title="Teams" trailing={<Text tone="muted">{card.data.teams.map((t) => `@${t.handle}`).join(", ")}</Text>} /> : null}
          </ListGroup>
          {!card.data.deleted ? (
            <Button
              leftIcon={MessageSquareText}
              loading={openDM.isPending}
              onPress={() => openDM.mutate([card.data.id], { onSuccess: (d) => router.replace(`/channels/${d.id}`), onError: (e) => toast.error(errorMessage(e)) })}
            >
              Message
            </Button>
          ) : null}
        </>
      )}
    </ScrollView>
  );
}

// ───────────────────────────────────────────────────────────── forward ──

/** channels/forward/[id]: pick a conversation and forward a message there with a comment. */
export function ForwardScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const col = theme.colors;
  const s = theme.space;
  const { id } = useLocalSearchParams<{ id: string }>();
  const original = useChannelsMessage(id);
  const orgId = original.data?.channel.org_id;
  const ws = useChannelsWorkspace(orgId);
  const forward = useChannelsForward();
  const [q, setQ] = useState("");
  const [comment, setComment] = useState("");
  const [target, setTarget] = useState<ChannelsSidebarChannel | null>(null);
  const list = useMemo(() => {
    const query = q.trim().toLowerCase();
    return (ws.data?.channels ?? []).filter((c) => !c.archived && (!query || c.name.toLowerCase().includes(query))).slice(0, 60);
  }, [ws.data, q]);
  if (original.isError) return <ErrorState title="Couldn't load the message" info={original.error.message} onRetry={() => void original.refetch()} retrying={original.isFetching} />;
  return (
    <View style={{ flex: 1, backgroundColor: col.canvas }}>
      <Stack.Screen options={{ title: "Forward" }} />
      <View style={{ padding: layout.gutter, gap: s[3] }}>
        {original.data ? (
          <View style={{ padding: s[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: col.border, backgroundColor: col.surface }}>
            <Text role="caption" tone="muted">
              {`${channelsAuthorName(original.data.message)} · ${channelsDisplayName(original.data.channel)}`}
            </Text>
            <ChannelsMarkdown body={original.data.message.body || " "} mentions={original.data.message.mentions} numberOfLines={3} />
          </View>
        ) : (
          <Skeleton width="100%" height={56} />
        )}
        <Field label="Add a message">
          <Input value={comment} onChangeText={setComment} multiline maxLength={4000} />
        </Field>
        {target ? (
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
            <Text role="body" weight={600} numberOfLines={1} style={{ flex: 1 }}>
              {`To ${channelsDisplayName(target)}`}
            </Text>
            <Button size="md" variant="ghost" onPress={() => setTarget(null)}>
              Change
            </Button>
          </View>
        ) : (
          <Input value={q} onChangeText={setQ} placeholder="Search conversations" accessibilityLabel="Search conversations" autoCapitalize="none" />
        )}
        <Button
          disabled={!target || !original.data}
          loading={forward.isPending}
          onPress={() =>
            target &&
            original.data &&
            forward.mutate(
              { message: original.data.message, channel_id: target.id, comment: comment.trim() },
              {
                onSuccess: (m) => {
                  toast.success(`Forwarded to ${channelsDisplayName(target)}.`);
                  router.replace(`/channels/${m.channel_id}`);
                },
                onError: (e) => toast.error(errorMessage(e)),
              },
            )
          }
        >
          Forward
        </Button>
      </View>
      {!target ? (
        <FlatList
          data={list}
          keyExtractor={(c) => c.id}
          renderItem={({ item }) => <ListRow title={channelsDisplayName(item)} onPress={() => setTarget(item)} />}
          contentContainerStyle={{ paddingHorizontal: layout.gutter, paddingBottom: s[8], flexGrow: 1 }}
          keyboardShouldPersistTaps="handled"
          ListEmptyComponent={ws.isPending ? <ListSkeleton /> : <EmptyState icon={MessageSquareText} title="No conversations match" />}
        />
      ) : null}
    </View>
  );
}
