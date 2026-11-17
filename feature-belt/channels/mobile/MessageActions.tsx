import {
  CHANNELS_QUICK_REACTIONS,
  CHANNELS_REMIND_OPTIONS,
  channelsMessagePath,
  channelsWhen,
  channelsPlainText,
  errorMessage,
  refreshChannelsAfterBlock,
  useChannelsDelete,
  useChannelsPin,
  useChannelsReact,
  useChannelsSaveLater,
  useChannelsMarkUnread,
  useChannelsHidePreviews,
  useCreateChannelsReminder,
  useQueryClient,
  type ChannelsCachedMessage,
  type ChannelsDetail,
} from "@scaffoldapp/shared";
import { Button, ConfirmDialog, Field, Input, ListGroup, ListRow, Sheet, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { useRouter, type Href } from "expo-router";
import { AlarmClock, Bookmark, BookmarkMinus, EyeOff, Flag, Forward, Link2, MailOpen, MessageSquareText, Pencil, Pin, PinOff, Share2, Trash2 } from "lucide-react-native";
import { useRef, useState, type ReactNode } from "react";
import { Pressable, Share, View } from "react-native";
import { ReportDialog } from "@/features/moderation";
import { WEB_URL } from "@/lib/env";

type Pending = { kind: "report" | "delete" | "remind"; message: ChannelsCachedMessage } | null;

export interface MessageActionsOptions {
  channel: ChannelsDetail;
  meId: string | undefined;
  /** Reply in thread (timelines only). */
  onReplyInThread?: (m: ChannelsCachedMessage) => void;
  onEdit: (m: ChannelsCachedMessage) => void;
}

/**
 * The long-press actions sheet for messages: quick reactions, reply in thread, share,
 * pin, save for later, edit, delete and report. Returns `open(message)` and the element
 * to render once in the screen.
 */
export function useMessageActions({ channel, meId, onReplyInThread, onEdit }: MessageActionsOptions): { open: (m: ChannelsCachedMessage) => void; element: ReactNode } {
  const theme = useTheme();
  const col = theme.colors;
  const s = theme.space;
  const qc = useQueryClient();
  const react = useChannelsReact();
  const pin = useChannelsPin();
  const later = useChannelsSaveLater(channel.org_id);
  const remove = useChannelsDelete();
  const router = useRouter();
  const remind = useCreateChannelsReminder(channel.org_id);
  const markUnread = useChannelsMarkUnread(channel.org_id);
  const hidePreviews = useChannelsHidePreviews();
  const [when, setWhen] = useState("");
  const [m, setM] = useState<ChannelsCachedMessage | null>(null);
  const [next, setNext] = useState<Pending>(null);
  const afterSheet = useRef<(() => void) | null>(null);

  const own = !!m?.sender && m.sender.id === meId;
  const close = (then?: () => void) => {
    afterSheet.current = then ?? null;
    setM(null);
  };
  const link = m ? `${WEB_URL}${channelsMessagePath(m)}` : "";

  const element = (
    <>
      <Sheet
        open={m !== null}
        onClose={() => close()}
        onDismissed={() => {
          const run = afterSheet.current;
          afterSheet.current = null;
          run?.();
        }}
      >
        {m ? (
          <View style={{ gap: s[4] }}>
            {channel.member && !channel.archived ? (
              <View style={{ flexDirection: "row", justifyContent: "space-between" }} accessibilityLabel="Quick reactions">
                {CHANNELS_QUICK_REACTIONS.map((e) => (
                  <Pressable
                    key={e}
                    accessibilityRole="button"
                    accessibilityLabel={`React with ${e}`}
                    onPress={() => {
                      react.mutate({ message: m, emoji: e }, { onError: (err) => toast.error(errorMessage(err)) });
                      close();
                    }}
                    style={({ pressed }) => ({ width: theme.tap.base, height: theme.tap.base, alignItems: "center", justifyContent: "center", borderRadius: theme.radius.pill, backgroundColor: pressed ? col.sunken : "transparent" })}
                  >
                    <Text role="title">{e}</Text>
                  </Pressable>
                ))}
              </View>
            ) : null}
            <ListGroup>
              {onReplyInThread && !m.thread_root_id ? (
                <ListRow title="Reply in thread" leading={<MessageSquareText color={col.textMuted} size={20} />} onPress={() => close(() => onReplyInThread(m))} />
              ) : null}
              {WEB_URL ? (
                <ListRow title="Share link" leading={<Link2 color={col.textMuted} size={20} />} onPress={() => close(() => void Share.share({ message: link }))} />
              ) : null}
              {m.body ? (
                <ListRow
                  title="Share text"
                  leading={<Share2 color={col.textMuted} size={20} />}
                  onPress={() => close(() => void Share.share({ message: channelsPlainText(m.body, m.mentions) }))}
                />
              ) : null}
              {channel.member && !channel.archived ? (
                <ListRow
                  title={m.pinned ? "Unpin" : "Pin to channel"}
                  leading={m.pinned ? <PinOff color={col.textMuted} size={20} /> : <Pin color={col.textMuted} size={20} />}
                  onPress={() => {
                    pin.mutate({ message: m, pinned: !m.pinned }, { onError: (err) => toast.error(errorMessage(err)) });
                    close();
                  }}
                />
              ) : null}
              <ListRow
                title={m.saved ? "Remove from Later" : "Save for later"}
                leading={m.saved ? <BookmarkMinus color={col.textMuted} size={20} /> : <Bookmark color={col.textMuted} size={20} />}
                onPress={() => {
                  later.mutate(
                    { message: m, saved: !m.saved },
                    { onSuccess: () => toast.success(m.saved ? "Removed from Later." : "Saved for later."), onError: (err) => toast.error(errorMessage(err)) },
                  );
                  close();
                }}
              />
              <ListRow title="Remind me about this" leading={<AlarmClock color={col.textMuted} size={20} />} onPress={() => close(() => (setWhen(""), setNext({ kind: "remind", message: m })))} />
              <ListRow
                title="Mark unread"
                leading={<MailOpen color={col.textMuted} size={20} />}
                onPress={() => {
                  markUnread.mutate(m, { onSuccess: () => toast.success("Marked unread."), onError: (err) => toast.error(errorMessage(err)) });
                  close();
                }}
              />
              {channel.member && !channel.archived ? (
                <ListRow title="Forward" leading={<Forward color={col.textMuted} size={20} />} onPress={() => close(() => router.push(`/channels/forward/${m.id}` as Href))} />
              ) : null}
              {own && m.links.some((l) => l.kind === "external") && !m.links_hidden ? (
                <ListRow
                  title="Remove previews"
                  leading={<EyeOff color={col.textMuted} size={20} />}
                  onPress={() => {
                    hidePreviews.mutate(m, { onError: (err) => toast.error(errorMessage(err)) });
                    close();
                  }}
                />
              ) : null}
              {own && m.kind === "text" && channel.member && !channel.archived ? (
                <ListRow title="Edit" leading={<Pencil color={col.textMuted} size={20} />} onPress={() => close(() => onEdit(m))} />
              ) : null}
              {!own && m.sender ? <ListRow title="Report" leading={<Flag color={col.textMuted} size={20} />} onPress={() => close(() => setNext({ kind: "report", message: m }))} /> : null}
              {own || channel.permissions.edit ? (
                <ListRow
                  title="Delete for everyone"
                  destructive
                  leading={<Trash2 color={col.dangerStrong} size={20} />}
                  onPress={() => close(() => setNext({ kind: "delete", message: m }))}
                />
              ) : null}
            </ListGroup>
          </View>
        ) : null}
      </Sheet>
      <Sheet open={next?.kind === "remind"} onClose={() => setNext(null)} title="Remind me about this">
        {next?.kind === "remind" ? (
          <View style={{ gap: s[4] }}>
            <ListGroup>
              {CHANNELS_REMIND_OPTIONS.map((o) => (
                <ListRow
                  key={o.when}
                  title={o.label}
                  onPress={() => {
                    remind.mutate({ message_id: next.message.id, when: o.when }, { onSuccess: (r) => toast.success(`I'll remind you ${channelsWhen(r.remind_at)}.`), onError: (err) => toast.error(errorMessage(err)) });
                    setNext(null);
                  }}
                />
              ))}
            </ListGroup>
            <Field label="Or when" info='Try "in 2 hours", "tomorrow at 9am", "next week" or "2026-12-01 10am".'>
              <Input value={when} onChangeText={setWhen} placeholder="friday at 3pm" autoCapitalize="none" />
            </Field>
            <Button
              disabled={!when.trim()}
              loading={remind.isPending}
              onPress={() =>
                remind.mutate(
                  { message_id: next.message.id, when: when.trim() },
                  {
                    onSuccess: (r) => {
                      toast.success(`I'll remind you ${channelsWhen(r.remind_at)}.`);
                      setNext(null);
                    },
                    onError: (err) => toast.error(errorMessage(err)),
                  },
                )
              }
            >
              Set reminder
            </Button>
          </View>
        ) : null}
      </Sheet>
      <ConfirmDialog
        open={next?.kind === "delete"}
        onClose={() => setNext(null)}
        title="Delete this message for everyone?"
        confirmLabel="Delete"
        tone="danger"
        onConfirm={() => (next ? remove.mutateAsync(next.message) : undefined)}
      >
        {next && next.message.sender?.id !== meId ? "You're deleting someone else's message as a channel admin." : undefined}
      </ConfirmDialog>
      {next?.kind === "report" && next.message.sender ? (
        <ReportDialog
          open
          onClose={() => setNext(null)}
          targetKind="channels.message"
          targetId={next.message.id}
          noun="message"
          author={next.message.sender.deleted ? null : { id: next.message.sender.id, name: next.message.sender.display_name }}
          onReported={(r) => r.blocked && void refreshChannelsAfterBlock(qc)}
        />
      ) : null}
    </>
  );
  return { open: setM, element };
}
