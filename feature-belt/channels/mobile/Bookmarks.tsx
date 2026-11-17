import { errorMessage, useChannelsBookmarks, useCreateChannelsBookmark, type ChannelsDetail } from "@scaffoldapp/shared";
import { Button, Field, Input, Sheet, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { FileText, Link2, Lock, Plus } from "lucide-react-native";
import { useState } from "react";
import { Linking, Pressable, ScrollView, View } from "react-native";
import { useOpenAppPath } from "./MessageExtras";

/** A channel's bookmarks as a sideways-scrolling row under the header; members add links. */
export function BookmarksRow({ c }: { c: ChannelsDetail }) {
  const theme = useTheme();
  const col = theme.colors;
  const s = theme.space;
  const list = useChannelsBookmarks(c.bookmark_count > 0 || c.member ? c.id : null);
  const create = useCreateChannelsBookmark(c.id);
  const open = useOpenAppPath();
  const [adding, setAdding] = useState(false);
  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");
  const items = list.data?.items ?? [];
  const canAdd = c.member && !c.archived;
  if (items.length === 0 && !canAdd) return null;
  return (
    <View style={{ borderBottomWidth: 1, borderBottomColor: col.border }}>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2], paddingHorizontal: s[4], paddingVertical: s[2], alignItems: "center" }} accessibilityLabel="Bookmarks">
        {items.map((b) => {
          const Icon = !b.accessible ? Lock : b.kind === "internal" ? FileText : Link2;
          return (
            <Pressable
              key={b.id}
              accessibilityRole="link"
              accessibilityLabel={b.accessible ? b.title : "Bookmark you can't open"}
              disabled={!b.accessible}
              onPress={() => (b.kind === "internal" ? open(b.card?.link ?? b.url) : void Linking.openURL(b.url))}
              style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[1], minHeight: theme.tap.sm, paddingHorizontal: s[3], borderRadius: theme.radius.pill, borderWidth: 1, borderColor: col.border, backgroundColor: pressed ? col.sunken : col.surface })}
            >
              <Icon color={b.accessible ? col.textMuted : col.textSubtle} size={14} />
              <Text role="label" tone={b.accessible ? "default" : "muted"} numberOfLines={1} style={{ maxWidth: 180 }}>
                {b.title}
              </Text>
            </Pressable>
          );
        })}
        {canAdd ? (
          <Button size="md" variant="ghost" leftIcon={Plus} onPress={() => (setUrl(""), setTitle(""), setAdding(true))}>
            {items.length === 0 ? "Add a bookmark" : "Add"}
          </Button>
        ) : null}
      </ScrollView>
      <Sheet open={adding} onClose={() => setAdding(false)} title="Add a bookmark" info="A link to a site, or to a page, file or record in this app.">
        <View style={{ gap: s[4] }}>
          <Field label="Link">
            <Input value={url} onChangeText={setUrl} placeholder="https://" autoCapitalize="none" keyboardType="url" />
          </Field>
          <Field label="Title">
            <Input value={title} onChangeText={setTitle} maxLength={120} />
          </Field>
          <Button
            disabled={!url.trim()}
            loading={create.isPending}
            onPress={() => create.mutate({ url: url.trim(), ...(title.trim() ? { title: title.trim() } : {}) }, { onSuccess: () => setAdding(false), onError: (e) => toast.error(errorMessage(e)) })}
          >
            Add bookmark
          </Button>
        </View>
      </Sheet>
    </View>
  );
}
