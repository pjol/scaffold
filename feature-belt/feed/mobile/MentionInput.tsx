import { insertMention, mentionQueryAt, useFeedPeople, type FeedAuthor } from "@scaffoldapp/shared";
import { Avatar, Spinner, Text, TextArea, useTheme } from "@scaffoldapp/ui-native";
import { forwardRef, useState } from "react";
import { Pressable, View, type TextInput } from "react-native";

export type MentionPerson = Pick<FeedAuthor, "id" | "display_name">;

export interface MentionInputProps {
  value: string;
  onChange: (value: string) => void;
  people: MentionPerson[];
  onPeopleChange: (people: MentionPerson[]) => void;
  placeholder?: string;
  accessibilityLabel: string;
  autoFocus?: boolean;
  minHeight?: number;
}

/** A multiline input that suggests people after "@"; tap a suggestion to insert "@Name ". */
export const MentionInput = forwardRef<TextInput, MentionInputProps>(function MentionInput({ value, onChange, people, onPeopleChange, placeholder, accessibilityLabel, autoFocus, minHeight }, ref) {
  const theme = useTheme();
  const s = theme.space;
  const [selection, setSelection] = useState({ start: value.length, end: value.length });
  const [forced, setForced] = useState<{ start: number; end: number } | undefined>(undefined);
  const mention = mentionQueryAt(value, selection.start);
  const open = !!mention && mention.query.length > 0;
  const suggestions = useFeedPeople(open ? mention.query : "");
  const items = open ? (suggestions.data?.items ?? []) : [];

  const pick = (person: FeedAuthor) => {
    if (!mention) return;
    const next = insertMention(value, mention.start, selection.start, person);
    if (!people.some((p) => p.id === person.id)) onPeopleChange([...people, { id: person.id, display_name: person.display_name }]);
    onChange(next.text);
    const caret = { start: next.caret, end: next.caret };
    setSelection(caret);
    setForced(caret);
  };

  return (
    <View style={{ gap: s[2] }}>
      <TextArea
        ref={ref}
        value={value}
        onChangeText={(t) => {
          setForced(undefined);
          onChange(t);
        }}
        selection={forced}
        onSelectionChange={(e) => {
          setSelection(e.nativeEvent.selection);
          if (forced) setForced(undefined);
        }}
        placeholder={placeholder}
        accessibilityLabel={accessibilityLabel}
        autoFocus={autoFocus}
        style={minHeight ? { minHeight } : undefined}
        textAlignVertical="top"
      />
      {open ? (
        <View accessibilityRole="list" accessibilityLabel="People" style={{ borderRadius: theme.radius.md, borderWidth: 1, borderColor: theme.colors.border, backgroundColor: theme.colors.surfaceRaised, overflow: "hidden" }}>
          {items.length === 0 ? (
            <View style={{ minHeight: theme.tap.base, alignItems: "center", justifyContent: "center" }}>
              {suggestions.isFetching ? <Spinner size={18} /> : <Text role="label" tone="muted">No one by that name</Text>}
            </View>
          ) : (
            items.map((p) => (
              <Pressable
                key={p.id}
                accessibilityRole="button"
                accessibilityLabel={`Mention ${p.display_name}`}
                onPress={() => pick(p)}
                style={({ pressed }) => ({ minHeight: theme.tap.base, flexDirection: "row", alignItems: "center", gap: s[3], paddingHorizontal: s[3], backgroundColor: pressed ? theme.colors.sunken : "transparent" })}
              >
                <Avatar name={p.display_name} src={p.avatar_url} size="sm" />
                <Text role="label" numberOfLines={1} style={{ flex: 1 }}>
                  {p.display_name}
                </Text>
              </Pressable>
            ))
          )}
        </View>
      ) : null}
    </View>
  );
});
