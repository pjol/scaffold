import {
  channelsAuthorName,
  channelsFileSize,
  channelsReplyCount,
  channelsShortTime,
  channelsSystemText,
  channelsTime,
  useChannelsAttachmentUrl,
  type ChannelsAttachment,
  type ChannelsCachedMessage,
} from "@scaffoldapp/shared";
import { Avatar, AvatarGroup, Badge, Button, Spinner, Text, haptic, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { Bot, FileText, Film, Music, Pin } from "lucide-react-native";
import { memo, useState } from "react";
import { Linking, Pressable, View } from "react-native";
import { ChannelsMarkdown } from "./Markdown";

export interface MessageRowProps {
  message: ChannelsCachedMessage;
  meId: string | undefined;
  /** Same author as the message above: no avatar or name. */
  grouped: boolean;
  /** Day separator above this message ("Today"). */
  dayLabel?: string | undefined;
  /** Show the "New" divider above this message. */
  firstUnread?: boolean;
  highlighted: boolean;
  canInteract: boolean;
  /** Show the "N replies" row (timelines; not inside the thread itself). */
  showThread: boolean;
  onLongPress: (m: ChannelsCachedMessage) => void;
  onReact: (m: ChannelsCachedMessage, emoji: string) => void;
  onOpenThread: (m: ChannelsCachedMessage) => void;
  onRetry: (m: ChannelsCachedMessage) => void;
  onDiscard: (m: ChannelsCachedMessage) => void;
  onOpenImage: (m: ChannelsCachedMessage, attachmentId: string) => void;
}

function useSrc(a: ChannelsAttachment): { src: string; onError: () => void } {
  const [failed, setFailed] = useState(false);
  const fresh = useChannelsAttachmentUrl(a.id, failed && !a.id.startsWith("local-"));
  return { src: fresh.data?.url ?? a.url, onError: () => setFailed(true) };
}

function ImageTile({ a, size, pending, onPress }: { a: ChannelsAttachment; size: number; pending: boolean; onPress: () => void }) {
  const theme = useTheme();
  const { src, onError } = useSrc(a);
  const ratio = a.width && a.height ? a.width / a.height : 1;
  const height = Math.min(size * 1.25, Math.max(size * 0.6, size / ratio));
  return (
    <Pressable accessibilityRole="imagebutton" accessibilityLabel={a.file_name || "Photo"} onPress={onPress} disabled={pending}>
      <Image source={{ uri: src }} onError={onError} contentFit="cover" style={{ width: size, height, borderRadius: theme.radius.md, backgroundColor: theme.colors.sunken, opacity: pending ? 0.6 : 1 }} />
      {pending ? (
        <View style={{ position: "absolute", top: 0, left: 0, right: 0, bottom: 0, alignItems: "center", justifyContent: "center" }}>
          <Spinner label="Uploading" />
        </View>
      ) : null}
    </Pressable>
  );
}

function FileTile({ a, pending }: { a: ChannelsAttachment; pending: boolean }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const { src } = useSrc(a);
  const Icon = a.kind === "video" ? Film : a.kind === "audio" ? Music : FileText;
  return (
    <Pressable
      accessibilityRole="link"
      accessibilityLabel={`Open ${a.file_name || "file"}`}
      disabled={pending || !src}
      onPress={() => void Linking.openURL(src)}
      style={({ pressed }) => ({
        flexDirection: "row",
        alignItems: "center",
        gap: s[3],
        minHeight: theme.tap.base,
        maxWidth: 320,
        paddingHorizontal: s[3],
        paddingVertical: s[2],
        borderRadius: theme.radius.md,
        borderWidth: 1,
        borderColor: c.border,
        backgroundColor: pressed ? c.sunken : c.surface,
      })}
    >
      <Icon color={c.textMuted} size={20} />
      <View style={{ flex: 1, minWidth: 0 }}>
        <Text role="label" numberOfLines={1}>
          {a.file_name || (a.kind === "video" ? "Video" : a.kind === "audio" ? "Audio" : "File")}
        </Text>
        {a.size_bytes ? (
          <Text role="caption" tone="muted">
            {channelsFileSize(a.size_bytes)}
          </Text>
        ) : null}
      </View>
      {pending ? <Spinner /> : null}
    </Pressable>
  );
}

/** A message's files: photos in a row, everything else as file rows. */
export function ChannelsAttachments({ message: m, onOpenImage }: { message: ChannelsCachedMessage; onOpenImage: (id: string) => void }) {
  const s = useTheme().space;
  const pending = m.status === "sending";
  const images = m.attachments.filter((a) => a.kind === "image");
  const others = m.attachments.filter((a) => a.kind !== "image");
  return (
    <View style={{ gap: s[2] }}>
      {images.length > 0 ? (
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[1], maxWidth: 260 }}>
          {images.map((a) => (
            <ImageTile key={a.id} a={a} size={images.length === 1 ? 260 : 128} pending={pending} onPress={() => onOpenImage(a.id)} />
          ))}
        </View>
      ) : null}
      {others.map((a) => (
        <FileTile key={a.id} a={a} pending={pending} />
      ))}
    </View>
  );
}

