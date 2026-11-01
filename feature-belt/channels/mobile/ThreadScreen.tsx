import {
  channelsDayLabel,
  channelsDisplayName,
  channelsGroupsWith,
  channelsReplyCount,
  channelsSameDay,
  channelsTypingText,
  errorMessage,
  flattenChannelsPages,
  useChannel,
  useChannelsActiveThread,
  useChannelsFollow,
  useChannelsMarkThreadRead,
  useChannelsReact,
  useChannelsSend,
  useChannelsThread,
  useChannelsTypingUsers,
  useMe,
  useTopic,
  topic,
  type ChannelsCachedMessage,
  type ChannelsDetail,
} from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, PhotoLightbox, Screen, Skeleton, Spinner, Text, toast, useLayout, useTheme, type GalleryPhoto } from "@scaffoldapp/ui-native";
import { Stack, router, useFocusEffect, useLocalSearchParams } from "expo-router";
import { Bell, BellOff, Hash, MessageSquareText } from "lucide-react-native";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AppState, FlatList, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { Composer } from "./Composer";
import { useMessageActions } from "./MessageActions";
import { MessageRow } from "./MessageRow";

/** channels/thread/<root id>: the first message, its replies and a reply composer. */
export function ThreadScreen() {
  const params = useLocalSearchParams<{ id: string; message?: string }>();
  const rootId = params.id ?? "";
  const around = params.message || null;
  const theme = useTheme();
  const me = useMe();
  const thread = useChannelsThread(rootId, around);
  const first = thread.data?.pages[0];
  const channel = useChannel(first?.channel.id);
  // Subscribe for live replies without marking the channel itself as read on screen.
  useTopic(first ? topic("channels", first.channel.id) : null);
  useChannelsActiveThread(rootId);

  if (thread.isPending || (first && channel.isPending)) {
    return (
      <Screen scroll={false} edges={["bottom"]} gap={theme.space[4]}>
        <Stack.Screen options={{ title: "Thread" }} />
        <Skeleton width="80%" height={56} />
        <Skeleton width="65%" height={40} />
        <Skeleton width="50%" height={40} />
      </Screen>
    );
  }
  if (thread.isError || channel.isError || !first || !channel.data) {
    const notFound = thread.error?.status === 404 || channel.error?.status === 404;
    return (
      <Screen centered edges={["bottom"]}>
        <Stack.Screen options={{ title: "Thread" }} />
        {notFound ? (
          <EmptyState
            icon={MessageSquareText}
            title="Thread not found"
            action={
              <Button variant="secondary" onPress={() => router.back()}>
                Go back
              </Button>
            }
          />
        ) : (
          <ErrorState
            title="Couldn't load this thread"
            info={(thread.error ?? channel.error)?.message}
            onRetry={() => {
              void thread.refetch();
              void channel.refetch();
            }}
            retrying={thread.isFetching || channel.isFetching}
          />
        )}
      </Screen>
    );
  }
  return <Thread key={rootId} rootId={rootId} c={channel.data} around={around} meId={me.data?.id} suspended={me.data?.status === "suspended"} />;
}

