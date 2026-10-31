import {
  CHAT_QUICK_REACTIONS,
  chatDayLabel,
  errorMessage,
  flattenThread,
  groupsWith,
  isReadBy,
  refreshChatAfterBlock,
  sameDay,
  useChatConversation,
  useChatConversationTopic,
  useChatMessages,
  useChatTypingUsers,
  useDeleteMessage,
  useIsBlocked,
  useMarkRead,
  useMe,
  useQueryClient,
  useSendMessage,
  useToggleReaction,
  useUnblockUser,
  type ChatConversationDetail,
  type ChatThreadMessage,
  type ChatUser,
} from "@scaffoldapp/shared";
import { Button, ConfirmDialog, EmptyState, ErrorState, IconButton, ListGroup, ListRow, PhotoLightbox, Screen, Sheet, Skeleton, Spinner, Text, toast, useLayout, useTheme, type GalleryPhoto } from "@scaffoldapp/ui-native";
import { Stack, router, useFocusEffect, useLocalSearchParams } from "expo-router";
import { ArrowDown, Flag, Info, MessagesSquare, Pencil, Reply, Trash2 } from "lucide-react-native";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AppState, FlatList, Pressable, View } from "react-native";
import { ReportDialog } from "@/features/moderation";
import { Composer } from "./Composer";
import { ConversationAvatar } from "./ConversationAvatar";
import { MessageBubble } from "./MessageBubble";

const EDIT_WINDOW_MS = 5 * 60_000;

function typingText(users: ChatUser[]): string {
  const [a, b] = users;
  if (!a) return "";
  if (!b) return `${a.display_name} is typing…`;
  if (users.length === 2) return `${a.display_name} and ${b.display_name} are typing…`;
  return `${users.length} people are typing…`;
}

