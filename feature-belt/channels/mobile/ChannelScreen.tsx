import {
  channelsDayLabel,
  channelsDisplayName,
  channelsGroupsWith,
  channelsReplyCount,
  channelsSameDay,
  channelsShortTime,
  channelsTypingText,
  errorMessage,
  flattenChannelsPages,
  isChannelsDirect,
  useChannel,
  useChannelsMarkRead,
  useChannelsMessages,
  useChannelsReact,
  useChannelsSend,
  useChannelsTopic,
  useChannelsTopics,
  useChannelsTypingUsers,
  useChannelsWorkspace,
  useJoinChannel,
  useMe,
  useStartChannelCall,
  type ChannelsCachedMessage,
  type ChannelsDetail,
  type ChannelsMessage,
} from "@scaffoldapp/shared";
import {
  AvatarGroup,
  Button,
  Chip,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  PhotoLightbox,
  Screen,
  Sheet,
  Skeleton,
  SkeletonRow,
  Spinner,
  Text,
  TextArea,
  ToggleChip,
  haptic,
  toast,
  useLayout,
  useTheme,
  type GalleryPhoto,
} from "@scaffoldapp/ui-native";
import { Stack, router, useFocusEffect, useLocalSearchParams, type Href } from "expo-router";
import { ArrowDown, Hash, LayoutList, Lock, MessagesSquare, Phone, Plus } from "lucide-react-native";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AppState, FlatList, Pressable, RefreshControl, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { PresenceAvatar } from "@/features/presence";
import { useChannelMenuActions } from "./ChannelMenu";
import { Composer } from "./Composer";
import { ChannelsMarkdown } from "./Markdown";
import { useMessageActions } from "./MessageActions";
import { MessageRow } from "./MessageRow";

function HeaderTitle({ c }: { c: ChannelsDetail }) {
  const theme = useTheme();
  const col = theme.colors;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${channelsDisplayName(c)}. Details`}
      onPress={() => router.push(`/channels/${c.id}/info`)}
      style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], maxWidth: 220 }}
    >
      {c.kind === "dm" && c.people[0] ? (
        <PresenceAvatar userId={c.people[0].id} name={c.people[0].display_name} src={c.people[0].avatar_url} size="xs" />
      ) : c.kind === "group_dm" ? (
        <AvatarGroup people={c.people.map((p) => ({ name: p.display_name, src: p.avatar_url }))} size="xs" max={2} />
      ) : c.layout === "forum" ? (
        <LayoutList color={col.textMuted} size={18} />
      ) : c.kind === "private" ? (
        <Lock color={col.textMuted} size={18} />
      ) : (
        <Hash color={col.textMuted} size={18} />
      )}
      <Text role="body" weight={600} numberOfLines={1} style={{ flexShrink: 1 }} maxFontSizeMultiplier={1.5}>
        {c.name}
      </Text>
    </Pressable>
  );
}

function HeaderRight({ c }: { c: ChannelsDetail }) {
  const config = useChannelsWorkspace(c.org_id).data?.config;
  const call = useStartChannelCall(c.id);
  const menu = useChannelMenuActions(c);
  return (
    <View style={{ flexDirection: "row", alignItems: "center" }}>
      {config?.calls_enabled && c.permissions.post ? (
        <IconButton
          label="Start call"
          icon={Phone}
          variant="ghost"
          loading={call.isPending}
          onPress={() => call.mutate(undefined, { onSuccess: (r) => router.push(r.path as Href), onError: (e) => toast.error(errorMessage(e, "Couldn't start the call.")) })}
        />
      ) : null}
      <ResourceMenu feature="channels" kind="channels.channel" id={c.id} title={channelsDisplayName(c)} orgId={c.org_id} actions={menu.actions} label="Channel options" />
      {menu.element}
    </View>
  );
}

/** channels/<id>: the chat timeline or a forum's posts, with the composer. */
export function ChannelScreen() {
  const params = useLocalSearchParams<{ id: string; message?: string }>();
  const id = params.id ?? "";
  const around = params.message || null;
  const theme = useTheme();
  const me = useMe();
  const channel = useChannel(id);
  useChannelsTopic(id);

  if (channel.isPending) {
    return (
      <Screen scroll={false} edges={["bottom"]} gap={theme.space[4]} contentStyle={{ justifyContent: "flex-end" }}>
        <Stack.Screen options={{ title: "" }} />
        <Skeleton width="65%" height={40} />
        <Skeleton width="50%" height={40} />
        <Skeleton width="60%" height={64} />
      </Screen>
    );
  }
  if (channel.isError) {
    return (
      <Screen centered edges={["bottom"]}>
        <Stack.Screen options={{ title: "" }} />
        {channel.error.status === 404 ? (
          <EmptyState
            icon={MessagesSquare}
            title="Channel not found"
            action={
              <Button variant="secondary" onPress={() => router.replace("/channels")}>
                Back to channels
              </Button>
            }
          />
        ) : (
          <ErrorState title="Couldn't load this channel" info={channel.error.message} onRetry={() => void channel.refetch()} retrying={channel.isFetching} />
        )}
      </Screen>
    );
  }
  const c = channel.data;
  const header = <Stack.Screen options={{ headerTitle: () => <HeaderTitle c={c} />, headerRight: () => <HeaderRight c={c} /> }} />;
  if (c.layout === "forum") {
    return (
      <>
        {header}
        <Forum key={id} c={c} />
      </>
    );
  }
  return (
    <>
      {header}
      <Timeline key={id} c={c} around={around} meId={me.data?.id} suspended={me.data?.status === "suspended"} />
    </>
  );
}

/** What replaces the composer when you can't post here. */
function ComposerNotice({ c, suspended }: { c: ChannelsDetail; suspended: boolean }) {
  const theme = useTheme();
  const join = useJoinChannel(c.id);
  let text: string | null = null;
  if (suspended) text = "Your account is suspended. You can read but not send.";
  else if (c.archived) text = "This channel is archived.";
  else if (!c.member) text = `You're viewing ${channelsDisplayName(c)}.`;
  if (!text) return null;
  return (
    <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "center", gap: theme.space[3], minHeight: theme.tap.base }}>
      <Text role="label" tone="muted" style={{ flexShrink: 1 }}>
        {text}
      </Text>
      {!suspended && !c.archived && !c.member ? (
        <Button size="md" loading={join.isPending} onPress={() => join.mutate(undefined, { onError: (e) => toast.error(errorMessage(e)) })}>
          Join channel
        </Button>
      ) : null}
    </View>
  );
}

