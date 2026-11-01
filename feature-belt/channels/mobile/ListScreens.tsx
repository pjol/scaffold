import {
  channelsAuthorName,
  channelsDisplayName,
  channelsPlainText,
  channelsReplyCount,
  channelsShortTime,
  errorMessage,
  orgsNounA,
  useActiveOrg,
  useChannelsBrowse,
  useChannelsLater,
  useChannelsSaveLater,
  useChannelsSearch,
  useChannelsThreads,
  useDebouncedValue,
  useJoinChannel,
  type ChannelsBrowseChannel,
  type ChannelsMessage,
  type ChannelsRef,
} from "@scaffoldapp/shared";
import { Avatar, AvatarGroup, Badge, Button, EmptyState, ErrorState, IconButton, InfoTip, Input, SegmentedControl, SkeletonRow, Spinner, Text, toast, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, type Href } from "expo-router";
import { Bookmark, Building2, Check, Compass, Hash, LayoutList, Lock, MessageSquareText, Search, SearchX, Trash2 } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { FlatList, Pressable, RefreshControl, View } from "react-native";

/** Where a message opens: its thread for a reply, else its channel at that message. */
function messageHref(m: Pick<ChannelsMessage, "id" | "channel_id" | "thread_root_id">): Href {
  return (m.thread_root_id ? `/channels/thread/${m.thread_root_id}?message=${m.id}` : `/channels/${m.channel_id}?message=${m.id}`) as Href;
}

function NoOrg({ title }: { title: string }) {
  return (
    <View style={{ flex: 1, justifyContent: "center" }}>
      <Stack.Screen options={{ title }} />
      <EmptyState icon={Building2} title={`Join ${orgsNounA()} first`} />
    </View>
  );
}

function ListSkeleton() {
  const layout = useLayout();
  const s = useTheme().space;
  return (
    <View style={{ gap: s[2], paddingHorizontal: layout.gutter, paddingTop: s[4] }}>
      {Array.from({ length: 6 }, (_, i) => (
        <SkeletonRow key={i} />
      ))}
    </View>
  );
}

