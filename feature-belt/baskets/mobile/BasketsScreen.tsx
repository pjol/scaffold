import { useBasketsList, useMyBaskets, type BasketsSummary, type PagedQueryResult } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, SegmentedControl, SkeletonRow, Spinner, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { Plus, ShoppingBasket } from "lucide-react-native";
import { useState } from "react";
import { FlatList, RefreshControl, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { BasketRow } from "./parts";

type TabId = "popular" | "new" | "mine";
const TABS: { value: TabId; label: string }[] = [
  { value: "popular", label: "Popular" },
  { value: "new", label: "New" },
  { value: "mine", label: "Mine" },
];

function List({ query, mine }: { query: PagedQueryResult<BasketsSummary>; mine: boolean }) {
  const theme = useTheme();
  const layout = useLayout();
  const insets = useSafeAreaInsets();
  const s = theme.space;
  const pad = { paddingLeft: insets.left + layout.gutter, paddingRight: insets.right + layout.gutter };
  if (query.isPending) {
    return (
      <View style={{ gap: s[2], ...pad }} accessibilityLabel="Loading baskets" accessibilityRole="progressbar">
        {Array.from({ length: 6 }, (_, i) => (
          <SkeletonRow key={i} />
        ))}
      </View>
    );
  }
  if (query.isError) {
    return <ErrorState title="Couldn't load baskets" info={query.error.message} onRetry={() => void query.refetch()} retrying={query.isFetching} />;
  }
  return (
    <FlatList
      data={query.data.items}
      keyExtractor={(b) => b.id}
      renderItem={({ item }) => <BasketRow b={item} />}
      contentContainerStyle={{ ...pad, paddingBottom: insets.bottom + s[8], flexGrow: 1 }}
      refreshControl={<RefreshControl refreshing={query.isRefetching} onRefresh={() => void query.refetch()} tintColor={theme.colors.textMuted} />}
      onEndReached={() => {
        if (query.hasNextPage && !query.isFetchingNextPage) void query.fetchNextPage();
      }}
      ListFooterComponent={query.isFetchingNextPage ? <Spinner /> : null}
      ListEmptyComponent={
        <EmptyState
          icon={ShoppingBasket}
          title={mine ? "You haven't made a basket" : "No baskets yet"}
          action={
            <Button variant="secondary" leftIcon={Plus} onPress={() => router.push("/baskets/new")}>
              New basket
            </Button>
          }
        />
      }
    />
  );
}

function Discover({ sort }: { sort: "popular" | "new" }) {
  return <List query={useBasketsList({ sort })} mine={false} />;
}

function Mine() {
  return <List query={useMyBaskets()} mine />;
}

/** Baskets: popular, new and the caller's own; a press opens one. */
export function BasketsScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const insets = useSafeAreaInsets();
  const s = theme.space;
  const [tab, setTab] = useState<TabId>("popular");
  return (
    <View style={{ flex: 1, backgroundColor: theme.colors.canvas }}>
      <Stack.Screen options={{ title: "Baskets" }} />
      <View style={{ gap: s[3], paddingTop: s[4], paddingBottom: s[3], paddingLeft: insets.left + layout.gutter, paddingRight: insets.right + layout.gutter }}>
        <SegmentedControl accessibilityLabel="Baskets" options={TABS} value={tab} onValueChange={setTab} />
        <Button leftIcon={Plus} onPress={() => router.push("/baskets/new")}>
          New basket
        </Button>
      </View>
      <View style={{ flex: 1 }}>{tab === "mine" ? <Mine /> : <Discover key={tab} sort={tab} />}</View>
    </View>
  );
}
