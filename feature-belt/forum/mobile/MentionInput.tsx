import { forumMentionQuery, insertForumMention, useForumPeople, type ForumAuthor } from "@scaffoldapp/shared";
import { Avatar, ListRow, Spinner, TextArea, useTheme } from "@scaffoldapp/ui-native";
import { useState } from "react";
import { View } from "react-native";

export type ForumMentionPerson = Pick<ForumAuthor, "id" | "display_name">;

/** A multiline input that suggests people after "@"; picking one writes "@Name " and remembers who it was. */
export function ForumMentionInput({
  value,
  onChange,
  people,
  onPeopleChange,
  placeholder,
  accessibilityLabel,
  minHeight,
  autoFocus,
}: {
  value: string;
  onChange: (value: string) => void;
  people: ForumMentionPerson[];
  onPeopleChange: (people: ForumMentionPerson[]) => void;
  placeholder?: string;
  accessibilityLabel?: string;
  minHeight?: number;
  autoFocus?: boolean;
}) {
  const theme = useTheme();
  const [caret, setCaret] = useState(0);
  const [selection, setSelection] = useState<{ start: number; end: number } | undefined>(undefined);
  const mention = forumMentionQuery(value, caret);
  const open = !!mention && mention.query.length > 0;
  const suggestions = useForumPeople(open ? mention.query : "");
  const items = open ? (suggestions.data?.items ?? []) : [];

  const pick = (p: ForumAuthor) => {
    if (!mention) return;
    const next = insertForumMention(value, mention.start, caret, p.display_name);
    if (!people.some((x) => x.id === p.id)) onPeopleChange([...people, { id: p.id, display_name: p.display_name }]);
    onChange(next.text);
    setCaret(next.caret);
    setSelection({ start: next.caret, end: next.caret });
  };

  return (
    <View style={{ gap: theme.space[2] }}>
      <TextArea
        value={value}
        onChangeText={(t) => {
          onChange(t);
          setSelection(undefined);
        }}
        onSelectionChange={(e) => setCaret(e.nativeEvent.selection.start)}
        {...(selection ? { selection } : {})}
        placeholder={placeholder}
        accessibilityLabel={accessibilityLabel}
        autoFocus={autoFocus}
        style={{ minHeight: minHeight ?? theme.space[24], textAlignVertical: "top" }}
      />
      {open && (items.length > 0 || suggestions.isFetching) ? (
        <View accessibilityRole="list" accessibilityLabel="People" style={{ borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.md }}>
          {items.length === 0 ? (
            <View style={{ padding: theme.space[3], alignItems: "center" }}>
              <Spinner size={16} />
            </View>
          ) : (
            items.map((p) => <ListRow key={p.id} title={p.display_name} leading={<Avatar name={p.display_name} src={p.avatar_url} size="sm" />} onPress={() => pick(p)} chevron={false} />)
          )}
        </View>
      ) : null}
    </View>
  );
}
