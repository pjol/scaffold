import { chatInboxTime, useChatSearch, useDebouncedValue, type ChatSearchHit } from "@scaffoldapp/shared";
import { Avatar, EmptyState, ErrorState, Input, SkeletonRow, Spinner, Text, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router } from "expo-router";
import { Search, SearchX } from "lucide-react-native";
import { useState } from "react";
import { FlatList, Pressable, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

function Hit({ hit }: { hit: ChatSearchHit }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <Pressable
      accessibilityRole="button"
      onPress={() => router.push(`/chat/${hit.conversation.id}?message=${hit.message.id}`)}
      style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.xl, paddingVertical: s[2], backgroundColor: pressed ? theme.colors.sunken : "transparent" })}
    >
      <Avatar name={hit.message.sender.display_name} src={hit.message.sender.avatar_url} size="lg" />
      <View style={{ flex: 1, minWidth: 0, gap: s[0.5] }}>
        <View style={{ flexDirection: "row", gap: s[2], alignItems: "center" }}>
          <Text role="body" weight={600} numberOfLines={1} style={{ flex: 1 }}>
            {hit.conversation.title}
          </Text>
          <Text role="caption" tone="muted">
            {chatInboxTime(hit.message.created_at)}
          </Text>
        </View>
        <Text role="label" tone="muted" numberOfLines={2}>
          {hit.message.sender.display_name}: {hit.message.body}
        </Text>
      </View>
    </Pressable>
  );
}

/** chat/search: messages in your conversations, newest first. */
export function SearchScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const insets = useSafeAreaInsets();
  const s = theme.space;
  const [query, setQuery] = useState("");
  const q = useDebouncedValue(query.trim(), 300);
  const results = useChatSearch(q);
  const pad = { paddingLeft: insets.left + layout.gutter, paddingRight: insets.right + layout.gutter };

  let body;
  if (q.length < 2) body = <EmptyState icon={Search} title="Search your messages" compact />;
  else if (results.isPending)
    body = (
      <View style={{ gap: s[2] }}>
        {Array.from({ length: 4 }, (_, i) => (
          <SkeletonRow key={i} />
        ))}
      </View>
    );
  else if (results.isError) body = <ErrorState title="Couldn't search" info={results.error.message} onRetry={() => void results.refetch()} retrying={results.isFetching} compact />;
  else body = null;

  return (
    <View style={{ flex: 1, backgroundColor: theme.colors.canvas }}>
      <Stack.Screen options={{ title: "Search" }} />
      <View style={{ ...pad, paddingTop: s[4], paddingBottom: s[3] }}>
        <Input value={query} onChangeText={setQuery} placeholder="Search messages" accessibilityLabel="Search messages" leadingIcon={Search} autoFocus returnKeyType="search" />
      </View>
      {body ? (
        <View style={pad}>{body}</View>
      ) : (
        <FlatList
          data={results.data?.items ?? []}
          keyExtractor={(h) => h.message.id}
          renderItem={({ item }) => <Hit hit={item} />}
          contentContainerStyle={{ ...pad, paddingBottom: insets.bottom + s[8], flexGrow: 1 }}
          keyboardShouldPersistTaps="handled"
          onEndReached={() => results.hasNextPage && !results.isFetchingNextPage && void results.fetchNextPage()}
          ListFooterComponent={results.isFetchingNextPage ? <Spinner label="Loading more" /> : null}
          ListEmptyComponent={<EmptyState icon={SearchX} title="No matching messages" compact />}
        />
      )}
    </View>
  );
}
