import { errorMessage, useSocialFeed, useSocialSuggestions, type SocialActivityItem } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, IconButton, Spinner, Title, TitleSm, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { router, useScrollToTop } from "expo-router";
import { Rss, UserSearch } from "lucide-react-native";
import { useRef } from "react";
import { FlatList, RefreshControl, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ActivityRow, ActivitySkeleton, PersonRow } from "./parts";

/** Under an empty feed: a few people to start with. */
function StartWith() {
  const theme = useTheme();
  const s = useSocialSuggestions(5);
  if (!s.data || s.data.items.length === 0) return null;
  return (
    <View style={{ gap: theme.space[3], paddingTop: theme.space[8] }}>
      <TitleSm>People to follow</TitleSm>
      <View>
        {s.data.items.map((p) => (
          <PersonRow key={p.user_id} person={p} />
        ))}
      </View>
    </View>
  );
}

/** Following tab: what the people you follow did, newest first; pull to refresh. */
export function FollowingScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const s = theme.space;
  const insets = useSafeAreaInsets();
  const feed = useSocialFeed();
  const list = useRef<FlatList<SocialActivityItem>>(null);
  useScrollToTop(list);
  const items = feed.data?.items ?? [];

  return (
    <FlatList
      ref={list}
      data={feed.isPending || feed.isError ? [] : items}
      keyExtractor={(it) => it.id}
      renderItem={({ item }) => <ActivityRow item={item} />}
      ListHeaderComponent={
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], paddingBottom: s[4] }}>
          <Title style={{ flex: 1 }}>Following</Title>
          <IconButton label="Find people" icon={UserSearch} variant="soft" onPress={() => router.push("/people")} />
        </View>
      }
      ListEmptyComponent={
        feed.isPending ? (
          <ActivitySkeleton rows={6} />
        ) : feed.isError ? (
          <ErrorState title="Couldn't load the feed" info={errorMessage(feed.error)} onRetry={() => void feed.refetch()} retrying={feed.isRefetching} />
        ) : (
          <View>
            <EmptyState
              icon={Rss}
              title="Nothing from people you follow yet"
              action={
                <Button variant="secondary" onPress={() => router.push("/people")}>
                  Find people
                </Button>
              }
            />
            <StartWith />
          </View>
        )
      }
      ListFooterComponent={
        feed.isFetchingNextPage ? (
          <View style={{ paddingTop: s[4], alignItems: "center" }}>
            <Spinner size={20} />
          </View>
        ) : null
      }
      onEndReached={() => {
        if (feed.hasNextPage && !feed.isFetchingNextPage) void feed.fetchNextPage();
      }}
      onEndReachedThreshold={1.2}
      refreshControl={
        <RefreshControl refreshing={feed.isRefetching && !feed.isFetchingNextPage} onRefresh={() => void feed.refetch()} tintColor={theme.colors.textMuted} />
      }
      style={{ flex: 1, backgroundColor: theme.colors.canvas }}
      contentContainerStyle={{ paddingTop: insets.top + s[4], paddingBottom: s[8], paddingHorizontal: layout.gutter, width: "100%", maxWidth: layout.maxContent, alignSelf: "center" }}
    />
  );
}