/** A message in a list: who, where, when, and one or two lines of it. */
function MessageListRow({ m, channel, trailing, onPress }: { m: ChannelsMessage; channel: ChannelsRef; trailing?: ReactNode; onPress: () => void }) {
  const theme = useTheme();
  const col = theme.colors;
  const s = theme.space;
  const text = m.title ?? channelsPlainText(m.body, m.mentions);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${channelsAuthorName(m)} in ${channelsDisplayName(channel)}: ${text}`}
      onPress={onPress}
      style={({ pressed }) => ({ flexDirection: "row", gap: s[3], paddingVertical: s[3], borderBottomWidth: 1, borderBottomColor: col.border, backgroundColor: pressed ? col.sunken : "transparent" })}
    >
      <Avatar name={channelsAuthorName(m)} src={m.sender?.avatar_url ?? null} size="sm" />
      <View style={{ flex: 1, minWidth: 0, gap: s[1] }}>
        <View style={{ flexDirection: "row", alignItems: "baseline", gap: s[2] }}>
          <Text role="label" weight={600} numberOfLines={1} style={{ flexShrink: 1 }}>
            {channelsAuthorName(m)}
          </Text>
          <Text role="caption" tone="muted" numberOfLines={1} style={{ flexShrink: 1 }}>
            {channelsDisplayName(channel)}
          </Text>
          <Text role="caption" tone="muted" style={{ marginLeft: "auto" }}>
            {channelsShortTime(m.created_at)}
          </Text>
        </View>
        <Text role="body" numberOfLines={2}>
          {text || (m.attachments.length ? "Shared a file" : "")}
        </Text>
      </View>
      {trailing}
    </Pressable>
  );
}

// ───────────────────────────────────────────────────────────── search ──

/** channels/search: messages across the workspace, with in:#channel and from:@person filters. */
export function SearchScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const s = theme.space;
  const { org } = useActiveOrg();
  const [query, setQuery] = useState("");
  const q = useDebouncedValue(query.trim(), 300);
  const results = useChannelsSearch(org?.id, q);
  if (!org) return <NoOrg title="Search" />;
  let body;
  if (q.length < 2) body = <EmptyState icon={Search} title="Search messages" info="Narrow it down with in:#channel, from:@name, from:me, has:file or is:thread." />;
  else if (results.isPending) body = <ListSkeleton />;
  else if (results.isError) body = <ErrorState title="Couldn't search" info={results.error.message} onRetry={() => void results.refetch()} retrying={results.isFetching} />;
  else
    body = (
      <FlatList
        data={results.data.items}
        keyExtractor={(h) => h.message.id}
        renderItem={({ item }) => <MessageListRow m={item.message} channel={item.channel} onPress={() => router.push(messageHref(item.message))} />}
        contentContainerStyle={{ paddingHorizontal: layout.gutter, paddingBottom: s[8], flexGrow: 1 }}
        keyboardShouldPersistTaps="handled"
        onEndReached={() => results.hasNextPage && !results.isFetchingNextPage && void results.fetchNextPage()}
        onEndReachedThreshold={0.5}
        ListFooterComponent={results.isFetchingNextPage ? <Spinner label="Loading more" /> : null}
        ListEmptyComponent={<EmptyState icon={SearchX} title="No matching messages" />}
      />
    );
  return (
    <View style={{ flex: 1, backgroundColor: theme.colors.canvas }}>
      <Stack.Screen options={{ title: "Search" }} />
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], paddingHorizontal: layout.gutter, paddingVertical: s[3] }}>
        <Input value={query} onChangeText={setQuery} placeholder={`Search ${org.name}`} leadingIcon={Search} autoFocus autoCapitalize="none" autoCorrect={false} returnKeyType="search" containerStyle={{ flex: 1 }} accessibilityLabel="Search messages" />
        <InfoTip content="Filters: in:#channel, from:@name, from:me, has:file, is:thread. Combine them with words." label="Search filters" />
      </View>
      <View style={{ flex: 1 }}>{body}</View>
    </View>
  );
}

// ───────────────────────────────────────────────────────────── browse ──

function BrowseRow({ c }: { c: ChannelsBrowseChannel }) {
  const theme = useTheme();
  const col = theme.colors;
  const s = theme.space;
  const join = useJoinChannel(c.id);
  const Icon = c.layout === "forum" ? LayoutList : c.kind === "private" ? Lock : Hash;
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.lg, borderBottomWidth: 1, borderBottomColor: col.border, paddingVertical: s[2] }}>
      <Icon color={col.textMuted} size={20} />
      <View style={{ flex: 1, minWidth: 0, flexDirection: "row", alignItems: "baseline", gap: s[2], flexWrap: "wrap" }}>
        <Text role="body" weight={600} numberOfLines={1}>
          {c.name}
        </Text>
        <Text role="caption" tone="muted">
          {c.member_count === 1 ? "1 member" : `${c.member_count} members`}
        </Text>
      </View>
      {c.topic ? <InfoTip content={c.topic} label={`About ${c.name}`} /> : null}
      {c.joined || c.archived ? (
        <Button size="md" variant="secondary" onPress={() => router.push(`/channels/${c.id}`)}>
          Open
        </Button>
      ) : (
        <Button
          size="md"
          loading={join.isPending}
          onPress={() => join.mutate(undefined, { onSuccess: () => router.push(`/channels/${c.id}`), onError: (e) => toast.error(errorMessage(e)) })}
        >
          Join
        </Button>
      )}
    </View>
  );
}

/** channels/browse: public channels (and private ones you're in), by name. */
export function BrowseScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const s = theme.space;
  const { org } = useActiveOrg();
  const [query, setQuery] = useState("");
  const [archived, setArchived] = useState(false);
  const q = useDebouncedValue(query.trim(), 250);
  const list = useChannelsBrowse(org?.id, q, archived);
  if (!org) return <NoOrg title="Browse channels" />;
  return (
    <View style={{ flex: 1, backgroundColor: theme.colors.canvas }}>
      <Stack.Screen options={{ title: "Browse channels" }} />
      <View style={{ gap: s[3], paddingHorizontal: layout.gutter, paddingVertical: s[3] }}>
        <Input value={query} onChangeText={setQuery} placeholder="Find a channel" leadingIcon={Search} autoCapitalize="none" autoCorrect={false} accessibilityLabel="Find a channel" />
        <SegmentedControl<"active" | "archived">
          accessibilityLabel="Show"
          value={archived ? "archived" : "active"}
          onValueChange={(v) => setArchived(v === "archived")}
          options={[
            { value: "active", label: "Channels" },
            { value: "archived", label: "Archived" },
          ]}
        />
      </View>
      {list.isPending ? (
        <ListSkeleton />
      ) : list.isError ? (
        <ErrorState title="Couldn't load channels" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
      ) : (
        <FlatList
          data={list.data.items}
          keyExtractor={(c) => c.id}
          renderItem={({ item }) => <BrowseRow c={item} />}
          contentContainerStyle={{ paddingHorizontal: layout.gutter, paddingBottom: s[8], flexGrow: 1 }}
          keyboardShouldPersistTaps="handled"
          onEndReached={() => list.hasNextPage && !list.isFetchingNextPage && void list.fetchNextPage()}
          onEndReachedThreshold={0.5}
          ListFooterComponent={list.isFetchingNextPage ? <Spinner label="Loading more" /> : null}
          ListEmptyComponent={<EmptyState icon={Compass} title={q ? "No channels match" : archived ? "No archived channels" : "No channels yet"} />}
        />
      )}
    </View>
  );
}

// ───────────────────────────────────────────────────────────── threads ──

/** channels/threads: threads you follow, latest reply first, with unread counts. */
export function ThreadsScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const col = theme.colors;
  const s = theme.space;
  const { org } = useActiveOrg();
  const threads = useChannelsThreads(org?.id);
  if (!org) return <NoOrg title="Threads" />;
  return (
    <View style={{ flex: 1, backgroundColor: col.canvas }}>
      <Stack.Screen options={{ title: "Threads" }} />
      {threads.isPending ? (
        <ListSkeleton />
      ) : threads.isError ? (
        <ErrorState title="Couldn't load threads" info={threads.error.message} onRetry={() => void threads.refetch()} retrying={threads.isFetching} />
      ) : (
        <FlatList
          data={threads.data.items}
          keyExtractor={(t) => t.root.id}
          renderItem={({ item: t }) => {
            const last = t.latest_replies[t.latest_replies.length - 1];
            return (
              <Pressable
                accessibilityRole="button"
                accessibilityLabel={`Thread in ${channelsDisplayName(t.channel)}${t.unread ? `, ${t.unread} unread` : ""}`}
                onPress={() => router.push(`/channels/thread/${t.root.id}`)}
                style={({ pressed }) => ({ gap: s[2], paddingVertical: s[3], borderBottomWidth: 1, borderBottomColor: col.border, backgroundColor: pressed ? col.sunken : "transparent" })}
              >
                <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
                  <Text role="caption" weight={600} tone="muted" numberOfLines={1} style={{ flex: 1 }}>
                    {channelsDisplayName(t.channel)}
                  </Text>
                  {t.unread > 0 ? (
                    <Badge tone="primary" variant="solid" size="sm">
                      {t.unread > 99 ? "99+" : String(t.unread)}
                    </Badge>
                  ) : null}
                </View>
                <Text role="body" weight={t.unread ? 700 : 400} numberOfLines={2}>
                  {t.root.title ?? `${channelsAuthorName(t.root)}: ${channelsPlainText(t.root.body, t.root.mentions)}`}
                </Text>
                {last ? (
                  <Text role="label" tone="muted" numberOfLines={1}>
                    {`${channelsAuthorName(last)}: ${channelsPlainText(last.body, last.mentions) || "Shared a file"}`}
                  </Text>
                ) : null}
                <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
                  {t.root.reply_users.length > 0 ? <AvatarGroup people={t.root.reply_users.map((u) => ({ name: u.display_name, src: u.avatar_url }))} size="xs" max={4} /> : null}
                  <Text role="caption" tone="muted">
                    {channelsReplyCount(t.root.reply_count)}
                    {t.root.last_reply_at ? ` · ${channelsShortTime(t.root.last_reply_at)}` : ""}
                  </Text>
                </View>
              </Pressable>
            );
          }}
          contentContainerStyle={{ paddingHorizontal: layout.gutter, paddingBottom: s[8], flexGrow: 1 }}
          onEndReached={() => threads.hasNextPage && !threads.isFetchingNextPage && void threads.fetchNextPage()}
          onEndReachedThreshold={0.5}
          refreshControl={<RefreshControl refreshing={threads.isRefetching && !threads.isFetchingNextPage} onRefresh={() => void threads.refetch()} tintColor={col.textMuted} />}
          ListFooterComponent={threads.isFetchingNextPage ? <Spinner label="Loading more" /> : null}
          ListEmptyComponent={<EmptyState icon={MessageSquareText} title="No threads yet" info="Threads you reply in, start or are mentioned in show here." />}
        />
      )}
    </View>
  );
}

// ───────────────────────────────────────────────────────────── later ──

/** channels/later: messages you saved, open or done. */
export function LaterScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const col = theme.colors;
  const s = theme.space;
  const { org } = useActiveOrg();
  const [state, setState] = useState<"open" | "done">("open");
  const later = useChannelsLater(org?.id, state);
  const save = useChannelsSaveLater(org?.id);
  if (!org) return <NoOrg title="Later" />;
  return (
    <View style={{ flex: 1, backgroundColor: col.canvas }}>
      <Stack.Screen options={{ title: "Later" }} />
      <View style={{ paddingHorizontal: layout.gutter, paddingVertical: s[3] }}>
        <SegmentedControl<"open" | "done">
          accessibilityLabel="Show"
          value={state}
          onValueChange={setState}
          options={[
            { value: "open", label: "Saved" },
            { value: "done", label: "Done" },
          ]}
        />
      </View>
      {later.isPending ? (
        <ListSkeleton />
      ) : later.isError ? (
        <ErrorState title="Couldn't load saved items" info={later.error.message} onRetry={() => void later.refetch()} retrying={later.isFetching} />
      ) : (
        <FlatList
          data={later.data.items}
          keyExtractor={(i) => i.message.id}
          renderItem={({ item }) => (
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
              <View style={{ flex: 1, minWidth: 0 }}>
                <MessageListRow m={item.message} channel={item.channel} onPress={() => router.push(messageHref(item.message))} />
              </View>
              {state === "open" ? (
                <IconButton
                  label="Mark done"
                  icon={Check}
                  variant="ghost"
                  onPress={() => save.mutate({ message: item.message, saved: true, done: true }, { onSuccess: () => toast.success("Marked done."), onError: (e) => toast.error(errorMessage(e)) })}
                />
              ) : null}
              <IconButton
                label="Remove from Later"
                icon={Trash2}
                variant="ghost"
                onPress={() => save.mutate({ message: item.message, saved: false }, { onError: (e) => toast.error(errorMessage(e)) })}
              />
            </View>
          )}
          contentContainerStyle={{ paddingHorizontal: layout.gutter, paddingBottom: s[8], flexGrow: 1 }}
          onEndReached={() => later.hasNextPage && !later.isFetchingNextPage && void later.fetchNextPage()}
          onEndReachedThreshold={0.5}
          refreshControl={<RefreshControl refreshing={later.isRefetching && !later.isFetchingNextPage} onRefresh={() => void later.refetch()} tintColor={col.textMuted} />}
          ListFooterComponent={later.isFetchingNextPage ? <Spinner label="Loading more" /> : null}
          ListEmptyComponent={<EmptyState icon={Bookmark} title={state === "open" ? "Nothing saved" : "Nothing done yet"} info="Long-press a message and choose Save for later." />}
        />
      )}
    </View>
  );
}
