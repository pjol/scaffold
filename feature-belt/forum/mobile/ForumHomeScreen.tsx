import { errorMessage, FORUM_TOP_PERIODS, useForumCommunities, useForumPosts, type ForumPostSort, type ForumPostSummary, type ForumTopPeriod } from "@scaffoldapp/shared";
import { Button, Chip, EmptyState, ErrorState, SegmentedControl, Skeleton, Spinner, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { router, type Href } from "expo-router";
import { MessagesSquare, SquarePen } from "lucide-react-native";
import { useState, type ReactElement } from "react";
import { FlatList, RefreshControl, ScrollView, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { forumCommunityHref, ForumPostRow } from "./parts";

export function ForumRowsSkeleton() {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View style={{ gap: s[6] }} accessibilityLabel="Loading posts" accessibilityRole="progressbar">
      {[0, 1, 2].map((i) => (
        <View key={i} style={{ gap: s[2] }}>
          <Skeleton width="40%" height={12} />
          <Skeleton width="85%" height={16} />
          <Skeleton height={14} />
        </View>
      ))}
    </View>
  );
}

/** Hot / New / Top, with the period chips for Top. */
export function ForumSortControls({ sort, period, onSort, onPeriod }: { sort: ForumPostSort; period: ForumTopPeriod; onSort: (s: ForumPostSort) => void; onPeriod: (t: ForumTopPeriod) => void }) {
  const theme = useTheme();
  return (
    <View style={{ gap: theme.space[3] }}>
      <SegmentedControl<ForumPostSort>
        accessibilityLabel="Sort posts"
        value={sort}
        onValueChange={onSort}
        options={[
          { value: "hot", label: "Hot" },
          { value: "new", label: "New" },
          { value: "top", label: "Top" },
        ]}
      />
      {sort === "top" ? (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: theme.space[2] }}>
          {FORUM_TOP_PERIODS.map((p) => (
            <Chip key={p.value} size="sm" selected={period === p.value} onPress={() => onPeriod(p.value)}>
              {p.label}
            </Chip>
          ))}
        </ScrollView>
      ) : null}
    </View>
  );
}

/** A FlatList of posts with pull to refresh, endless paging, skeleton, error and empty states. */
export function ForumPostFeed({ community, header, showCommunity = true, empty }: { community?: string; header: ReactElement; showCommunity?: boolean; empty?: ReactElement }) {
  const theme = useTheme();
  const layout = useLayout();
  const insets = useSafeAreaInsets();
  const s = theme.space;
  const [sort, setSort] = useState<ForumPostSort>("hot");
  const [period, setPeriod] = useState<ForumTopPeriod>("week");
  const posts = useForumPosts({ ...(community ? { community } : {}), sort, ...(sort === "top" ? { t: period } : {}) });
  const items = posts.data?.items ?? [];
  return (
    <FlatList<ForumPostSummary>
      data={posts.isPending || posts.isError ? [] : items}
      keyExtractor={(p) => p.id}
      renderItem={({ item }) => <ForumPostRow post={item} showCommunity={showCommunity} />}
      ItemSeparatorComponent={() => <View style={{ height: 1, backgroundColor: theme.colors.border }} />}
      ListHeaderComponent={
        <View style={{ gap: s[4], paddingBottom: s[2] }}>
          {header}
          <ForumSortControls sort={sort} period={period} onSort={setSort} onPeriod={setPeriod} />
        </View>
      }
      ListEmptyComponent={
        posts.isPending ? (
          <ForumRowsSkeleton />
        ) : posts.isError ? (
          <ErrorState title="Couldn't load posts" info={errorMessage(posts.error)} onRetry={() => void posts.refetch()} retrying={posts.isRefetching} />
        ) : (
          (empty ?? <EmptyState icon={MessagesSquare} title="No posts yet" />)
        )
      }
      ListFooterComponent={
        posts.isFetchingNextPage ? (
          <View style={{ paddingTop: s[4], alignItems: "center" }}>
            <Spinner size={20} />
          </View>
        ) : null
      }
      onEndReached={() => {
        if (posts.hasNextPage && !posts.isFetchingNextPage) void posts.fetchNextPage();
      }}
      onEndReachedThreshold={1.2}
      refreshControl={<RefreshControl refreshing={posts.isRefetching && !posts.isFetchingNextPage} onRefresh={() => void posts.refetch()} tintColor={theme.colors.textMuted} />}
      style={{ flex: 1, backgroundColor: theme.colors.canvas }}
      contentContainerStyle={{ paddingTop: s[4], paddingBottom: insets.bottom + s[8], paddingHorizontal: layout.gutter, width: "100%", maxWidth: layout.maxContent, alignSelf: "center" }}
    />
  );
}

/** /forum (a stack screen titled Forum): New post, community chips, then the hot feed across communities. */
export function ForumHomeScreen() {
  const theme = useTheme();
  const communities = useForumCommunities();
  const list = communities.data?.items ?? [];
  const header = (
    <View style={{ gap: theme.space[4] }}>
      <Button size="md" leftIcon={SquarePen} style={{ alignSelf: "flex-start" }} onPress={() => router.push("/forum/submit" as Href)}>
        New post
      </Button>
      {list.length > 0 ? (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: theme.space[2] }} accessibilityLabel="Communities">
          {list.map((c) => (
            <Chip key={c.id} size="sm" onPress={() => router.push(forumCommunityHref(c.slug))}>
              {c.title}
            </Chip>
          ))}
        </ScrollView>
      ) : null}
    </View>
  );
  return (
    <ForumPostFeed
      header={header}
      empty={
        <EmptyState
          icon={MessagesSquare}
          title="No posts yet"
          action={
            <Button variant="secondary" onPress={() => router.push("/forum/submit" as Href)}>
              Write the first post
            </Button>
          }
        />
      }
    />
  );
}
