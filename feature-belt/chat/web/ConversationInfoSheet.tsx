"use client";

import {
  chatFullTime,
  CHAT_MUTE_OPTIONS,
  errorMessage,
  muteRequest,
  refreshChatAfterBlock,
  useAddMembers,
  useCreateConversation,
  useIsBlocked,
  useLeaveConversation,
  useMuteConversation,
  useQueryClient,
  useRemoveChatMember,
  useSetMemberRole,
  useUnblockUser,
  useUpdateConversation,
  useUpload,
  type ChatConversationDetail,
  type ChatMember,
  type ChatUser,
} from "@scaffoldapp/shared";
import { Badge, Button, ConfirmDialog, Dialog, Field, IconButton, Input, Menu, MenuItem, MenuSeparator, Select, Sheet, toast } from "@scaffoldapp/ui-web";
import { Ban, Camera, Crown, Flag, LogOut, MessageSquare, MoreHorizontal, ShieldCheck, ShieldMinus, Trash2, UserMinus, UserPlus, UserRoundCheck } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { BlockDialog, ReportDialog } from "@/features/moderation";
import { ConversationAvatar } from "./InboxPanel";
import { PeoplePicker } from "./PeoplePicker";

const ROLE_LABEL = { owner: "Owner", admin: "Admin", member: "" } as const;

type Confirm =
  | { kind: "owner"; member: ChatMember }
  | { kind: "remove"; member: ChatMember }
  | { kind: "leave" }
  | null;