function Timeline({ c, around, meId, suspended }: { c: ChannelsDetail; around: string | null; meId: string | undefined; suspended: boolean }) {
  const theme = useTheme();
  const layout = useLayout();
  const s = theme.space;
  const thread = useChannelsMessages(c.id, around);
  const messages = useMemo(() => flattenChannelsPages(thread.data), [thread.data]);
  const data = useMemo(() => [...messages].reverse(), [messages]);
  const typing = useChannelsTypingUsers(c.id);
  const markRead = useChannelsMarkRead(c.id, c.org_id);
  const react = useChannelsReact();
  const sender = useChannelsSend(c.id);
  const [editing, setEditing] = useState<ChannelsCachedMessage | null>(null);
  const [lightbox, setLightbox] = useState<{ photos: GalleryPhoto[]; index: number } | null>(null);
  const [highlight, setHighlight] = useState<string | null>(null);
  const [nearNewest, setNearNewest] = useState(true);
  const listRef = useRef<FlatList<ChannelsCachedMessage>>(null);
  const canPost = c.permissions.post && !suspended;
  const openThread = useCallback((m: ChannelsMessage) => router.push(`/channels/thread/${m.thread_root_id ?? m.id}`), []);
  const actions = useMessageActions({ channel: c, meId, onReplyInThread: openThread, onEdit: setEditing });

  // The "New" divider sits above the first message after your read pointer when the screen opened.
  const readAt = useRef(c.last_read_at);
  const firstUnread = useMemo(() => {
    const at = readAt.current;
    if (!at || !c.member) return null;
    return messages.find((m) => m.kind !== "system" && m.sender?.id !== meId && !m.status && Date.parse(m.created_at) > Date.parse(at))?.id ?? null;
  }, [messages, meId, c.member]);

  const latest = data.find((m) => !m.status);
  const [focused, setFocused] = useState(false);
  useFocusEffect(
    useCallback(() => {
      setFocused(true);
      return () => setFocused(false);
    }, []),
  );
  useEffect(() => {
    if (!focused || !nearNewest || thread.hasPreviousPage || !latest || !c.member) return undefined;
    if (AppState.currentState === "active") markRead(latest);
    const sub = AppState.addEventListener("change", (state) => state === "active" && markRead(latest));
    return () => sub.remove();
  }, [focused, nearNewest, thread.hasPreviousPage, latest, markRead, c.member]);

  const jumpTo = useCallback(
    (messageId: string) => {
      const index = data.findIndex((m) => m.id === messageId);
      if (index < 0) {
        router.setParams({ message: messageId });
        return;
      }
      listRef.current?.scrollToIndex({ index, viewPosition: 0.5, animated: true });
      setHighlight(messageId);
    },
    [data],
  );
  const jumped = useRef(false);
  useEffect(() => {
    if (!around || jumped.current || data.length === 0) return undefined;
    jumped.current = true;
    const t = setTimeout(() => jumpTo(around), 50);
    return () => clearTimeout(t);
  }, [around, data.length, jumpTo]);
  useEffect(() => {
    if (!highlight) return undefined;
    const t = setTimeout(() => setHighlight(null), 2500);
    return () => clearTimeout(t);
  }, [highlight]);

  const goLatest = () => {
    if (around || thread.hasPreviousPage) router.setParams({ message: "" });
    else listRef.current?.scrollToOffset({ offset: 0, animated: true });
  };

  const footer = (
    <View style={{ gap: s[2] }}>
      <Text role="caption" tone="muted" accessibilityLiveRegion="polite" style={{ opacity: typing.length ? 1 : 0 }}>
        {channelsTypingText(typing) || " "}
      </Text>
      {canPost ? (
        <Composer
          channel={c}
          editing={editing}
          onCancelEdit={() => setEditing(null)}
          placeholder={`Message ${channelsDisplayName(c)}`}
          onSent={() => {
            if (around || thread.hasPreviousPage) router.setParams({ message: "" });
            else listRef.current?.scrollToOffset({ offset: 0, animated: true });
          }}
        />
      ) : (
        <ComposerNotice c={c} suspended={suspended} />
      )}
    </View>
  );

  return (
    <Screen scroll={false} padded={false} edges={["bottom"]} keyboard gap={0} footer={footer}>
      {thread.isPending ? (
        <View style={{ flex: 1, justifyContent: "flex-end", gap: s[4], paddingHorizontal: layout.gutter, paddingBottom: s[4] }}>
          {Array.from({ length: 4 }, (_, i) => (
            <SkeletonRow key={i} />
          ))}
        </View>
      ) : thread.isError ? (
        <ErrorState title="Couldn't load messages" info={thread.error.message} onRetry={() => void thread.refetch()} retrying={thread.isFetching} />
      ) : (
        <View style={{ flex: 1 }}>
          <FlatList
            ref={listRef}
            inverted
            data={data}
            keyExtractor={(item) => (item.client_id && item.sender?.id === meId ? `c-${item.client_id}` : item.id)}
            renderItem={({ item, index }) => {
              const older = data[index + 1];
              const newDay = !older || !channelsSameDay(older.created_at, item.created_at);
              const unreadHere = item.id === firstUnread;
              return (
                <MessageRow
                  message={item}
                  meId={meId}
                  grouped={!newDay && !unreadHere && channelsGroupsWith(older, item)}
                  dayLabel={newDay ? channelsDayLabel(item.created_at) : undefined}
                  firstUnread={unreadHere}
                  highlighted={highlight === item.id}
                  canInteract={!suspended}
                  showThread
                  onLongPress={actions.open}
                  onReact={(msg, emoji) => react.mutate({ message: msg, emoji }, { onError: (err) => toast.error(errorMessage(err)) })}
                  onOpenThread={openThread}
                  onRetry={sender.retry}
                  onDiscard={sender.discard}
                  onOpenImage={(msg, attachmentId) => {
                    const photos = msg.attachments.filter((a) => a.kind === "image").map((a) => ({ id: a.id, url: a.url }));
                    setLightbox({ photos, index: Math.max(0, photos.findIndex((p) => p.id === attachmentId)) });
                  }}
                />
              );
            }}
            contentContainerStyle={{ paddingHorizontal: layout.gutter, paddingVertical: s[4], flexGrow: 1 }}
            keyboardDismissMode="interactive"
            keyboardShouldPersistTaps="handled"
            onEndReached={() => thread.hasNextPage && !thread.isFetchingNextPage && void thread.fetchNextPage()}
            onEndReachedThreshold={0.5}
            onStartReached={() => thread.hasPreviousPage && !thread.isFetchingPreviousPage && void thread.fetchPreviousPage()}
            onStartReachedThreshold={0.5}
            maintainVisibleContentPosition={{ minIndexForVisible: 0 }}
            onScroll={(e) => setNearNewest(e.nativeEvent.contentOffset.y < 80)}
            scrollEventThrottle={100}
            onScrollToIndexFailed={(info) => {
              listRef.current?.scrollToOffset({ offset: info.averageItemLength * info.index, animated: false });
              setTimeout(() => listRef.current?.scrollToIndex({ index: info.index, viewPosition: 0.5, animated: true }), 100);
            }}
            ListFooterComponent={thread.isFetchingNextPage ? <Spinner label="Loading older messages" /> : null}
            ListEmptyComponent={
              <View style={{ flex: 1, justifyContent: "center", transform: [{ scaleY: -1 }] }}>
                <EmptyState icon={MessagesSquare} title={isChannelsDirect(c) ? "No messages yet" : `This is the start of ${channelsDisplayName(c)}`} compact />
              </View>
            }
          />
          {(!nearNewest || thread.hasPreviousPage) && data.length > 0 ? (
            <View pointerEvents="box-none" style={{ position: "absolute", left: 0, right: 0, bottom: s[4], alignItems: "center" }}>
              <Button size="md" variant="secondary" leftIcon={ArrowDown} onPress={goLatest}>
                Latest messages
              </Button>
            </View>
          ) : null}
        </View>
      )}
      {actions.element}
      {lightbox ? <PhotoLightbox photos={lightbox.photos} open onClose={() => setLightbox(null)} initialIndex={lightbox.index} /> : null}
    </Screen>
  );
}

