import {
  attachmentExtension,
  attachmentName,
  FEED_REACTIONS,
  splitText,
  type FeedMedia,
  type FeedMention,
  type FeedReaction,
  type FeedReactionCounts,
} from "@scaffoldapp/shared";
import { IconButton, ListRow, PhotoLightbox, Sheet, Text, useTheme, type GalleryPhoto } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import * as WebBrowser from "expo-web-browser";
import { ChevronDown, FileText, Frown, Heart, Laugh, Play, Sparkles, ThumbsUp, type LucideIcon } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { Pressable, StyleSheet, View } from "react-native";

export const REACTION_META: Record<FeedReaction, { label: string; icon: LucideIcon }> = {
  like: { label: "Like", icon: ThumbsUp },
  love: { label: "Love", icon: Heart },
  laugh: { label: "Haha", icon: Laugh },
  wow: { label: "Wow", icon: Sparkles },
  sad: { label: "Sad", icon: Frown },
};

/** Open a link or attachment in the in-app browser. */
export function openUrl(url: string): void {
  void WebBrowser.openBrowserAsync(url).catch(() => undefined);
}

/** Post or comment text with @mentions and tappable links. */
export function RichText({ text, mentions }: { text: string; mentions: readonly FeedMention[] }) {
  const theme = useTheme();
  if (!text) return null;
  return (
    <Text role="body">
      {splitText(text, mentions).map((seg, i) =>
        seg.type === "mention" ? (
          <Text key={i} role="body" weight={600} style={{ color: theme.colors.primaryStrong }}>
            {seg.text}
          </Text>
        ) : seg.type === "link" ? (
          <Text key={i} role="body" accessibilityRole="link" onPress={() => openUrl(seg.href)} style={{ color: theme.colors.primaryStrong, textDecorationLine: "underline" }}>
            {seg.text}
          </Text>
        ) : (
          <Text key={i} role="body">
            {seg.text}
          </Text>
        ),
      )}
    </Text>
  );
}

/** The three most used reactions and the total. */
export function ReactionSummary({ reactions, total }: { reactions: FeedReactionCounts; total: number }) {
  const theme = useTheme();
  if (total <= 0) return null;
  const top = FEED_REACTIONS.filter((r) => (reactions[r] ?? 0) > 0)
    .sort((a, b) => (reactions[b] ?? 0) - (reactions[a] ?? 0))
    .slice(0, 3);
  const label = top.map((r) => `${reactions[r]} ${REACTION_META[r].label.toLowerCase()}`).join(", ");
  return (
    <View accessible accessibilityLabel={label} style={{ flexDirection: "row", alignItems: "center", gap: theme.space[1] }}>
      {top.map((r) => {
        const Icon = REACTION_META[r].icon;
        return <Icon key={r} size={14} color={theme.colors.primaryStrong} strokeWidth={2.25} />;
      })}
      <Text role="caption" tone="muted">
        {total}
      </Text>
    </View>
  );
}

/** Like toggle plus a chevron (or long press) for the full reaction set. */
export function ReactButton({ mine, onReact }: { mine: FeedReaction | null; onReact: (r: FeedReaction | null) => void }) {
  const theme = useTheme();
  const [picking, setPicking] = useState(false);
  const current = mine ? REACTION_META[mine] : REACTION_META.like;
  return (
    <View style={{ flexDirection: "row", alignItems: "center" }}>
      <Pressable
        accessibilityRole="button"
        accessibilityState={{ selected: !!mine }}
        accessibilityHint="Long press for more reactions"
        onPress={() => onReact(mine ? null : "like")}
        onLongPress={() => setPicking(true)}
        hitSlop={4}
        style={{ minHeight: theme.tap.base, flexDirection: "row", alignItems: "center", gap: theme.space[2], paddingHorizontal: theme.space[2] }}
      >
        <current.icon size={18} color={mine ? theme.colors.primaryStrong : theme.colors.textMuted} fill={mine ? theme.colors.primaryStrong : "transparent"} strokeWidth={2} />
        <Text role="label" weight={600} style={{ color: mine ? theme.colors.primaryStrong : theme.colors.textMuted }}>
          {current.label}
        </Text>
      </Pressable>
      <IconButton label="Choose a reaction" icon={ChevronDown} variant="ghost" onPress={() => setPicking(true)} />
      <Sheet open={picking} onClose={() => setPicking(false)} title="React">
        <View style={{ gap: theme.space[1] }}>
          {FEED_REACTIONS.map((r) => {
            const M = REACTION_META[r];
            return (
              <ListRow
                key={r}
                title={r === mine ? `Remove ${M.label.toLowerCase()}` : M.label}
                leading={<M.icon size={20} color={theme.colors.primaryStrong} strokeWidth={2} />}
                onPress={() => {
                  setPicking(false);
                  onReact(r === mine ? null : r);
                }}
              />
            );
          })}
        </View>
      </Sheet>
    </View>
  );
}

