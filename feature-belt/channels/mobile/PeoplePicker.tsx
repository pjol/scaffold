import { useChannelsPeople, useDebouncedValue, type ChannelsPerson } from "@scaffoldapp/shared";
import { Chip, EmptyState, ErrorState, Input, SkeletonRow, Text, useTheme } from "@scaffoldapp/ui-native";
import { Check, Search, UserRoundX } from "lucide-react-native";
import { useState } from "react";
import { Pressable, View } from "react-native";
import { PresenceAvatar } from "@/features/presence";

export interface PeoplePickerProps {
  orgId: string;
  /** Marks people already in this channel (they can't be picked again). */
  channelId?: string;
  selected: ChannelsPerson[];
  onChange: (people: ChannelsPerson[]) => void;
  /** Most people that can be picked. */
  max: number;
  /** Leave yourself out of the list. */
  meId?: string | undefined;
}

/** Search the org's people and pick several; chips show who's picked. */
export function PeoplePicker({ orgId, channelId, selected, onChange, max, meId }: PeoplePickerProps) {
  const theme = useTheme();
  const col = theme.colors;
  const s = theme.space;
  const [query, setQuery] = useState("");
  const q = useDebouncedValue(query, 250);
  const people = useChannelsPeople(orgId, q, channelId);
  const picked = new Set(selected.map((p) => p.id));
  const toggle = (p: ChannelsPerson) => {
    if (picked.has(p.id)) onChange(selected.filter((x) => x.id !== p.id));
    else if (selected.length < max) onChange([...selected, p]);
  };
  const list = (people.data?.items ?? []).filter((p) => p.id !== meId);

  return (
    <View style={{ gap: s[3] }}>
      <Input value={query} onChangeText={setQuery} placeholder="Search by name or email" leadingIcon={Search} autoCapitalize="none" autoCorrect={false} accessibilityLabel="Search people" />
      {selected.length > 0 ? (
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }} accessibilityLabel="Picked">
          {selected.map((p) => (
            <Chip key={p.id} size="sm" selected onPress={() => toggle(p)} accessibilityLabel={`Remove ${p.display_name}`}>
              {p.display_name}
            </Chip>
          ))}
        </View>
      ) : null}
      {people.isPending ? (
        <View style={{ gap: s[2] }}>
          {Array.from({ length: 5 }, (_, i) => (
            <SkeletonRow key={i} />
          ))}
        </View>
      ) : people.isError ? (
        <ErrorState compact title="Couldn't load people" info={people.error.message} onRetry={() => void people.refetch()} retrying={people.isFetching} />
      ) : list.length === 0 ? (
        <EmptyState compact icon={UserRoundX} title="No one found" />
      ) : (
        <View>
          {list.map((p) => {
            const on = picked.has(p.id);
            const already = !!channelId && p.in_channel;
            const full = !on && selected.length >= max;
            return (
              <Pressable
                key={p.id}
                accessibilityRole="checkbox"
                accessibilityState={{ checked: on || already, disabled: already || full }}
                accessibilityLabel={already ? `${p.display_name}, already in this channel` : p.display_name}
                disabled={already || full}
                onPress={() => toggle(p)}
                style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.lg, backgroundColor: pressed ? col.sunken : "transparent", opacity: already || full ? 0.5 : 1 })}
              >
                <PresenceAvatar userId={p.id} name={p.display_name} src={p.avatar_url} size="sm" />
                <Text role="body" numberOfLines={1} style={{ flex: 1 }}>
                  {p.display_name}
                </Text>
                {already ? (
                  <Text role="caption" tone="muted">
                    Member
                  </Text>
                ) : on ? (
                  <Check color={col.primaryStrong} size={20} />
                ) : null}
              </Pressable>
            );
          })}
        </View>
      )}
    </View>
  );
}