function HeaderTitle({ c }: { c: ChatConversationDetail }) {
  const theme = useTheme();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${c.title}. Details`}
      onPress={() => router.push(`/chat/${c.id}/info`)}
      style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], maxWidth: 240 }}
    >
      <ConversationAvatar conversation={c} size="sm" />
      <Text role="body" weight={600} numberOfLines={1} style={{ flexShrink: 1 }} maxFontSizeMultiplier={1.5}>
        {c.title}
      </Text>
    </Pressable>
  );
}

/** chat/<id>: messages (inverted list, older on scroll, jump to `?message=`), typing, composer above the keyboard. */
export function ThreadScreen() {
  const params = useLocalSearchParams<{ id: string; message?: string }>();
  const id = params.id ?? "";
  const around = params.message || null;
  const theme = useTheme();
  const me = useMe();
  const conversation = useChatConversation(id);
  useChatConversationTopic(id);

  if (conversation.isPending) {
    return (
      <Screen scroll={false} edges={["bottom"]} gap={theme.space[4]} contentStyle={{ justifyContent: "flex-end" }}>
        <Stack.Screen options={{ title: "" }} />
        <Skeleton width="65%" height={40} />
        <Skeleton width="50%" height={40} style={{ alignSelf: "flex-end" }} />
        <Skeleton width="60%" height={64} />
      </Screen>
    );
  }
  if (conversation.isError) {
    return (
      <Screen centered edges={["bottom"]}>
        <Stack.Screen options={{ title: "" }} />
        {conversation.error.status === 404 ? (
          <EmptyState
            icon={MessagesSquare}
            title="Conversation not found"
            action={
              <Button variant="secondary" onPress={() => router.replace("/chat")}>
                Back to chat
              </Button>
            }
          />
        ) : (
          <ErrorState title="Couldn't load this conversation" info={conversation.error.message} onRetry={() => void conversation.refetch()} retrying={conversation.isFetching} />
        )}
      </Screen>
    );
  }
  return <Thread key={id} c={conversation.data} around={around} meId={me.data?.id} suspended={me.data?.status === "suspended"} />;
}

type Pending = { kind: "report" | "delete"; message: ChatThreadMessage } | null;

function Thread({ c, around, meId, suspended }: { c: ChatConversationDetail; around: string | null; meId: string | undefined; suspended: boolean }) {
  const theme = useTheme();
  const layout = useLayout();
  const col = theme.colors;
  const s = theme.space;
  const qc = useQueryClient();
  const thread = useChatMessages(c.id, around);
  const messages = useMemo(() => flattenThread(thread.data), [thread.data]);
  const data = useMemo(() => [...messages].reverse(), [messages]);
  const typing = useChatTypingUsers(c.id);
  const markRead = useMarkRead(c.id);
  const react = useToggleReaction(c.id);
  const remove = useDeleteMessage(c.id);
  const sender = useSendMessage(c.id);
  const unblock = useUnblockUser();
  const blockedByMe = useIsBlocked(c.peer?.id);
  const group = c.kind === "group";
  const [replyTo, setReplyTo] = useState<ChatThreadMessage | null>(null);
  const [editing, setEditing] = useState<ChatThreadMessage | null>(null);
  const [actionsFor, setActionsFor] = useState<ChatThreadMessage | null>(null);
  const [next, setNext] = useState<Pending>(null);
  const pendingAfterSheet = useRef<Pending>(null);
  const [lightbox, setLightbox] = useState<{ photos: GalleryPhoto[]; index: number } | null>(null);
  const [highlight, setHighlight] = useState<string | null>(null);
  const [nearNewest, setNearNewest] = useState(true);
  const listRef = useRef<FlatList<ChatThreadMessage>>(null);

  const latest = data.find((m) => !m.status);
  const lastOwn = data.find((m) => m.sender.id === meId && m.kind === "text" && !m.deleted && !m.status);
  const canInteract = !suspended && !(c.kind === "direct" && (c.blocked || !!c.peer?.deleted));

  // Read state while the screen is focused, the app is active and the newest message is on screen.
  const [focused, setFocused] = useState(false);
  useFocusEffect(
    useCallback(() => {
      setFocused(true);
      return () => setFocused(false);
    }, []),
  );
  useEffect(() => {
    if (!focused || !nearNewest || thread.hasPreviousPage || !latest) return undefined;
    if (AppState.currentState === "active") markRead(latest);
    const sub = AppState.addEventListener("change", (state) => state === "active" && markRead(latest));
    return () => sub.remove();
  }, [focused, nearNewest, thread.hasPreviousPage, latest, markRead]);

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
    if (!around || jumped.current || data.length === 0) return;
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

  const notice = suspended
    ? "Your account is suspended. You can read but not send."
    : c.kind === "direct" && c.peer?.deleted
      ? "This account was deleted."
      : c.kind === "direct" && c.blocked
        ? blockedByMe
          ? `You blocked ${c.peer?.display_name ?? "this person"}.`
          : "You can't reply to this conversation."
        : null;

  const footer = (
    <View style={{ gap: s[2] }}>
      <Text role="caption" tone="muted" accessibilityLiveRegion="polite" style={{ opacity: typing.length ? 1 : 0 }}>
        {typingText(typing) || " "}
      </Text>
      {notice ? (
        <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "center", gap: s[3], minHeight: theme.tap.base }}>
          <Text role="label" tone="muted" style={{ flexShrink: 1 }}>
            {notice}
          </Text>
          {blockedByMe && c.peer ? (
            <Button size="md" variant="secondary" loading={unblock.isPending} onPress={() => c.peer && unblock.mutate(c.peer.id, { onSuccess: () => void refreshChatAfterBlock(qc) })}>
              Unblock
            </Button>
          ) : null}
        </View>
      ) : (
        <Composer
          conversationId={c.id}
          replyTo={replyTo}
          onCancelReply={() => setReplyTo(null)}
          editing={editing}
          onCancelEdit={() => setEditing(null)}
          onSent={() => {
            if (around || thread.hasPreviousPage) router.setParams({ message: "" });
            else listRef.current?.scrollToOffset({ offset: 0, animated: true });
          }}
        />
      )}
    </View>
  );

  const m = actionsFor;
  const own = m?.sender.id === meId;
  const canEdit = !!m && own && Date.now() - Date.parse(m.created_at) < EDIT_WINDOW_MS;

  return (
    <Screen scroll={false} padded={false} edges={["bottom"]} keyboard gap={0} footer={footer}>
      <Stack.Screen
        options={{
          headerTitle: () => <HeaderTitle c={c} />,
          headerRight: () => <IconButton label="Details" icon={Info} variant="ghost" onPress={() => router.push(`/chat/${c.id}/info`)} />,
        }}
      />
      {thread.isPending ? (
        <View style={{ flex: 1, justifyContent: "flex-end", gap: s[4], paddingHorizontal: layout.gutter, paddingBottom: s[4] }}>
          <Skeleton width="65%" height={40} />
          <Skeleton width="50%" height={40} style={{ alignSelf: "flex-end" }} />
        </View>
      ) : thread.isError ? (
        <ErrorState title="Couldn't load messages" info={thread.error.message} onRetry={() => void thread.refetch()} retrying={thread.isFetching} />
      ) : (
        <View style={{ flex: 1 }}>
          <FlatList
            ref={listRef}
            inverted
            data={data}
            keyExtractor={(item) => (item.client_id && item.sender.id === meId ? `c-${item.client_id}` : item.id)}
            renderItem={({ item, index }) => {
              const older = data[index + 1];
              const newDay = !older || !sameDay(older.created_at, item.created_at);
              return (
                <MessageBubble
                  message={item}
                  meId={meId}
                  group={group}
                  grouped={!newDay && groupsWith(older, item)}
                  dayLabel={newDay ? chatDayLabel(item.created_at) : undefined}
                  status={item.id === lastOwn?.id ? (isReadBy(c.others_read, item) ? "Read" : "Sent") : undefined}
                  highlighted={highlight === item.id}
                  canInteract={canInteract}
                  onLongPress={setActionsFor}
                  onReact={(msg, emoji) => react.mutate({ message: msg, emoji }, { onError: (err) => toast.error(errorMessage(err)) })}
                  onRetry={sender.retry}
                  onDiscard={sender.discard}
                  onOpenImage={(msg, attachmentId) => {
                    const photos = msg.attachments.filter((a) => a.kind === "image").map((a) => ({ id: a.id, url: a.url }));
                    setLightbox({ photos, index: Math.max(0, photos.findIndex((p) => p.id === attachmentId)) });
                  }}
                  onJumpTo={jumpTo}
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
                <EmptyState icon={MessagesSquare} title="No messages yet" compact />
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

      <Sheet
        open={actionsFor !== null}
        onClose={() => setActionsFor(null)}
        onDismissed={() => {
          if (pendingAfterSheet.current) {
            setNext(pendingAfterSheet.current);
            pendingAfterSheet.current = null;
          }
        }}
      >
        {m ? (
          <View style={{ gap: s[4] }}>
            <View style={{ flexDirection: "row", justifyContent: "space-between" }} accessibilityLabel="Quick reactions">
              {CHAT_QUICK_REACTIONS.map((e) => (
                <Pressable
                  key={e}
                  accessibilityRole="button"
                  accessibilityLabel={`React with ${e}`}
                  onPress={() => {
                    react.mutate({ message: m, emoji: e }, { onError: (err) => toast.error(errorMessage(err)) });
                    setActionsFor(null);
                  }}
                  style={({ pressed }) => ({ width: theme.tap.base, height: theme.tap.base, alignItems: "center", justifyContent: "center", borderRadius: theme.radius.pill, backgroundColor: pressed ? col.sunken : "transparent" })}
                >
                  <Text role="title">{e}</Text>
                </Pressable>
              ))}
            </View>
            <ListGroup>
              <ListRow
                title="Reply"
                leading={<Reply color={col.textMuted} size={20} />}
                onPress={() => {
                  setEditing(null);
                  setReplyTo(m);
                  setActionsFor(null);
                }}
              />
              {canEdit ? (
                <ListRow
                  title="Edit"
                  leading={<Pencil color={col.textMuted} size={20} />}
                  onPress={() => {
                    setReplyTo(null);
                    setEditing(m);
                    setActionsFor(null);
                  }}
                />
              ) : null}
              {!own ? (
                <ListRow
                  title="Report"
                  leading={<Flag color={col.textMuted} size={20} />}
                  onPress={() => {
                    pendingAfterSheet.current = { kind: "report", message: m };
                    setActionsFor(null);
                  }}
                />
              ) : null}
              {own ? (
                <ListRow
                  title="Delete for everyone"
                  destructive
                  leading={<Trash2 color={col.dangerStrong} size={20} />}
                  onPress={() => {
                    pendingAfterSheet.current = { kind: "delete", message: m };
                    setActionsFor(null);
                  }}
                />
              ) : null}
            </ListGroup>
          </View>
        ) : null}
      </Sheet>
      <ConfirmDialog
        open={next?.kind === "delete"}
        onClose={() => setNext(null)}
        title="Delete this message for everyone?"
        confirmLabel="Delete"
        tone="danger"
        onConfirm={() => (next ? remove.mutateAsync(next.message.id) : undefined)}
      />
      {next?.kind === "report" ? (
        <ReportDialog
          open
          onClose={() => setNext(null)}
          targetKind="chat.message"
          targetId={next.message.id}
          noun="message"
          author={next.message.sender.deleted ? null : { id: next.message.sender.id, name: next.message.sender.display_name }}
          onReported={(r) => r.blocked && void refreshChatAfterBlock(qc)}
        />
      ) : null}
      {lightbox ? <PhotoLightbox photos={lightbox.photos} open onClose={() => setLightbox(null)} initialIndex={lightbox.index} /> : null}
    </Screen>
  );
}