function Tile({ media, onPress, more, style }: { media: FeedMedia; onPress: () => void; more?: number; style: object }) {
  const theme = useTheme();
  return (
    <Pressable
      accessibilityRole={media.kind === "video" ? "button" : "imagebutton"}
      accessibilityLabel={media.kind === "video" ? "Play video" : more ? `Open photo, ${more} more` : "Open photo"}
      onPress={onPress}
      style={[{ overflow: "hidden", borderRadius: theme.radius.md, backgroundColor: media.kind === "video" ? theme.colors.text : theme.colors.sunken }, style]}
    >
      {media.kind === "image" ? <Image source={{ uri: media.url }} style={{ flex: 1 }} contentFit="cover" transition={160} /> : null}
      {media.kind === "video" ? (
        <View style={{ flex: 1, alignItems: "center", justifyContent: "center" }}>
          <Play size={32} color={theme.colors.textInverse} fill={theme.colors.textInverse} />
        </View>
      ) : null}
      {more ? (
        <View style={[StyleSheet.absoluteFill, { backgroundColor: theme.colors.overlay, alignItems: "center", justifyContent: "center" }]}>
          <Text role="title" style={{ color: theme.colors.textInverse }}>
            +{more}
          </Text>
        </View>
      ) : null}
    </Pressable>
  );
}

/** Photos and videos sized by count; photos open the lightbox, videos the in-app browser; documents as rows. */
export function MediaGrid({ media }: { media: readonly FeedMedia[] }) {
  const theme = useTheme();
  const visual = media.filter((m) => m.kind !== "file");
  const files = media.filter((m) => m.kind === "file");
  const photos: GalleryPhoto[] = visual.filter((m) => m.kind === "image").map((m) => ({ id: m.id, url: m.url }));
  const [open, setOpen] = useState(false);
  const [index, setIndex] = useState(0);
  const press = (m: FeedMedia) => {
    if (m.kind === "video") return openUrl(m.url);
    const i = photos.findIndex((p) => p.id === m.id);
    if (i >= 0) {
      setIndex(i);
      setOpen(true);
    }
  };
  const shown = visual.slice(0, 4);
  const extra = visual.length - shown.length;
  const gap = theme.space[1];
  const single = shown.length === 1 ? shown[0] : undefined;
  const ratio = single?.width && single.height ? Math.min(Math.max(single.width / single.height, 0.8), 1.91) : single?.kind === "video" ? 16 / 9 : 4 / 3;

  return (
    <View style={{ gap: theme.space[2] }}>
      {single ? (
        <Tile media={single} onPress={() => press(single)} style={{ width: "100%", aspectRatio: ratio }} />
      ) : shown.length > 1 ? (
        <View style={{ aspectRatio: 4 / 3, flexDirection: "row", gap }}>
          <View style={{ flex: 1, gap }}>
            {shown
              .filter((_, i) => (shown.length === 3 ? i === 0 : i % 2 === 0))
              .map((m) => (
                <Tile key={m.id} media={m} onPress={() => press(m)} style={{ flex: 1 }} />
              ))}
          </View>
          <View style={{ flex: 1, gap }}>
            {shown
              .filter((_, i) => (shown.length === 3 ? i > 0 : i % 2 === 1))
              .map((m) => (
                <Tile key={m.id} media={m} onPress={() => press(m)} more={m === shown[shown.length - 1] && extra > 0 ? extra : undefined} style={{ flex: 1 }} />
              ))}
          </View>
        </View>
      ) : null}
      {files.map((f) => (
        <FileRow key={f.id} name={attachmentName(f)} onPress={() => openUrl(f.url)} />
      ))}
      {photos.length > 0 ? <PhotoLightbox photos={photos} open={open} onClose={() => setOpen(false)} initialIndex={index} /> : null}
    </View>
  );
}

/** A document row: icon, name, type, and an optional trailing control. */
export function FileRow({ name, onPress, trailing }: { name: string; onPress?: () => void; trailing?: ReactNode }) {
  const theme = useTheme();
  const body = (
    <View
      style={{
        minHeight: theme.tap.base,
        flexDirection: "row",
        alignItems: "center",
        gap: theme.space[3],
        paddingHorizontal: theme.space[3],
        paddingVertical: theme.space[2],
        borderRadius: theme.radius.md,
        borderWidth: 1,
        borderColor: theme.colors.border,
        backgroundColor: theme.colors.surface,
      }}
    >
      <FileText size={20} color={theme.colors.primaryStrong} strokeWidth={2} />
      <Text role="label" numberOfLines={1} style={{ flex: 1 }}>
        {name}
      </Text>
      <Text role="caption" tone="muted">
        {attachmentExtension(name)}
      </Text>
      {trailing}
    </View>
  );
  return onPress ? (
    <Pressable accessibilityRole="link" accessibilityLabel={`Open ${name}`} onPress={onPress}>
      {body}
    </Pressable>
  ) : (
    body
  );
}

