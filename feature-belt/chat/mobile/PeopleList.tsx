import { useChatPeople, useDebouncedValue, type ChatUser } from "@scaffoldapp/shared";
import { Avatar, EmptyState, ErrorState, Input, SkeletonRow, Text, useTheme } from "@scaffoldapp/ui-native";
import { Check, Search, UserRoundSearch } from "lucide-react-native";
import { useState } from "react";
import { Pressable, View } from "react-native";

/** Search people by name or exact email (recent contacts before typing); tap to pick or toggle. */
export function PeopleList({ selected, exclude = [], onPress, multiple }: { selected: ChatUser[]; exclude?: string[]; onPress: (u: ChatUser) => void; multiple?: boolean }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [query, setQuery] = useState("");
  const q = useDebouncedValue(query.trim(), 250);
  const people = useChatPeople(q.length === 1 ? "" : q);
  const chosen = new Set(selected.map((u) => u.id));
  const excluded = new Set(exclude);

  return (
    <View style={{ gap: s[3] }}>
      <Input value={query} onChangeText={setQuery} placeholder="Name or email" accessibilityLabel="Search people" leadingIcon={Search} autoCapitalize="none" autoCorrect={false} autoFocus />
      {people.isPending ? (
        <View style={{ gap: s[2] }}>
          {Array.from({ length: 4 }, (_, i) => (
            <SkeletonRow key={i} />
          ))}
        </View>
      ) : people.isError ? (
        <ErrorState title="Couldn't load people" info={people.error.message} onRetry={() => void people.refetch()} retrying={people.isFetching} compact />
      ) : people.data.items.length === 0 ? (
        <EmptyState icon={UserRoundSearch} title={q ? "No one found" : "Search for someone to message"} compact />
      ) : (
        <View>
          {people.data.items.map((u) => {
            const already = excluded.has(u.id);
            const on = chosen.has(u.id) || already;
            return (
              <Pressable
                key={u.id}
                accessibilityRole={multiple ? "checkbox" : "button"}
                accessibilityState={multiple ? { checked: on, disabled: already } : { disabled: already }}
                accessibilityLabel={u.display_name}
                disabled={already}
                onPress={() => onPress(u)}
                style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.lg, paddingVertical: s[2], backgroundColor: pressed ? c.sunken : "transparent", opacity: already ? 0.6 : 1 })}
              >
                <Avatar name={u.display_name} src={u.avatar_url} size="md" />
                <Text role="body" numberOfLines={1} style={{ flex: 1 }}>
                  {u.display_name}
                </Text>
                {multiple && on ? <Check color={c.primaryStrong} size={20} /> : null}
              </Pressable>
            );
          })}
        </View>
      )}
    </View>
  );
}