/** The day separator and the "New" divider. */
function Divider({ label, tone }: { label: string; tone: "muted" | "danger" }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const line = tone === "danger" ? c.danger : c.border;
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], paddingTop: s[6], paddingBottom: s[2] }} accessibilityRole="header">
      <View style={{ flex: 1, height: 1, backgroundColor: line }} />
      <Text role="caption" weight={600} tone={tone}>
        {label}
      </Text>
      <View style={{ flex: 1, height: 1, backgroundColor: line }} />
    </View>
  );
}

/**
 * One message, Slack-style: avatar and name (unless grouped), time, the body, files,
 * reactions and the thread summary. Long-press opens the actions sheet.
 */
export const MessageRow = memo(function MessageRow(p: MessageRowProps) {
  const { message: m, meId, grouped, dayLabel, firstUnread, highlighted, canInteract, showThread } = p;
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const pending = m.status === "sending";
  const failed = m.status === "failed";
  const live = !m.deleted && !m.status;
  const author = channelsAuthorName(m);

  const dividers = (
    <>
      {dayLabel ? <Divider label={dayLabel} tone="muted" /> : null}
      {firstUnread ? <Divider label="New" tone="danger" /> : null}
    </>
  );

  if (m.kind === "system") {
    return (
      <View>
        {dividers}
        <Text role="caption" tone="muted" align="center" style={{ paddingVertical: s[2] }}>
          {channelsSystemText(m, meId)}
        </Text>
      </View>
    );
  }

  return (
    <View>
      {dividers}
      <Pressable
        accessibilityHint={live && canInteract ? "Double tap and hold for actions" : undefined}
        onLongPress={() => {
          if (!live || !canInteract) return;
          haptic.medium();
          p.onLongPress(m);
        }}
        delayLongPress={300}
        style={({ pressed }) => ({
          flexDirection: "row",
          gap: s[3],
          paddingTop: grouped ? s[1] : s[3],
          paddingBottom: s[1],
          paddingHorizontal: s[2],
          marginHorizontal: -s[2],
          borderRadius: theme.radius.md,
          backgroundColor: highlighted ? c.warningSoft : pressed && live && canInteract ? c.sunken : m.pinned ? c.primarySoft : "transparent",
          opacity: pending || failed ? 0.7 : 1,
        })}
      >
        <View style={{ width: s[8], alignItems: "center" }}>
          {grouped ? null : m.kind === "bot" ? (
            <View style={{ width: s[8], height: s[8], borderRadius: theme.radius.md, alignItems: "center", justifyContent: "center", backgroundColor: c.accentSoft }}>
              <Bot color={c.accentStrong} size={20} />
            </View>
          ) : (
            <Avatar name={author} src={m.sender?.avatar_url ?? null} size="sm" />
          )}
        </View>
        <View style={{ flex: 1, minWidth: 0, gap: s[1] }}>
          {!grouped ? (
            <View style={{ flexDirection: "row", alignItems: "baseline", gap: s[2], flexWrap: "wrap" }}>
              <Text role="body" weight={700} numberOfLines={1} style={{ flexShrink: 1 }}>
                {author}
              </Text>
              {m.kind === "bot" ? (
                <Badge size="sm" tone="accent">
                  App
                </Badge>
              ) : null}
              <Text role="caption" tone="muted">
                {channelsTime(m.created_at)}
              </Text>
              {m.pinned ? <Pin color={c.primaryStrong} size={12} accessibilityLabel="Pinned" /> : null}
            </View>
          ) : null}
          {m.title ? (
            <Text role="titleSm" style={{ color: m.deleted ? c.textMuted : c.text }}>
              {m.title}
            </Text>
          ) : null}
          {m.deleted ? (
            <Text role="body" tone="muted" style={{ fontStyle: "italic" }}>
              {m.deleted_by === "staff" ? "Removed by a moderator" : "Message deleted"}
            </Text>
          ) : (
            <>
              {m.body ? <ChannelsMarkdown body={m.body} mentions={m.mentions} meId={meId} /> : null}
              {m.edited_at ? (
                <Text role="caption" tone="muted">
                  Edited
                </Text>
              ) : null}
              {m.attachments.length > 0 ? <ChannelsAttachments message={m} onOpenImage={(id) => p.onOpenImage(m, id)} /> : null}
            </>
          )}
          {m.reactions.length > 0 && !m.deleted ? (
            <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[1] }}>
              {m.reactions.map((r) => {
                const mine = !!meId && r.user_ids.includes(meId);
                return (
                  <Pressable
                    key={r.emoji}
                    accessibilityRole="button"
                    accessibilityState={{ selected: mine, disabled: !canInteract }}
                    accessibilityLabel={`${r.emoji} ${r.count}${mine ? ", including you" : ""}`}
                    disabled={!canInteract}
                    onPress={() => p.onReact(m, r.emoji)}
                    hitSlop={s[1]}
                    style={{
                      flexDirection: "row",
                      alignItems: "center",
                      gap: s[1],
                      minHeight: theme.tap.sm,
                      paddingHorizontal: s[2],
                      borderRadius: theme.radius.pill,
                      borderWidth: 1,
                      borderColor: mine ? c.primary : c.border,
                      backgroundColor: mine ? c.primarySoft : c.surface,
                    }}
                  >
                    <Text role="label">{r.emoji}</Text>
                    <Text role="label" tabular style={{ color: mine ? c.primaryStrong : c.text }}>
                      {r.count}
                    </Text>
                  </Pressable>
                );
              })}
            </View>
          ) : null}
          {showThread && m.reply_count > 0 ? (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={`${channelsReplyCount(m.reply_count)}. Open thread`}
              onPress={() => p.onOpenThread(m)}
              style={({ pressed }) => ({
                flexDirection: "row",
                alignItems: "center",
                gap: s[2],
                minHeight: theme.tap.base,
                alignSelf: "flex-start",
                paddingHorizontal: s[2],
                marginLeft: -s[2],
                borderRadius: theme.radius.md,
                backgroundColor: pressed ? c.sunken : "transparent",
              })}
            >
              {m.reply_users.length > 0 ? <AvatarGroup people={m.reply_users.map((u) => ({ name: u.display_name, src: u.avatar_url }))} size="xs" max={4} /> : null}
              <Text role="label" weight={600} tone="primary">
                {channelsReplyCount(m.reply_count)}
              </Text>
              {m.last_reply_at ? (
                <Text role="caption" tone="muted">
                  {channelsShortTime(m.last_reply_at)}
                </Text>
              ) : null}
            </Pressable>
          ) : null}
          {failed ? (
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }} accessibilityLiveRegion="polite">
              <Text role="caption" tone="danger">
                Not sent.
              </Text>
              <Button size="md" variant="ghost" onPress={() => p.onRetry(m)}>
                Retry
              </Button>
              <Button size="md" variant="ghost" onPress={() => p.onDiscard(m)}>
                Delete
              </Button>
            </View>
          ) : pending ? (
            <Text role="caption" tone="muted">
              Sending…
            </Text>
          ) : null}
        </View>
      </Pressable>
    </View>
  );
});
