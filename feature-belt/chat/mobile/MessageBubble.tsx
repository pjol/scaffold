import { chatMessageTime, formatFileSize, systemEventText, useChatAttachmentUrl, type ChatAttachment, type ChatThreadMessage } from "@scaffoldapp/shared";
import { Avatar, Button, Spinner, Text, haptic, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { FileText, Film, Music } from "lucide-react-native";
import { memo, useState } from "react";
import { Linking, Pressable, View } from "react-native";

export interface BubbleProps {
  message: ChatThreadMessage;
  meId: string | undefined;
  group: boolean;
  grouped: boolean;
  status?: string | undefined;
  highlighted: boolean;
  dayLabel?: string | undefined;
  canInteract: boolean;
  onLongPress: (m: ChatThreadMessage) => void;
  onReact: (m: ChatThreadMessage, emoji: string) => void;
  onRetry: (m: ChatThreadMessage) => void;
  onDiscard: (m: ChatThreadMessage) => void;
  onOpenImage: (m: ChatThreadMessage, attachmentId: string) => void;
  onJumpTo: (messageId: string) => void;
}

function useSrc(a: ChatAttachment): { src: string; onError: () => void } {
  const [failed, setFailed] = useState(false);
  const fresh = useChatAttachmentUrl(a, failed && !a.id.startsWith("local-"));
  return { src: fresh.data?.url ?? a.url, onError: () => setFailed(true) };
}

function ImageTile({ a, size, pending, onPress }: { a: ChatAttachment; size: number; pending: boolean; onPress: () => void }) {
  const theme = useTheme();
  const { src, onError } = useSrc(a);
  const ratio = a.width && a.height ? a.width / a.height : 1;
  const width = size;
  const height = Math.min(size * 1.25, Math.max(size * 0.6, size / ratio));
  return (
    <Pressable accessibilityRole="imagebutton" accessibilityLabel={a.file_name || "Photo"} onPress={onPress} disabled={pending}>
      <Image source={{ uri: src }} onError={onError} contentFit="cover" style={{ width, height, borderRadius: theme.radius.md, backgroundColor: theme.colors.sunken, opacity: pending ? 0.6 : 1 }} />
      {pending ? (
        <View style={{ position: "absolute", inset: 0, alignItems: "center", justifyContent: "center" }}>
          <Spinner label="Uploading" />
        </View>
      ) : null}
    </Pressable>
  );
}

function FileTile({ a, own, pending }: { a: ChatAttachment; own: boolean; pending: boolean }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const { src } = useSrc(a);
  const Icon = a.kind === "video" ? Film : a.kind === "audio" ? Music : FileText;
  const fg = own ? c.primaryFg : c.text;
  return (
    <Pressable
      accessibilityRole="link"
      accessibilityLabel={`Open ${a.file_name || "file"}`}
      disabled={pending || !src}
      onPress={() => void Linking.openURL(src)}
      style={{ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.sm, paddingHorizontal: s[3], paddingVertical: s[2], borderRadius: theme.radius.md, backgroundColor: own ? c.primaryStrong : c.surface }}
    >
      <Icon color={fg} size={20} />
      <View style={{ flex: 1, minWidth: 0 }}>
        <Text role="label" numberOfLines={1} style={{ color: fg }}>
          {a.file_name || (a.kind === "video" ? "Video" : a.kind === "audio" ? "Audio" : "File")}
        </Text>
        {a.size_bytes ? (
          <Text role="caption" style={{ color: fg, opacity: 0.8 }}>
            {formatFileSize(a.size_bytes)}
          </Text>
        ) : null}
      </View>
      {pending ? <Spinner /> : null}
    </Pressable>
  );
}

/** One message: bubble, quote, files, reactions, status. Long-press opens the actions sheet. */
export const MessageBubble = memo(function MessageBubble(p: BubbleProps) {
  const { message: m, meId, group, grouped, status, highlighted, dayLabel, canInteract } = p;
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const own = m.sender.id === meId;
  const pending = m.status === "sending";
  const failed = m.status === "failed";
  const live = !m.deleted && !m.status;
  const showAvatar = group && !own;
  const fg = m.deleted ? c.textMuted : own ? c.primaryFg : c.text;
  const images = m.attachments.filter((a) => a.kind === "image");
  const others = m.attachments.filter((a) => a.kind !== "image");

  const day = dayLabel ? (
    <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], paddingTop: s[6], paddingBottom: s[2] }} accessibilityRole="header">
      <View style={{ flex: 1, height: 1, backgroundColor: c.border }} />
      <Text role="caption" weight={600} tone="muted">
        {dayLabel}
      </Text>
      <View style={{ flex: 1, height: 1, backgroundColor: c.border }} />
    </View>
  ) : null;

  if (m.kind === "system") {
    return (
      <View>
        {day}
        <Text role="caption" tone="muted" align="center" style={{ paddingVertical: s[2] }}>
          {systemEventText(m, meId)}
        </Text>
      </View>
    );
  }

  return (
    <View>
      {day}
      <View style={{ flexDirection: own ? "row-reverse" : "row", gap: s[2], paddingTop: grouped ? s[1] : s[4] }}>
        {showAvatar ? grouped ? <View style={{ width: 32 }} /> : <Avatar name={m.sender.display_name} src={m.sender.avatar_url} size="sm" /> : null}
        <View style={{ maxWidth: "80%", alignItems: own ? "flex-end" : "flex-start", gap: s[1] }}>
          {showAvatar && !grouped ? (
            <Text role="caption" tone="muted" style={{ paddingHorizontal: s[3] }}>
              {m.sender.display_name}
            </Text>
          ) : null}
          <Pressable
            accessibilityRole="text"
            accessibilityHint={live && canInteract ? "Double tap and hold for actions" : undefined}
            onLongPress={() => {
              if (!live || !canInteract) return;
              haptic.medium();
              p.onLongPress(m);
            }}
            delayLongPress={300}
            style={{
              gap: s[2],
              paddingHorizontal: s[3],
              paddingVertical: s[2],
              borderRadius: theme.radius.lg,
              backgroundColor: m.deleted ? "transparent" : own ? c.primary : c.sunken,
              borderWidth: m.deleted || highlighted ? 2 : 0,
              borderColor: highlighted ? c.ring : c.border,
              opacity: pending || failed ? 0.7 : 1,
            }}
          >
            {m.deleted ? (
              <Text role="body" style={{ color: fg, fontStyle: "italic" }}>
                Message deleted
              </Text>
            ) : (
              <>
                {m.reply_to ? (
                  <Pressable
                    accessibilityRole="button"
                    accessibilityLabel={`Reply to ${m.reply_to.sender.display_name}. Show message`}
                    onPress={() => m.reply_to && p.onJumpTo(m.reply_to.id)}
                    style={{ borderRadius: theme.radius.md, paddingHorizontal: s[3], paddingVertical: s[1.5], backgroundColor: own ? c.primaryStrong : c.surface }}
                  >
                    <Text role="caption" weight={600} numberOfLines={1} style={{ color: fg }}>
                      {m.reply_to.sender.display_name}
                    </Text>
                    <Text role="label" numberOfLines={2} style={{ color: fg, fontStyle: m.reply_to.deleted ? "italic" : "normal" }}>
                      {m.reply_to.deleted ? "Message deleted" : m.reply_to.body || (m.reply_to.has_attachments ? "Attachment" : "")}
                    </Text>
                  </Pressable>
                ) : null}
                {images.length > 0 ? (
                  <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[1], maxWidth: 240 }}>
                    {images.map((a) => (
                      <ImageTile key={a.id} a={a} size={images.length === 1 ? 240 : 118} pending={pending} onPress={() => p.onOpenImage(m, a.id)} />
                    ))}
                  </View>
                ) : null}
                {others.map((a) => (
                  <FileTile key={a.id} a={a} own={own} pending={pending} />
                ))}
                {m.body ? (
                  <Text role="body" selectable style={{ color: fg }}>
                    {m.body}
                  </Text>
                ) : null}
              </>
            )}
            <Text role="caption" align="right" style={{ color: fg, opacity: own && !m.deleted ? 0.8 : 1 }}>
              {m.edited_at && !m.deleted ? "Edited · " : ""}
              {chatMessageTime(m.created_at)}
            </Text>
          </Pressable>
          {m.reactions.length > 0 && !m.deleted ? (
            <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[1], justifyContent: own ? "flex-end" : "flex-start" }}>
              {m.reactions.map((r) => {
                const mine = !!meId && r.user_ids.includes(meId);
                return (
                  <Pressable
                    key={r.emoji}
                    accessibilityRole="button"
                    accessibilityState={{ selected: mine, disabled: !canInteract }}
                    accessibilityLabel={`${r.emoji} ${r.count}`}
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
          ) : status ? (
            <Text role="caption" tone="muted">
              {status}
            </Text>
          ) : null}
        </View>
      </View>
    </View>
  );
});
