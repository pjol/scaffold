import { errorMessage, useFeedPinned, useFeedPosts, useFeedTopic, useMarkFeedSeen, useMe, useRealtimeEvent, type FeedPost } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, IconButton, Skeleton, Spinner, Title, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { router, useFocusEffect, useScrollToTop } from "expo-router";
import { ArrowUp, Newspaper, SquarePen } from "lucide-react-native";
import { useCallback, useRef, useState } from "react";
import { FlatList, RefreshControl, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { PostCard } from "./PostCard";

function CardSkeleton() {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View style={{ gap: s[3], padding: s[4], borderRadius: theme.radius.lg, borderWidth: 1, borderColor: theme.colors.border }} accessibilityElementsHidden>
      <View style={{ flexDirection: "row", gap: s[3], alignItems: "center" }}>
        <Skeleton shape="circle" width={40} height={40} />
        <View style={{ flex: 1, gap: s[2] }}>
          <Skeleton width="50%" height={14} />
          <Skeleton width="30%" height={12} />
        </View>
      </View>
      <Skeleton height={14} />
      <Skeleton width="70%" height={14} />
    </View>
  );
}

/** Feed tab: pinned announcements, then posts newest first; pull to refresh, "Show new posts", opening it clears the unread dot. */
export function FeedScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const s = theme.space;
  const insets = useSafeAreaInsets();
  useFeedTopic();
  const me = useMe();
  const pinned = useFeedPinned();
  const posts = useFeedPosts();
  const markSeen = useMarkFeedSeen();
  const [incoming, setIncoming] = useState(0);
  const list = useRef<FlatList<FeedPost>>(null);
  useScrollToTop(list);

  useRealtimeEvent("feed.post", (e) => {
    if (e.data.author_id !== me.data?.id && e.data.kind === "post") setIncoming((n) => n + 1);
  });

  const { mutate: seen } = markSeen;
  useFocusEffect(
    useCallback(() => {
      seen();
    }, [seen]),
  );

  const showNew = async () => {
    setIncoming(0);
    list.current?.scrollToOffset({ offset: 0, animated: true });
    await posts.refetch();
    seen();
  };

  const items = posts.data?.items ?? [];
  const announcements = pinned.data?.items ?? [];

  const header = (
    <View style={{ gap: s[4], paddingBottom: s[4] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[3] }}>
        <Title style={{ flex: 1 }}>Feed</Title>
        <IconButton label="New post" icon={SquarePen} variant="soft" onPress={() => router.push("/feed/compose")} />
      </View>
      {incoming > 0 ? (
        <Button size="md" leftIcon={ArrowUp} onPress={() => void showNew()} style={{ alignSelf: "center" }}>
          {incoming === 1 ? "Show 1 new post" : `Show ${incoming} new posts`}
        </Button>
      ) : null}
      {announcements.map((p) => (
        <PostCard key={p.id} post={p} />
      ))}
    </View>
  );

  return (
    <FlatList
      ref={list}
      data={posts.isPending || posts.isError ? [] : items}
      keyExtractor={(p) => p.id}
      renderItem={({ item }) => <PostCard post={item} />}
      ItemSeparatorComponent={() => <View style={{ height: s[4] }} />}
      ListHeaderComponent={header}
      ListEmptyComponent={
        posts.isPending ? (
          <View style={{ gap: s[4] }}>
            <CardSkeleton />
            <CardSkeleton />
          </View>
        ) : posts.isError ? (
          <ErrorState title="Couldn't load the feed" info={errorMessage(posts.error)} onRetry={() => void posts.refetch()} retrying={posts.isRefetching} />
        ) : (
          <EmptyState
            icon={Newspaper}
            title="No posts yet"
            action={
              <Button variant="secondary" onPress={() => router.push("/feed/compose")}>
                Write the first post
              </Button>
            }
          />
        )
      }
      ListFooterComponent={posts.isFetchingNextPage ? <View style={{ paddingTop: s[4], alignItems: "center" }}><Spinner size={20} /></View> : null}
      onEndReached={() => {
        if (posts.hasNextPage && !posts.isFetchingNextPage) void posts.fetchNextPage();
      }}
      onEndReachedThreshold={1.2}
      refreshControl={
        <RefreshControl
          refreshing={posts.isRefetching && !posts.isFetchingNextPage}
          onRefresh={() => {
            setIncoming(0);
            void posts.refetch();
            void pinned.refetch();
            seen();
          }}
          tintColor={theme.colors.textMuted}
        />
      }
      style={{ flex: 1, backgroundColor: theme.colors.canvas }}
      contentContainerStyle={{ paddingTop: insets.top + s[4], paddingBottom: s[8], paddingHorizontal: layout.gutter, width: "100%", maxWidth: layout.maxContent, alignSelf: "center" }}
      keyboardShouldPersistTaps="handled"
    />
  );
}