// ───────────────────────────────────────────────────────────── forum ──

function NewPostSheet({ c, open, onClose }: { c: ChannelsDetail; open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const sender = useChannelsSend(c.id);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submit = async () => {
    if (!title.trim()) return setError("Give your post a title.");
    if (!body.trim()) return setError(null);
    setError(null);
    setBusy(true);
    const msg = await sender.send({ body, title, tags });
    setBusy(false);
    if (!msg) {
      toast.error("Couldn't post. Try again.");
      return;
    }
    haptic.success();
    setTitle("");
    setBody("");
    setTags([]);
    onClose();
    router.push(`/channels/thread/${msg.id}`);
  };
  return (
    <Sheet
      open={open}
      onClose={() => !busy && onClose()}
      title="New post"
      snapPoints={["85%"]}
      footer={
        <Button fullWidth loading={busy} disabled={!title.trim() || !body.trim()} onPress={() => void submit()}>
          Post
        </Button>
      }
    >
      <View style={{ gap: theme.space[4] }}>
        <Field label="Title" error={error}>
          <Input value={title} onChangeText={setTitle} maxLength={200} autoFocus returnKeyType="next" />
        </Field>
        <Field label="Post">
          <TextArea value={body} onChangeText={setBody} maxLength={8000} />
        </Field>
        {c.forum_tags.length > 0 ? (
          <Field label="Tags">
            <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[2] }}>
              {c.forum_tags.map((t) => (
                <ToggleChip key={t} pressed={tags.includes(t)} onPressedChange={(on) => setTags((list) => (on ? [...list, t] : list.filter((x) => x !== t)))}>
                  {t}
                </ToggleChip>
              ))}
            </View>
          </Field>
        ) : null}
      </View>
    </Sheet>
  );
}

