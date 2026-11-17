"use client";

import {
  channelsMessagePath,
  channelsWhen,
  errorMessage,
  refreshChannelsAfterBlock,
  useChannelsHidePreviews,
  useChannelsMarkUnread,
  useCreateChannelsReminder,
  useChannelsDelete,
  useChannelsPin,
  useChannelsReact,
  useChannelsSaveLater,
  useChannelsSend,
  useQueryClient,
  type ChannelsCachedMessage,
  type ChannelsDetail,
} from "@scaffoldapp/shared";
import { ConfirmDialog, PhotoLightbox, toast, type GalleryPhoto } from "@scaffoldapp/ui-web";
import { useMemo, useState, type ReactNode } from "react";
import { ReportDialog } from "@/features/moderation";
import { ForwardDialog, PickTimeDialog, ProfileCardDialog } from "./ActionDialogs";
import { EmojiPickerDialog } from "./EmojiPicker";
import type { MessageActions } from "./MessageItem";

/**
 * Message actions shared by the timeline and the thread pane, with the dialogs they open
 * (delete, report, emoji, photos). Render `dialogs` once beside the list.
 */
export function useMessageActions({
  channel,
  onOpenThread,
  onEdit,
}: {
  channel: ChannelsDetail;
  onOpenThread: (rootId: string, messageId?: string) => void;
  onEdit: (m: ChannelsCachedMessage) => void;
}): { actions: MessageActions; dialogs: ReactNode } {
  const qc = useQueryClient();
  const { mutate: react } = useChannelsReact();
  const { mutate: pin } = useChannelsPin();
  const { mutate: save } = useChannelsSaveLater(channel.org_id);
  const remove = useChannelsDelete();
  const sender = useChannelsSend(channel.id);
  const remind = useCreateChannelsReminder(channel.org_id);
  const { mutate: markUnread } = useChannelsMarkUnread(channel.org_id);
  const { mutate: hidePreviews } = useChannelsHidePreviews();
  const [remindAt, setRemindAt] = useState<ChannelsCachedMessage | null>(null);
  const [forwarding, setForwarding] = useState<ChannelsCachedMessage | null>(null);
  const [profile, setProfile] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<ChannelsCachedMessage | null>(null);
  const [reporting, setReporting] = useState<ChannelsCachedMessage | null>(null);
  const [emojiFor, setEmojiFor] = useState<ChannelsCachedMessage | null>(null);
  const [lightbox, setLightbox] = useState<{ photos: GalleryPhoto[]; index: number } | null>(null);

  const actions: MessageActions = useMemo(
    () => ({
      onReplyInThread: (m) => onOpenThread(m.id),
      onEdit,
      onDelete: setDeleting,
      onReport: setReporting,
      onReact: (m, emoji) => react({ message: m, emoji }, { onError: (e) => toast.error(errorMessage(e)) }),
      onPickEmoji: setEmojiFor,
      onTogglePin: (m) =>
        pin({ message: m, pinned: !m.pinned }, { onSuccess: () => toast.success(m.pinned ? "Unpinned." : "Pinned to the channel."), onError: (e) => toast.error(errorMessage(e)) }),
      onToggleSave: (m) =>
        save({ message: m, saved: !m.saved }, { onSuccess: () => toast.success(m.saved ? "Removed from Later." : "Saved for later."), onError: (e) => toast.error(errorMessage(e)) }),
      onCopyLink: (m) => {
        void navigator.clipboard?.writeText(`${window.location.origin}${channelsMessagePath(m)}`).then(() => toast.success("Link copied."));
      },
      onRetry: sender.retry,
      onDiscard: sender.discard,
      onOpenImage: (m, attachmentId) => {
        const photos = m.attachments.filter((a) => a.kind === "image").map((a) => ({ id: a.id, url: a.url, caption: a.file_name || null, created_at: m.created_at }));
        setLightbox({ photos, index: Math.max(0, photos.findIndex((p) => p.id === attachmentId)) });
      },
      onOpenThread,
      onRemind: (m, when) => {
        if (when === "custom") {
          setRemindAt(m);
          return;
        }
        remind.mutate({ message_id: m.id, when }, { onSuccess: (r) => toast.success(`I'll remind you ${channelsWhen(r.remind_at)}.`), onError: (e) => toast.error(errorMessage(e)) });
      },
      onMarkUnread: (m) => markUnread(m, { onSuccess: () => toast.success("Marked unread."), onError: (e) => toast.error(errorMessage(e)) }),
      onForward: setForwarding,
      onHidePreviews: (m) => hidePreviews(m, { onError: (e) => toast.error(errorMessage(e)) }),
      onOpenProfile: setProfile,
    }),
    [react, pin, save, sender.retry, sender.discard, onOpenThread, onEdit, remind, markUnread, hidePreviews],
  );

  const dialogs = (
    <>
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={deleting?.title !== null && deleting?.title !== undefined ? "Delete this post for everyone?" : "Delete this message for everyone?"}
        confirmLabel="Delete"
        tone="danger"
        onConfirm={() => (deleting ? remove.mutateAsync(deleting) : undefined)}
        errorText={(e) => errorMessage(e)}
      >
        {deleting && deleting.reply_count > 0 ? "Its replies stay in the thread." : undefined}
      </ConfirmDialog>
      {reporting && (
        <ReportDialog
          open
          onOpenChange={(o) => !o && setReporting(null)}
          targetKind="channels.message"
          targetId={reporting.id}
          noun="message"
          author={reporting.sender && !reporting.sender.deleted ? { id: reporting.sender.id, name: reporting.sender.display_name } : null}
          onReported={(r) => r.blocked && void refreshChannelsAfterBlock(qc)}
        />
      )}
      <EmojiPickerDialog
        open={emojiFor !== null}
        onOpenChange={(o) => !o && setEmojiFor(null)}
        onPick={(emoji) => {
          if (emojiFor) actions.onReact(emojiFor, emoji);
          setEmojiFor(null);
        }}
      />
      {lightbox && <PhotoLightbox photos={lightbox.photos} open onOpenChange={(o) => !o && setLightbox(null)} defaultIndex={lightbox.index} />}
      <PickTimeDialog
        open={remindAt !== null}
        onOpenChange={(o) => !o && setRemindAt(null)}
        title="Remind me about this"
        confirmLabel="Set reminder"
        onPick={(at) =>
          !remindAt
            ? undefined
            : remind.mutateAsync({ message_id: remindAt.id, at: at.toISOString() }).then(
            (r) => toast.success(`I'll remind you ${channelsWhen(r.remind_at)}.`),
            (e: unknown) => {
              toast.error(errorMessage(e));
              throw e;
            },
          )
        }
      />
      <ForwardDialog message={forwarding} orgId={channel.org_id} onOpenChange={(o) => !o && setForwarding(null)} />
      <ProfileCardDialog orgId={channel.org_id} userId={profile} onOpenChange={(o) => !o && setProfile(null)} />
    </>
  );
  return { actions, dialogs };
}
