import { useAccessDirectory, type AccessDirectoryEntry } from "@scaffoldapp/shared";
import { Avatar, Input, ListRow, Spinner, Text, useTheme } from "@scaffoldapp/ui-native";
import { Search } from "lucide-react-native";
import { useMemo, useState } from "react";
import { View } from "react-native";
import { memberCount } from "./labels";

export interface PeoplePickerProps {
  orgId: string;
  /** Default both. */
  types?: "user" | "team" | "user,team";
  /** Ids not to offer (already chosen or already have access). */
  exclude?: ReadonlySet<string>;
  onPick: (entry: AccessDirectoryEntry) => void;
  placeholder?: string;
}

/** Search the org's people and teams; results list under the field (works inside sheets). */
export function PeoplePicker({ orgId, types = "user,team", exclude, onPick, placeholder }: PeoplePickerProps) {
  const theme = useTheme();
  const [query, setQuery] = useState("");
  const [focused, setFocused] = useState(false);
  const dir = useAccessDirectory(orgId, query, { types, limit: 8, enabled: focused || query.length > 0 });
  const items = useMemo(() => (dir.data ?? []).filter((e) => !exclude?.has(e.id)), [dir.data, exclude]);
  const hint = types === "team" ? "teams" : types === "user" ? "people" : "people or teams";
  const open = query.trim().length > 0;
  return (
    <View style={{ gap: theme.space[2] }}>
      <Input
        value={query}
        onChangeText={setQuery}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        placeholder={placeholder ?? `Search ${hint}`}
        leadingIcon={Search}
        trailing={dir.isFetching && open ? <Spinner size={16} /> : undefined}
        autoCapitalize="none"
        autoCorrect={false}
        accessibilityLabel={`Search ${hint}`}
        returnKeyType="search"
      />
      {open ? (
        <View>
          {dir.isError ? (
            <Text tone="danger">Couldn&apos;t search. Check your connection.</Text>
          ) : items.length === 0 ? (
            <Text tone="muted">{dir.isFetching ? "Searching…" : `No ${hint} match “${query.trim()}”`}</Text>
          ) : (
            items.map((e) => (
              <ListRow
                key={e.id}
                title={e.type === "team" ? `${e.name} (@${e.handle})` : e.name}
                leading={<Avatar name={e.name} src={e.avatar_url} size="sm" />}
                trailing={e.type === "team" ? <Text role="caption" tone="muted">{memberCount(e.member_count)}</Text> : undefined}
                chevron={false}
                onPress={() => {
                  onPick(e);
                  setQuery("");
                }}
              />
            ))
          )}
        </View>
      ) : null}
    </View>
  );
}
