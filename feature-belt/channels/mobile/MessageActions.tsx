import {
  CHANNELS_QUICK_REACTIONS,
  channelsMessagePath,
  channelsPlainText,
  errorMessage,
  refreshChannelsAfterBlock,
  useChannelsDelete,
  useChannelsPin,
  useChannelsReact,
  useChannelsSaveLater,
  useQueryClient,
  type ChannelsCachedMessage,
  type ChannelsDetail,
} from "@scaffoldapp/shared";
import { ConfirmDialog, ListGroup, ListRow, Sheet, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Bookmark, BookmarkMinus, Flag, Link2, MessageSquareText, Pencil, Pin, PinOff, Share2, Trash2 } from "lucide-react-native";
import { useRef, useState, type ReactNode } from "react";
import { Pressable, Share, View } from "react-native";
import { ReportDialog } from "@/features/moderation";
import { WEB_URL } from "@/lib/env";

type Pending = { kind: "report" | "delete"; message: ChannelsCachedMessage } | null;

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
            {channel.permissions.post ? (
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
              {channel.permissions.post ? (
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
              {own && m.kind === "text" && channel.permissions.post ? (
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