function Thread({ rootId, c, around, meId, suspended }: { rootId: string; c: ChannelsDetail; around: string | null; meId: string | undefined; suspended: boolean }) {
  const theme = useTheme();
  const layout = useLayout();
  const col = theme.colors;
  const s = theme.space;
  const thread = useChannelsThread(rootId, around);
  const first = thread.data?.pages[0];
  const root = first?.root;
  const following = first?.following ?? false;
  const replies = useMemo(() => flattenChannelsPages(thread.data), [thread.data]);
  const data = useMemo(() => [...replies].reverse(), [replies]);
  const typing = useChannelsTypingUsers(c.id, rootId);
  const markRead = useChannelsMarkThreadRead(rootId, c.org_id);
  const follow = useChannelsFollow(rootId);
  const react = useChannelsReact();
  const sender = useChannelsSend(c.id);
  const [editing, setEditing] = useState<ChannelsCachedMessage | null>(null);
  const [lightbox, setLightbox] = useState<{ photos: GalleryPhoto[]; index: number } | null>(null);
  const [highlight, setHighlight] = useState<string | null>(around);
  const listRef = useRef<FlatList<ChannelsCachedMessage>>(null);
  const actions = useMessageActions({ channel: c, meId, onEdit: setEditing });
  const canPost = c.permissions.post && !suspended;

  const latest = data.find((m) => !m.status);
  const [focused, setFocused] = useState(false);
  useFocusEffect(
    useCallback(() => {
      setFocused(true);
      return () => setFocused(false);
    }, []),
  );
  useEffect(() => {
    if (!focused || !latest || !following) return undefined;
    if (AppState.currentState === "active") markRead(latest);
    const sub = AppState.addEventListener("change", (state) => state === "active" && markRead(latest));
    return () => sub.remove();
  }, [focused, latest, following, markRead]);
  useEffect(() => {
    if (!highlight) return undefined;
    const t = setTimeout(() => setHighlight(null), 2500);
    return () => clearTimeout(t);
  }, [highlight]);

  const onImage = (msg: ChannelsCachedMessage, attachmentId: string) => {
    const photos = msg.attachments.filter((a) => a.kind === "image").map((a) => ({ id: a.id, url: a.url }));
    setLightbox({ photos, index: Math.max(0, photos.findIndex((p) => p.id === attachmentId)) });
  };
  const onReact = (msg: ChannelsCachedMessage, emoji: string) => react.mutate({ message: msg, emoji }, { onError: (err) => toast.error(errorMessage(err)) });
  const toggleFollow = () => follow.mutate(!following, { onSuccess: (r) => toast.success(r.following ? "You'll get replies in Threads." : "Unfollowed."), onError: (e) => toast.error(errorMessage(e)) });

  const footer = (
    <View style={{ gap: s[2] }}>
      <Text role="caption" tone="muted" accessibilityLiveRegion="polite" style={{ opacity: typing.length ? 1 : 0 }}>
        {channelsTypingText(typing) || " "}
      </Text>
      {canPost && root && !root.deleted ? (
        <Composer
          channel={c}
          rootId={rootId}
          allowAlsoSend={c.layout !== "forum"}
          editing={editing}
          onCancelEdit={() => setEditing(null)}
          placeholder="Reply"
          onSent={() => listRef.current?.scrollToOffset({ offset: 0, animated: true })}
        />
      ) : (
        <Text role="label" tone="muted" align="center" style={{ paddingVertical: s[3] }}>
          {suspended ? "Your account is suspended. You can read but not send." : c.archived ? "This channel is archived." : !c.member ? `Join ${channelsDisplayName(c)} to reply.` : "Replies are closed."}
        </Text>
      )}
    </View>
  );

  return (
    <Screen scroll={false} padded={false} edges={["bottom"]} keyboard gap={0} footer={footer}>
      <Stack.Screen
        options={{
          title: root?.title ?? "Thread",
          headerRight: () => (
            <ResourceMenu
              feature="channels"
              kind="channels.message"
              id={rootId}
              title={root?.title ?? "Thread"}
              orgId={c.org_id}
              label="Thread options"
              actions={[
                ...(c.member ? [{ id: "follow", label: following ? "Unfollow thread" : "Follow thread", icon: following ? BellOff : Bell, onSelect: toggleFollow }] : []),
                { id: "channel", label: `Open ${channelsDisplayName(c)}`, icon: Hash, onSelect: () => router.push(`/channels/${c.id}`) },
              ]}
            />
          ),
        }}
      />
      <FlatList
        ref={listRef}
        inverted
        data={data}
        keyExtractor={(item) => (item.client_id && item.sender?.id === meId ? `c-${item.client_id}` : item.id)}
        renderItem={({ item, index }) => {
          const older = data[index + 1];
          const newDay = !older || !channelsSameDay(older.created_at, item.created_at);
          return (
            <MessageRow
              message={item}
              meId={meId}
              grouped={!newDay && channelsGroupsWith(older, item)}
              dayLabel={newDay && older ? channelsDayLabel(item.created_at) : undefined}
              highlighted={highlight === item.id}
              canInteract={!suspended}
              showThread={false}
              onLongPress={actions.open}
              onReact={onReact}
              onOpenThread={() => undefined}
              onRetry={sender.retry}
              onDiscard={sender.discard}
              onOpenImage={onImage}
            />
          );
        }}
        // Inverted: the footer renders at the top, above the oldest reply.
        ListFooterComponent={
          root ? (
            <View style={{ gap: s[2], paddingBottom: s[2] }}>
              {thread.hasNextPage ? (
                <Button variant="ghost" size="md" loading={thread.isFetchingNextPage} onPress={() => void thread.fetchNextPage()}>
                  Show earlier replies
                </Button>
              ) : null}
              <MessageRow
                message={root}
                meId={meId}
                grouped={false}
                highlighted={highlight === root.id}
                canInteract={!suspended}
                showThread={false}
                onLongPress={actions.open}
                onReact={onReact}
                onOpenThread={() => undefined}
                onRetry={() => undefined}
                onDiscard={() => undefined}
                onOpenImage={onImage}
              />
              <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], paddingTop: s[2] }}>
                <Text role="caption" weight={600} tone="muted">
                  {root.reply_count ? channelsReplyCount(root.reply_count) : "No replies yet"}
                </Text>
                <View style={{ flex: 1, height: 1, backgroundColor: col.border }} />
              </View>
            </View>
          ) : null
        }
        ListHeaderComponent={thread.isFetchingPreviousPage ? <Spinner label="Loading newer replies" /> : null}
        onStartReached={() => thread.hasPreviousPage && !thread.isFetchingPreviousPage && void thread.fetchPreviousPage()}
        onStartReachedThreshold={0.5}
        maintainVisibleContentPosition={{ minIndexForVisible: 0 }}
        contentContainerStyle={{ paddingHorizontal: layout.gutter, paddingVertical: s[4], flexGrow: 1 }}
        keyboardDismissMode="interactive"
        keyboardShouldPersistTaps="handled"
      />
      {actions.element}
      {lightbox ? <PhotoLightbox photos={lightbox.photos} open onClose={() => setLightbox(null)} initialIndex={lightbox.index} /> : null}
    </Screen>
  );
}
