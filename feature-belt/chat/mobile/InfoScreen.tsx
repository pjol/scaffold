import {
  CHAT_MUTE_OPTIONS,
  chatFullTime,
  errorMessage,
  muteRequest,
  refreshChatAfterBlock,
  useChatConversation,
  useCreateConversation,
  useIsBlocked,
  useLeaveConversation,
  useMe,
  useMuteConversation,
  useQueryClient,
  useRemoveChatMember,
  useSetMemberRole,
  useUnblockUser,
  useUpdateConversation,
  useUpload,
  type ChatMember,
  type ChatUser,
} from "@scaffoldapp/shared";
import { Avatar, Badge, Button, ConfirmDialog, EmptyState, ErrorState, Field, Input, ListGroup, ListRow, Screen, Sheet, Skeleton, Title, pickPhotos, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { Ban, Camera, Check, Crown, Flag, LogOut, MessageSquare, MessagesSquare, ShieldCheck, ShieldMinus, Trash2, UserMinus, UserPlus, UserRoundCheck } from "lucide-react-native";
import { useEffect, useRef, useState } from "react";
import { View } from "react-native";
import { BlockDialog, ReportDialog } from "@/features/moderation";
import { ConversationAvatar } from "./ConversationAvatar";

type Next = { kind: "owner" | "remove" | "report"; user: ChatUser } | { kind: "leave" } | { kind: "block"; user: ChatUser } | null;

/** chat/<id>/info: name and photo (owner/admin), notifications, members and roles, report/block, leave. */
export function InfoScreen() {
  const { id = "" } = useLocalSearchParams<{ id: string }>();
  const theme = useTheme();
  const col = theme.colors;
  const s = theme.space;
  const qc = useQueryClient();
  const me = useMe();
  const meId = me.data?.id;
  const conv = useChatConversation(id);
  const update = useUpdateConversation(id);
  const upload = useUpload();
  const mute = useMuteConversation(id);
  const setRole = useSetMemberRole(id);
  const removeMember = useRemoveChatMember(id);
  const leave = useLeaveConversation();
  const createDm = useCreateConversation();
  const unblock = useUnblockUser();
  const blockedByMe = useIsBlocked(conv.data?.peer?.id);
  const [title, setTitle] = useState("");
  const [memberSheet, setMemberSheet] = useState<ChatMember | null>(null);
  const [next, setNext] = useState<Next>(null);
  const afterSheet = useRef<Next>(null);

  useEffect(() => {
    if (conv.data) setTitle(conv.data.title);
  }, [conv.data?.title]); // eslint-disable-line react-hooks/exhaustive-deps

  if (conv.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Details" }} />
        <View style={{ alignItems: "center", gap: s[3] }}>
          <Skeleton shape="circle" width={64} height={64} />
          <Skeleton width="50%" height={theme.type.title.lineHeight} />
        </View>
        <Skeleton height={160} />
      </Screen>
    );
  }
  if (conv.isError) {
    return (
      <Screen centered edges={["bottom"]}>
        <Stack.Screen options={{ title: "Details" }} />
        {conv.error.status === 404 ? (
          <EmptyState icon={MessagesSquare} title="Conversation not found" />
        ) : (
          <ErrorState title="Couldn't load details" info={conv.error.message} onRetry={() => void conv.refetch()} retrying={conv.isFetching} />
        )}
      </Screen>
    );
  }

  const c = conv.data;
  const group = c.kind === "group";
  const manage = group && (c.role === "owner" || c.role === "admin");
  const peer = c.peer;
  const muteKey = !c.muted ? "off" : c.muted_until ? "until" : "forever";

  const changePhoto = async () => {
    const [photo] = await pickPhotos("library", 1, 0.8);
    if (!photo) return;
    try {
      const res = await upload.mutateAsync({ file: { uri: photo.uri, mimeType: photo.mimeType ?? "image/jpeg", name: photo.name ?? "photo.jpg" }, purpose: "chat_avatar", maxBytes: 5 * 1024 * 1024 });
      await update.mutateAsync({ avatar_key: res.key });
      toast.success("Group photo updated");
    } catch (err) {
      toast.error(errorMessage(err));
    }
  };

  const message = (user: ChatUser) =>
    createDm.mutate({ kind: "direct", user_id: user.id }, { onSuccess: (d) => router.push(`/chat/${d.id}`), onError: (err) => toast.error(errorMessage(err)) });

  const icon = (I: typeof Check, danger = false) => <I color={danger ? col.dangerStrong : col.textMuted} size={20} />;
  const m = memberSheet;
  const canRole = !!m && c.role === "owner" && m.user.id !== meId;
  const canRemove = !!m && m.user.id !== meId && m.role !== "owner" && (c.role === "owner" || (c.role === "admin" && m.role === "member"));
  const pickAfter = (n: Next) => {
    afterSheet.current = n;
    setMemberSheet(null);
  };

  return (
    <Screen edges={["bottom"]} keyboard>
      <Stack.Screen options={{ title: "Details" }} />
      <View style={{ alignItems: "center", gap: s[3] }}>
        <ConversationAvatar conversation={c} size="xl" />
        <Title align="center">{c.title}</Title>
        {manage ? (
          <View style={{ flexDirection: "row", flexWrap: "wrap", justifyContent: "center", gap: s[2] }}>
            <Button size="md" variant="secondary" leftIcon={Camera} loading={upload.isPending} onPress={() => void changePhoto()}>
              {c.avatar_url ? "Change photo" : "Add photo"}
            </Button>
            {c.avatar_url ? (
              <Button size="md" variant="ghost" onPress={() => update.mutate({ avatar_key: "" }, { onError: (err) => toast.error(errorMessage(err)) })}>
                Remove photo
              </Button>
            ) : null}
          </View>
        ) : null}
      </View>

      {manage ? (
        <View style={{ gap: s[3] }}>
          <Field label="Group name">
            <Input value={title} onChangeText={setTitle} maxLength={80} />
          </Field>
          <Button
            variant="secondary"
            disabled={!title.trim() || title.trim() === c.title}
            loading={update.isPending}
            onPress={() => update.mutate({ title: title.trim() }, { onSuccess: () => toast.success("Group renamed"), onError: (err) => toast.error(errorMessage(err)) })}
          >
            Save name
          </Button>
        </View>
      ) : null}

      <ListGroup title="Notifications">
        <ListRow title="On" trailing={muteKey === "off" ? icon(Check) : undefined} onPress={() => mute.mutate(null)} chevron={false} />
        {c.muted_until ? <ListRow title={`Off until ${chatFullTime(c.muted_until)}`} trailing={icon(Check)} chevron={false} /> : null}
        {CHAT_MUTE_OPTIONS.map((o) => (
          <ListRow
            key={o.label}
            title={o.minutes === null ? "Off until I turn them on" : `Off ${o.label.toLowerCase()}`}
            trailing={o.minutes === null && muteKey === "forever" ? icon(Check) : undefined}
            chevron={false}
            onPress={() => mute.mutate(muteRequest(o.minutes), { onError: (err) => toast.error(errorMessage(err)) })}
          />
        ))}
      </ListGroup>

      {group ? (
        <ListGroup title={`${c.member_count} members`}>
          {manage ? <ListRow title="Add people" leading={icon(UserPlus)} onPress={() => router.push(`/chat/new?add=${c.id}`)} /> : null}
          {c.members.map((mb) => (
            <ListRow
              key={mb.user.id}
              title={mb.user.id === meId ? `${mb.user.display_name} (you)` : mb.user.display_name}
              leading={<Avatar name={mb.user.display_name} src={mb.user.avatar_url} size="md" />}
              trailing={mb.role !== "member" ? <Badge tone={mb.role === "owner" ? "primary" : "neutral"}>{mb.role === "owner" ? "Owner" : "Admin"}</Badge> : undefined}
              {...(mb.user.id !== meId && !mb.user.deleted ? { onPress: () => setMemberSheet(mb) } : { chevron: false })}
            />
          ))}
        </ListGroup>
      ) : null}

      <ListGroup>
        {peer && !peer.deleted ? (
          <>
            <ListRow title={`Report ${peer.display_name}`} leading={icon(Flag)} onPress={() => setNext({ kind: "report", user: peer })} />
            {blockedByMe ? (
              <ListRow title={`Unblock ${peer.display_name}`} leading={icon(UserRoundCheck)} onPress={() => unblock.mutate(peer.id, { onSuccess: () => void refreshChatAfterBlock(qc) })} />
            ) : (
              <ListRow title={`Block ${peer.display_name}`} destructive leading={icon(Ban, true)} onPress={() => setNext({ kind: "block", user: peer })} />
            )}
          </>
        ) : null}
        <ListRow title={group ? "Leave group" : "Delete conversation"} destructive leading={icon(group ? LogOut : Trash2, true)} onPress={() => setNext({ kind: "leave" })} />
      </ListGroup>

      <Sheet
        open={m !== null}
        onClose={() => setMemberSheet(null)}
        title={m?.user.display_name ?? ""}
        onDismissed={() => {
          if (afterSheet.current) {
            setNext(afterSheet.current);
            afterSheet.current = null;
          }
        }}
      >
        {m ? (
          <ListGroup>
            <ListRow
              title="Message"
              leading={icon(MessageSquare)}
              onPress={() => {
                setMemberSheet(null);
                message(m.user);
              }}
            />
            {canRole && m.role === "member" ? (
              <ListRow
                title="Make admin"
                leading={icon(ShieldCheck)}
                onPress={() => {
                  setRole.mutate({ userId: m.user.id, role: "admin" }, { onError: (err) => toast.error(errorMessage(err)) });
                  setMemberSheet(null);
                }}
              />
            ) : null}
            {canRole && m.role === "admin" ? (
              <ListRow
                title="Remove admin"
                leading={icon(ShieldMinus)}
                onPress={() => {
                  setRole.mutate({ userId: m.user.id, role: "member" }, { onError: (err) => toast.error(errorMessage(err)) });
                  setMemberSheet(null);
                }}
              />
            ) : null}
            {canRole ? <ListRow title="Make owner" leading={icon(Crown)} onPress={() => pickAfter({ kind: "owner", user: m.user })} /> : null}
            <ListRow title="Report" leading={icon(Flag)} onPress={() => pickAfter({ kind: "report", user: m.user })} />
            {canRemove ? <ListRow title="Remove from group" destructive leading={icon(UserMinus, true)} onPress={() => pickAfter({ kind: "remove", user: m.user })} /> : null}
          </ListGroup>
        ) : null}
      </Sheet>

      <ConfirmDialog
        open={next?.kind === "owner" || next?.kind === "remove" || next?.kind === "leave"}
        onClose={() => setNext(null)}
        title={
          next?.kind === "owner"
            ? `Make ${next.user.display_name} the owner?`
            : next?.kind === "remove"
              ? `Remove ${next.user.display_name} from ${c.title}?`
              : group
                ? `Leave ${c.title}?`
                : "Delete this conversation?"
        }
        confirmLabel={next?.kind === "owner" ? "Make owner" : next?.kind === "remove" ? "Remove" : group ? "Leave group" : "Delete"}
        tone={next?.kind === "owner" ? "primary" : "danger"}
        onConfirm={async () => {
          if (next?.kind === "owner") await setRole.mutateAsync({ userId: next.user.id, role: "owner" });
          else if (next?.kind === "remove") await removeMember.mutateAsync(next.user.id);
          else if (next?.kind === "leave") {
            await leave.mutateAsync(c.id);
            router.dismissTo("/chat");
          }
        }}
        errorText={(e) => errorMessage(e)}
      />
      {next?.kind === "report" ? (
        <ReportDialog open onClose={() => setNext(null)} targetKind="user" targetId={next.user.id} author={{ id: next.user.id, name: next.user.display_name }} onReported={(r) => r.blocked && void refreshChatAfterBlock(qc)} />
      ) : null}
      {next?.kind === "block" ? <BlockDialog open onClose={() => setNext(null)} userId={next.user.id} name={next.user.display_name} onBlocked={() => void refreshChatAfterBlock(qc)} /> : null}
    </Screen>
  );
}