/** Conversation details: name and photo (owner/admin), notifications, members and roles, leave. */
export function ConversationInfoSheet({ conversation: c, open, onOpenChange, meId }: { conversation: ChatConversationDetail; open: boolean; onOpenChange: (open: boolean) => void; meId: string | undefined }) {
  const router = useRouter();
  const qc = useQueryClient();
  const group = c.kind === "group";
  const manage = group && (c.role === "owner" || c.role === "admin");
  const update = useUpdateConversation(c.id);
  const upload = useUpload();
  const mute = useMuteConversation(c.id);
  const addMembers = useAddMembers(c.id);
  const setRole = useSetMemberRole(c.id);
  const removeMember = useRemoveChatMember(c.id);
  const leave = useLeaveConversation();
  const createDm = useCreateConversation();
  const unblock = useUnblockUser();
  const peer = c.peer;
  const blockedByMe = useIsBlocked(peer?.id);
  const [title, setTitle] = useState(c.title);
  const [adding, setAdding] = useState(false);
  const [picked, setPicked] = useState<ChatUser[]>([]);
  const [confirm, setConfirm] = useState<Confirm>(null);
  const [reportUser, setReportUser] = useState<ChatUser | null>(null);
  const [blockUser, setBlockUser] = useState<ChatUser | null>(null);
  const photoRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) setTitle(c.title);
  }, [open, c.title]);

  const muteValue = !c.muted ? "off" : c.muted_until ? "until" : "forever";
  const muteOptions = [
    { value: "off", label: "On" },
    ...(c.muted_until ? [{ value: "until", label: `Off until ${chatFullTime(c.muted_until)}` }] : []),
    ...CHAT_MUTE_OPTIONS.map((o) => ({ value: o.minutes === null ? "forever" : String(o.minutes), label: o.minutes === null ? "Off until I turn them on" : `Off ${o.label.toLowerCase()}` })),
  ];

  const onPhoto = async (file: File | undefined) => {
    if (!file) return;
    try {
      const res = await upload.mutateAsync({ file, purpose: "chat_avatar", maxBytes: 5 * 1024 * 1024 });
      await update.mutateAsync({ avatar_key: res.key });
      toast.success("Group photo updated.");
    } catch (err) {
      toast.error(errorMessage(err));
    }
  };

  const message = (user: ChatUser) =>
    createDm.mutate(
      { kind: "direct", user_id: user.id },
      {
        onSuccess: (d) => {
          onOpenChange(false);
          router.push(`/chat/${d.id}`);
        },
        onError: (err) => toast.error(errorMessage(err)),
      },
    );

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange} title="Details" size="md">
        <div className="flex flex-col gap-8">
          <div className="flex flex-col items-center gap-3">
            <ConversationAvatar conversation={c} size="lg" />
            <p className="text-center type-title-sm text-fg">{c.title}</p>
            {manage && (
              <div className="flex flex-wrap justify-center gap-2">
                <input ref={photoRef} type="file" accept="image/jpeg,image/png,image/webp,image/heic" className="hidden" onChange={(e) => void onPhoto(e.target.files?.[0]).finally(() => (e.target.value = ""))} />
                <Button size="sm" variant="secondary" leftIcon={<Camera />} loading={upload.isPending || (update.isPending && update.variables?.avatar_key !== undefined)} onClick={() => photoRef.current?.click()}>
                  {c.avatar_url ? "Change photo" : "Add photo"}
                </Button>
                {c.avatar_url && (
                  <Button size="sm" variant="ghost" onClick={() => update.mutate({ avatar_key: "" }, { onError: (err) => toast.error(errorMessage(err)) })}>
                    Remove photo
                  </Button>
                )}
              </div>
            )}
          </div>

          {manage && (
            <form
              className="flex items-end gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                update.mutate({ title: title.trim() }, { onSuccess: () => toast.success("Group renamed."), onError: (err) => toast.error(errorMessage(err)) });
              }}
            >
              <Field label="Group name" className="flex-1">
                <Input value={title} maxLength={80} onChange={(e) => setTitle(e.target.value)} />
              </Field>
              <Button type="submit" variant="secondary" disabled={!title.trim() || title.trim() === c.title} loading={update.isPending && update.variables?.title !== undefined}>
                Save
              </Button>
            </form>
          )}

          <Field label="Notifications">
            <Select
              options={muteOptions}
              value={muteValue}
              onValueChange={(v) => {
                if (v === "off") mute.mutate(null);
                else if (v === "forever") mute.mutate({ forever: true });
                else if (v !== "until") mute.mutate(muteRequest(Number(v)));
              }}
            />
          </Field>

          {group && (
            <section className="flex flex-col gap-3">
              <div className="flex min-h-control items-center gap-3">
                <h3 className="flex-1 type-title-sm text-fg">{c.member_count} members</h3>
                {manage && (
                  <Button
                    size="sm"
                    variant="secondary"
                    leftIcon={<UserPlus />}
                    onClick={() => {
                      setPicked([]);
                      setAdding(true);
                    }}
                  >
                    Add people
                  </Button>
                )}
              </div>
              <ul className="flex flex-col" aria-label="Members">
                {c.members.map((m) => {
                  const self = m.user.id === meId;
                  const canRole = c.role === "owner" && !self;
                  const canRemove = !self && m.role !== "owner" && (c.role === "owner" || (c.role === "admin" && m.role === "member"));
                  return (
                    <li key={m.user.id} className="flex min-h-tap-xl items-center gap-3 py-2">
                      <AppAvatar name={m.user.display_name} src={m.user.avatar_url} size="md" />
                      <span className="min-w-0 flex-1 truncate type-body text-fg">{self ? `${m.user.display_name} (you)` : m.user.display_name}</span>
                      {ROLE_LABEL[m.role] && <Badge tone={m.role === "owner" ? "primary" : "neutral"}>{ROLE_LABEL[m.role]}</Badge>}
                      {!self && !m.user.deleted && (
                        <Menu trigger={<IconButton size="sm" variant="ghost" label={`Actions for ${m.user.display_name}`} icon={<MoreHorizontal />} />}>
                          <MenuItem icon={<MessageSquare />} onSelect={() => message(m.user)}>
                            Message
                          </MenuItem>
                          {canRole && m.role === "member" && (
                            <MenuItem icon={<ShieldCheck />} onSelect={() => setRole.mutate({ userId: m.user.id, role: "admin" }, { onError: (err) => toast.error(errorMessage(err)) })}>
                              Make admin
                            </MenuItem>
                          )}
                          {canRole && m.role === "admin" && (
                            <MenuItem icon={<ShieldMinus />} onSelect={() => setRole.mutate({ userId: m.user.id, role: "member" }, { onError: (err) => toast.error(errorMessage(err)) })}>
                              Remove admin
                            </MenuItem>
                          )}
                          {canRole && (
                            <MenuItem icon={<Crown />} onSelect={() => setConfirm({ kind: "owner", member: m })}>
                              Make owner
                            </MenuItem>
                          )}
                          <MenuItem icon={<Flag />} onSelect={() => setReportUser(m.user)}>
                            Report
                          </MenuItem>
                          {canRemove && (
                            <>
                              <MenuSeparator />
                              <MenuItem icon={<UserMinus />} destructive onSelect={() => setConfirm({ kind: "remove", member: m })}>
                                Remove from group
                              </MenuItem>
                            </>
                          )}
                        </Menu>
                      )}
                    </li>
                  );
                })}
              </ul>
            </section>
          )}

          <div className="flex flex-col gap-2">
            {peer && !peer.deleted && (
              <>
                <Button variant="secondary" leftIcon={<Flag />} onClick={() => setReportUser(peer)}>
                  Report {peer.display_name}
                </Button>
                {blockedByMe ? (
                  <Button variant="secondary" leftIcon={<UserRoundCheck />} loading={unblock.isPending} onClick={() => unblock.mutate(peer.id, { onSuccess: () => void refreshChatAfterBlock(qc) })}>
                    Unblock {peer.display_name}
                  </Button>
                ) : (
                  <Button variant="danger-ghost" leftIcon={<Ban />} onClick={() => setBlockUser(peer)}>
                    Block {peer.display_name}
                  </Button>
                )}
              </>
            )}
            <Button variant="danger-ghost" leftIcon={group ? <LogOut /> : <Trash2 />} onClick={() => setConfirm({ kind: "leave" })}>
              {group ? "Leave group" : "Delete conversation"}
            </Button>
          </div>
        </div>
      </Sheet>

      <Dialog
        open={adding}
        onOpenChange={setAdding}
        title="Add people"
        size="md"
        footer={
          <Button
            disabled={picked.length === 0}
            loading={addMembers.isPending}
            onClick={() =>
              addMembers.mutate(
                picked.map((u) => u.id),
                { onSuccess: () => setAdding(false), onError: (err) => toast.error(errorMessage(err)) },
              )
            }
          >
            {picked.length > 1 ? `Add ${picked.length} people` : "Add"}
          </Button>
        }
      >
        <PeoplePicker multiple selected={picked} exclude={c.members.map((m) => m.user.id)} onToggle={(u) => setPicked((l) => (l.some((x) => x.id === u.id) ? l.filter((x) => x.id !== u.id) : [...l, u]))} />
      </Dialog>

      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={
          confirm?.kind === "owner"
            ? `Make ${confirm.member.user.display_name} the owner?`
            : confirm?.kind === "remove"
              ? `Remove ${confirm.member.user.display_name} from ${c.title}?`
              : group
                ? `Leave ${c.title}?`
                : "Delete this conversation?"
        }
        confirmLabel={confirm?.kind === "owner" ? "Make owner" : confirm?.kind === "remove" ? "Remove" : group ? "Leave group" : "Delete"}
        tone={confirm?.kind === "owner" ? "primary" : "danger"}
        onConfirm={async () => {
          if (!confirm) return;
          if (confirm.kind === "owner") await setRole.mutateAsync({ userId: confirm.member.user.id, role: "owner" });
          else if (confirm.kind === "remove") await removeMember.mutateAsync(confirm.member.user.id);
          else {
            await leave.mutateAsync(c.id);
            onOpenChange(false);
            router.push("/chat");
          }
        }}
      >
        {confirm?.kind === "owner"
          ? "You become an admin."
          : confirm?.kind === "leave"
            ? group
              ? "You stop getting its messages."
              : `Its messages are removed for you. ${peer?.display_name ?? "The other person"} still sees them.`
            : null}
      </ConfirmDialog>
      {reportUser && (
        <ReportDialog
          open
          onOpenChange={(o) => !o && setReportUser(null)}
          targetKind="user"
          targetId={reportUser.id}
          author={{ id: reportUser.id, name: reportUser.display_name }}
          onReported={(r) => r.blocked && void refreshChatAfterBlock(qc)}
        />
      )}
      {blockUser && <BlockDialog open onOpenChange={(o) => !o && setBlockUser(null)} userId={blockUser.id} name={blockUser.display_name} onBlocked={() => void refreshChatAfterBlock(qc)} />}
    </>
  );
}
