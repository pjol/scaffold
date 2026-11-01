import { chatInboxTime, messagePreview, useChatInbox, useMe, type ChatConversation } from "@scaffoldapp/shared";
import { Badge, Button, EmptyState, ErrorState, IconButton, SegmentedControl, SkeletonRow, Spinner, Text, Title, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { router, useScrollToTop } from "expo-router";
import { BellOff, MessagesSquare, Search, SquarePen } from "lucide-react-native";
import { useRef, useState } from "react";
import { FlatList, Pressable, RefreshControl, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ConversationAvatar } from "./ConversationAvatar";

type View_ = "inbox" | "archived";

function Row({ c, meId }: { c: ChatConversation; meId: string | undefined }) {
  const theme = useTheme();
  const col = theme.colors;
  const s = theme.space;
  const unread = c.unread > 0;
  const preview = messagePreview(c.last_message, meId, c.kind === "group");
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${c.title}${unread ? `, ${c.unread} unread` : ""}. ${preview}`}
      onPress={() => router.push(`/chat/${c.id}`)}
      style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[3], paddingVertical: s[3], minHeight: theme.tap.xl, backgroundColor: pressed ? col.sunken : "transparent" })}
    >
      <ConversationAvatar conversation={c} />
      <View style={{ flex: 1, minWidth: 0, gap: s[0.5] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text role="body" weight={unread ? 600 : 400} numberOfLines={1} style={{ flex: 1 }}>
            {c.title}
          </Text>
          <Text role="caption" tone={unread ? "primary" : "muted"}>
            {chatInboxTime(c.last_message_at)}
          </Text>
        </View>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text role="label" tone={unread ? "default" : "muted"} numberOfLines={1} style={{ flex: 1, fontStyle: c.last_message?.deleted ? "italic" : "normal" }}>
            {preview}
          </Text>
          {c.muted ? <BellOff color={col.textSubtle} size={16} /> : null}
          {unread ? (
            <Badge tone="primary" variant="solid" size="sm">
              {c.unread > 99 ? "99+" : String(c.unread)}
            </Badge>
          ) : null}
        </View>
      </View>
    </Pressable>
  );
}

/** Chat tab: inbox or archive, newest activity first; search and new conversation in the header. */
export function InboxScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const insets = useSafeAreaInsets();
  const s = theme.space;
  const [view, setView] = useState<View_>("inbox");
  const inbox = useChatInbox(view === "archived");
  const me = useMe();
  const listRef = useRef<FlatList<ChatConversation>>(null);
  useScrollToTop(listRef);
  const pad = { paddingLeft: insets.left + layout.gutter, paddingRight: insets.right + layout.gutter };

  const header = (
    <View style={{ gap: s[4], paddingTop: insets.top + s[4], paddingBottom: s[3], ...pad }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <Title style={{ flex: 1 }}>Chat</Title>
        <IconButton label="Search messages" icon={Search} variant="ghost" onPress={() => router.push("/chat/search")} />
        <IconButton label="New message" icon={SquarePen} variant="soft" onPress={() => router.push("/chat/new")} />
      </View>
      <SegmentedControl<View_>
        accessibilityLabel="Show"
        value={view}
        onValueChange={setView}
        options={[
          { value: "inbox", label: "Inbox" },
          { value: "archived", label: "Archived" },
        ]}
      />
    </View>
  );

  let body;
  if (inbox.isPending) {
    body = (
      <View style={{ gap: s[2], ...pad }}>
        {Array.from({ length: 6 }, (_, i) => (
          <SkeletonRow key={i} />
        ))}
      </View>
    );
  } else if (inbox.isError) {
    body = <ErrorState title="Couldn't load conversations" info={inbox.error.message} onRetry={() => void inbox.refetch()} retrying={inbox.isFetching} />;
  } else {
    body = (
      <FlatList
        ref={listRef}
        data={inbox.data.items}
        keyExtractor={(c) => c.id}
        renderItem={({ item }) => <Row c={item} meId={me.data?.id} />}
        contentContainerStyle={{ ...pad, paddingBottom: s[8], flexGrow: 1 }}
        onEndReached={() => inbox.hasNextPage && !inbox.isFetchingNextPage && void inbox.fetchNextPage()}
        onEndReachedThreshold={0.5}
        refreshControl={<RefreshControl refreshing={inbox.isRefetching && !inbox.isFetchingNextPage} onRefresh={() => void inbox.refetch()} tintColor={theme.colors.textMuted} />}
        ListFooterComponent={inbox.isFetchingNextPage ? <Spinner label="Loading more" /> : null}
        ListEmptyComponent={
          view === "archived" ? (
            <EmptyState icon={MessagesSquare} title="No archived conversations" />
          ) : (
            <EmptyState
              icon={MessagesSquare}
              title="No messages yet"
              action={
                <Button variant="secondary" onPress={() => router.push("/chat/new")}>
                  Start a conversation
                </Button>
              }
            />
          )
        }
      />
    );
  }

  return (
    <View style={{ flex: 1, backgroundColor: theme.colors.canvas }}>
      {header}
      <View style={{ flex: 1 }}>{body}</View>
    </View>
  );
}
