import {
  catalogMonogram,
  catalogQueryReady,
  errorMessage,
  formatCatalogPrice,
  useCatalogFind,
  useCatalogItems,
  useCatalogSummary,
  useDebouncedValue,
  type CatalogListItem,
} from "@scaffoldapp/shared";
import { Badge, Button, Chip, EmptyState, ErrorState, Input, ListGroup, ListRow, Screen, SkeletonRow, Text, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { router, Stack } from "expo-router";
import { Library, Search, SearchX } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, ScrollView, View } from "react-native";

/** An item's picture, or its initials on a tile. */
export function CatalogThumb({ item, size = 40 }: { item: Pick<CatalogListItem, "name" | "image_url">; size?: number }) {
  const theme = useTheme();
  const box = { width: size, height: size, borderRadius: theme.radius.md, backgroundColor: theme.colors.sunken };
  if (item.image_url) {
    return <Image source={{ uri: item.image_url }} style={box} contentFit="contain" accessibilityIgnoresInvertColors />;
  }
  return (
    <View style={[box, { alignItems: "center", justifyContent: "center" }]} accessibilityElementsHidden importantForAccessibility="no-hide-descendants">
      <Text role="label" tone="muted" weight={600}>
        {catalogMonogram(item.name)}
      </Text>
    </View>
  );
}

function Rows({ items }: { items: CatalogListItem[] }) {
  const theme = useTheme();
  const thumbs = items.some((i) => i.image_url);
  return (
    <ListGroup>
      {items.map((it) => {
        const price = formatCatalogPrice(it.ref_price, it.currency);
        return (
          <ListRow
            key={it.key}
            title={it.name}
            leading={thumbs ? <CatalogThumb item={it} /> : undefined}
            trailing={
              price || it.retired ? (
                <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
                  {price ? (
                    <Text role="label" tabular>
                      {price}
                    </Text>
                  ) : null}
                  {it.retired ? (
                    <Badge tone="warning" size="sm">
                      Retired
                    </Badge>
                  ) : null}
                </View>
              ) : undefined
            }
            onPress={() => router.push({ pathname: "/catalog/[key]", params: { key: it.key } })}
          />
        );
      })}
    </ListGroup>
  );
}

function Loading() {
  return (
    <View>
      <SkeletonRow />
      <SkeletonRow />
      <SkeletonRow />
      <SkeletonRow />
    </View>
  );
}

/** Catalog: search by name or other name, or browse by category. */
export function CatalogScreen() {
  const theme = useTheme();
  const [text, setText] = useState("");
  const [category, setCategory] = useState("");
  const q = useDebouncedValue(text.trim(), 250);
  const searching = catalogQueryReady(q);
  const summary = useCatalogSummary();
  const find = useCatalogFind({ q, category: category || undefined, limit: 50 });
  const browse = useCatalogItems({ category: category || undefined, limit: 50 }, { enabled: !searching });
  const categories = summary.data?.categories ?? [];

  let body;
  if (searching) {
    if (find.isError) body = <ErrorState title="Couldn't search the catalog" info={errorMessage(find.error)} onRetry={find.retry} retrying={find.isFetching} />;
    else if (!find.items) body = <Loading />;
    else if (find.items.length === 0) body = <EmptyState icon={SearchX} title="No matching items" />;
    else body = <Rows items={find.items} />;
  } else if (browse.isPending) {
    body = <Loading />;
  } else if (browse.isError) {
    body = <ErrorState title="Couldn't load the catalog" info={errorMessage(browse.error)} onRetry={() => void browse.refetch()} retrying={browse.isRefetching} />;
  } else if (browse.data.items.length === 0) {
    body = <EmptyState icon={Library} title={category ? "No items in this category" : "No items yet"} />;
  } else {
    body = (
      <>
        <Rows items={browse.data.items} />
        {browse.hasNextPage ? (
          <Button variant="ghost" loading={browse.isFetchingNextPage} onPress={() => void browse.fetchNextPage()}>
            Load more
          </Button>
        ) : null}
      </>
    );
  }

  return (
    <Screen
      edges={["bottom"]}
      gap={theme.space[4]}
      refreshControl={
        <RefreshControl
          refreshing={!searching && browse.isRefetching && !browse.isFetchingNextPage}
          onRefresh={() => void Promise.all([summary.refetch(), browse.refetch()])}
        />
      }
    >
      <Stack.Screen options={{ title: "Catalog" }} />
      <Input
        leadingIcon={Search}
        value={text}
        onChangeText={setText}
        placeholder="Name or other name"
        accessibilityLabel="Search the catalog"
        autoCapitalize="none"
        autoCorrect={false}
        returnKeyType="search"
      />
      {categories.length > 1 ? (
        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          style={{ marginHorizontal: -theme.space[4] }}
          contentContainerStyle={{ gap: theme.space[2], paddingHorizontal: theme.space[4] }}
          accessibilityRole="tablist"
          accessibilityLabel="Category"
        >
          <Chip size="sm" selected={category === ""} onPress={() => setCategory("")}>
            All
          </Chip>
          {categories.map((c) => (
            <Chip key={c.name} size="sm" count={c.count} selected={category === c.name} onPress={() => setCategory(category === c.name ? "" : c.name)}>
              {c.name}
            </Chip>
          ))}
        </ScrollView>
      ) : null}
      {body}
    </Screen>
  );
}
