import { errorMessage, formatOraclePrice, useDebouncedValue, useOracleFeeds } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Input, ListGroup, ListRow, Screen, SkeletonRow, Text, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { LineChart, Search } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, View } from "react-native";
import { OracleStatusBadge } from "./components";

/** Prices: every listed feed with its published price, live over realtime. */
export function OracleFeedsScreen() {
  const theme = useTheme();
  const [q, setQ] = useState("");
  const query = useDebouncedValue(q.trim(), 300);
  const feeds = useOracleFeeds({ q: query || undefined, limit: 50 });
  const rows = feeds.data?.items ?? [];

  return (
    <Screen
      edges={["bottom"]}
      gap={theme.space[4]}
      refreshControl={<RefreshControl refreshing={feeds.isRefetching && !feeds.isFetchingNextPage} onRefresh={() => void feeds.refetch()} />}
    >
      <Stack.Screen options={{ title: "Prices" }} />
      <Input leadingIcon={Search} value={q} onChangeText={setQ} placeholder="Key or name" accessibilityLabel="Search prices" autoCapitalize="none" returnKeyType="search" />
      {feeds.isPending ? (
        <View>
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </View>
      ) : feeds.isError ? (
        <ErrorState title="Couldn't load prices" info={errorMessage(feeds.error)} onRetry={() => void feeds.refetch()} retrying={feeds.isRefetching} />
      ) : rows.length === 0 ? (
        <EmptyState icon={LineChart} title={query ? "No matching prices" : "No prices yet"} />
      ) : (
        <ListGroup>
          {rows.map((f) => (
            <ListRow
              key={f.id}
              title={f.name}
              trailing={
                <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
                  <Text role="label" tabular>
                    {formatOraclePrice(f.price)}
                  </Text>
                  <OracleStatusBadge status={f.status} />
                </View>
              }
              onPress={() => router.push({ pathname: "/oracle/[key]", params: { key: f.key } })}
            />
          ))}
        </ListGroup>
      )}
      {feeds.hasNextPage ? (
        <Button variant="ghost" loading={feeds.isFetchingNextPage} onPress={() => void feeds.fetchNextPage()}>
          Load more
        </Button>
      ) : null}
    </Screen>
  );
}