function TopicRow({ m, onPress }: { m: ChannelsMessage; onPress: () => void }) {
  const theme = useTheme();
  const col = theme.colors;
  const s = theme.space;
  const activity = m.last_reply_at ?? m.created_at;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${m.title ?? "Post"}. ${channelsReplyCount(m.reply_count)}`}
      onPress={onPress}
      style={({ pressed }) => ({ gap: s[2], paddingVertical: s[3], borderBottomWidth: 1, borderBottomColor: col.border, backgroundColor: pressed ? col.sunken : "transparent" })}
    >
      <View style={{ flexDirection: "row", alignItems: "baseline", gap: s[2] }}>
        <Text role="body" weight={600} numberOfLines={2} style={{ flex: 1, color: m.deleted ? col.textMuted : col.text }}>
          {m.title ?? "Post"}
        </Text>
        <Text role="caption" tone="muted">
          {channelsShortTime(activity)}
        </Text>
      </View>
      {!m.deleted && m.body ? <ChannelsMarkdown body={m.body} mentions={m.mentions} numberOfLines={2} tone="muted" /> : null}
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], flexWrap: "wrap" }}>
        {m.tags.map((t) => (
          <Chip key={t} size="sm">
            {t}
          </Chip>
        ))}
        <Text role="caption" tone={m.reply_count ? "primary" : "muted"} weight={600}>
          {m.reply_count ? channelsReplyCount(m.reply_count) : "No replies yet"}
        </Text>
        {m.reply_users.length > 0 ? <AvatarGroup people={m.reply_users.map((u) => ({ name: u.display_name, src: u.avatar_url }))} size="xs" max={3} /> : null}
      </View>
    </Pressable>
  );
}

function Forum({ c }: { c: ChannelsDetail }) {
  const theme = useTheme();
  const layout = useLayout();
  const s = theme.space;
  const [tag, setTag] = useState("");
  const topics = useChannelsTopics(c.id, tag);
  const [writing, setWriting] = useState(false);
  const footer = c.permissions.post ? (
    <Button fullWidth leftIcon={Plus} onPress={() => setWriting(true)}>
      New post
    </Button>
  ) : (
    <ComposerNotice c={c} suspended={false} />
  );
  return (
    <Screen scroll={false} padded={false} edges={["bottom"]} gap={0} footer={footer}>
      {c.forum_tags.length > 0 ? (
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2], paddingHorizontal: layout.gutter, paddingVertical: s[3] }} accessibilityLabel="Filter by tag">
          <Chip size="sm" selected={tag === ""} onPress={() => setTag("")}>
            All
          </Chip>
          {c.forum_tags.map((t) => (
            <Chip key={t} size="sm" selected={tag === t} onPress={() => setTag(tag === t ? "" : t)}>
              {t}
            </Chip>
          ))}
        </View>
      ) : null}
      {topics.isPending ? (
        <View style={{ gap: s[2], paddingHorizontal: layout.gutter }}>
          {Array.from({ length: 5 }, (_, i) => (
            <SkeletonRow key={i} />
          ))}
        </View>
      ) : topics.isError ? (
        <ErrorState title="Couldn't load posts" info={topics.error.message} onRetry={() => void topics.refetch()} retrying={topics.isFetching} />
      ) : (
        <FlatList
          data={topics.data.items}
          keyExtractor={(m) => m.id}
          renderItem={({ item }) => <TopicRow m={item} onPress={() => router.push(`/channels/thread/${item.id}`)} />}
          contentContainerStyle={{ paddingHorizontal: layout.gutter, paddingBottom: s[8], flexGrow: 1 }}
          onEndReached={() => topics.hasNextPage && !topics.isFetchingNextPage && void topics.fetchNextPage()}
          onEndReachedThreshold={0.5}
          refreshControl={<RefreshControl refreshing={topics.isRefetching && !topics.isFetchingNextPage} onRefresh={() => void topics.refetch()} tintColor={theme.colors.textMuted} />}
          ListFooterComponent={topics.isFetchingNextPage ? <Spinner label="Loading more" /> : null}
          ListEmptyComponent={
            <EmptyState
              icon={LayoutList}
              title={tag ? `No posts tagged ${tag}` : "No posts yet"}
              action={
                c.permissions.post && !tag ? (
                  <Button variant="secondary" onPress={() => setWriting(true)}>
                    Write the first post
                  </Button>
                ) : undefined
              }
            />
          }
        />
      )}
      <NewPostSheet c={c} open={writing} onClose={() => setWriting(false)} />
    </Screen>
  );
}
