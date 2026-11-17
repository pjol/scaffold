import {
  errorMessage,
  filesCanEdit,
  filesCanManage,
  useFilesAddComment,
  useFilesComments,
  useFilesCreateRequest,
  useFilesLock,
  useFilesPatchComment,
  FILES_SEARCH_KINDS,
  type FilesComment,
  type FilesItem,
  type FilesSearchFilters,
  type FilesSearchKind,
} from "@scaffoldapp/shared";
import { Avatar, Badge, Banner, Button, EmptyState, ErrorState, Field, Input, SegmentedControl, Sheet, Skeleton, Switch, Text, TextArea, ToggleChip, toast, useTheme } from "@scaffoldapp/ui-native";
import { Check, Inbox, Lock, LockOpen, MessageSquare, Reply, Star, Users } from "lucide-react-native";
import { useState } from "react";
import { ScrollView, Share, View } from "react-native";
import type { ResourceMenuAction } from "@/features/registry";

// Drive depth on phones: comments, check out / in, file requests (create and share), search filters.

const personName = (p: { name: string } | null | undefined) => (p ? p.name || "Deleted user" : "Deleted user");

function ago(iso: string): string {
  const mins = Math.round((Date.now() - new Date(iso).getTime()) / 60_000);
  if (mins < 1) return "now";
  if (mins < 60) return `${mins} min`;
  if (mins < 1440) return `${Math.round(mins / 60)} h`;
  return new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

/** ⋮ rows the depth features add (check out / in, request files). */
export function useFilesExtraRowsNative(item: FilesItem | null | undefined, myId: string | null | undefined, onRequestFiles: (folder: FilesItem) => void): ResourceMenuAction[] {
  const lock = useFilesLock();
  if (!item || item.trashed_at) return [];
  const rows: ResourceMenuAction[] = [];
  if (item.type === "file" && filesCanEdit(item) && !item.lock) {
    rows.push({ id: "check-out", label: "Check out", icon: Lock, onSelect: () => lock.mutate({ id: item.id, locked: true }, { onSuccess: () => toast.success("Checked out"), onError: (e) => toast.error(errorMessage(e, "Couldn't check it out.")) }) });
  }
  if (item.type === "file" && item.lock && (item.lock.by?.id === myId || filesCanManage(item))) {
    const mine = item.lock.by?.id === myId;
    rows.push({
      id: mine ? "check-in" : "break-lock",
      label: mine ? "Check in" : "Break the lock",
      icon: LockOpen,
      destructive: !mine,
      onSelect: () => lock.mutate({ id: item.id, locked: false }, { onSuccess: () => toast.success(mine ? "Checked in" : "Lock broken"), onError: (e) => toast.error(errorMessage(e, mine ? "Couldn't check it in." : "Couldn't break the lock.")) }),
    });
  }
  if (item.type === "folder" && filesCanEdit(item)) rows.push({ id: "request-files", label: "Request files…", icon: Inbox, onSelect: () => onRequestFiles(item) });
  return rows;
}

/** The lock line on a file screen. */
export function LockLine({ item, myId }: { item: FilesItem; myId: string | null | undefined }) {
  if (!item.lock) return null;
  const who = item.lock.by?.id === myId ? "You" : personName(item.lock.by);
  return <Banner tone="warning" title={`${who} checked this out ${ago(item.lock.at)} ago${item.lock.note ? `: “${item.lock.note}”` : ""}`} />;
}

/** Create a file request and open the share sheet with its link. */
export function RequestFilesSheet({ folder, onClose }: { folder: FilesItem | null; onClose: () => void }) {
  const theme = useTheme();
  const create = useFilesCreateRequest(folder?.id ?? "");
  const [title, setTitle] = useState("");
  const [maxFiles, setMaxFiles] = useState("");
  const [requireEmail, setRequireEmail] = useState(true);
  const close = () => {
    setTitle("");
    setMaxFiles("");
    setRequireEmail(true);
    onClose();
  };
  const submit = () =>
    create.mutate(
      { title: title.trim(), max_files: maxFiles ? Number(maxFiles) : null, require_email: requireEmail },
      {
        onSuccess: (r) => {
          close();
          if (r.url) void Share.share({ message: `${r.title}: ${r.url}`, url: r.url }).catch(() => undefined);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't make the request.")),
      },
    );
  return (
    <Sheet
      open={folder !== null}
      onClose={close}
      title="Request files"
      info="People with the link can upload into this folder without an account. They can't see what's in it."
      snapPoints={["70%"]}
      footer={
        <Button fullWidth onPress={submit} loading={create.isPending} disabled={!title.trim()}>
          Create and share link
        </Button>
      }
    >
      <View style={{ gap: theme.space[4] }}>
        <Field label="Title">
          <Input value={title} onChangeText={setTitle} maxLength={200} placeholder="Receipts for March" />
        </Field>
        <Field label="Most files">
          <Input value={maxFiles} onChangeText={(v) => setMaxFiles(v.replace(/\D/g, ""))} keyboardType="number-pad" placeholder="No limit" />
        </Field>
        <Switch value={requireEmail} onValueChange={setRequireEmail} label="Ask for their email" />
      </View>
    </Sheet>
  );
}

/** Comment threads on an item, with replies and resolve. */
export function MobileComments({ item }: { item: FilesItem }) {
  const theme = useTheme();
  const [status, setStatus] = useState<"open" | "resolved">("open");
  const threads = useFilesComments(item.id, status);
  const canComment = item.my_level !== "view" && item.my_level !== "none" && !item.trashed_at;
  const rows = threads.data?.items ?? [];
  return (
    <View style={{ gap: theme.space[3] }}>
      <Text role="titleSm" accessibilityRole="header">
        Comments
      </Text>
      {canComment ? <Composer item={item} /> : null}
      <SegmentedControl
        accessibilityLabel="Show"
        value={status}
        onValueChange={setStatus}
        options={[
          { value: "open", label: "Open" },
          { value: "resolved", label: "Resolved" },
        ]}
      />
      {threads.isPending ? (
        <Skeleton height={80} />
      ) : threads.isError ? (
        <ErrorState compact title="Couldn't load comments" onRetry={() => void threads.refetch()} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={MessageSquare} title={status === "open" ? "No open comments" : "No resolved comments"} />
      ) : (
        rows.map((t) => <Thread key={t.id} item={item} thread={t} canComment={canComment} />)
      )}
    </View>
  );
}

function Composer({ item, threadId, onDone }: { item: FilesItem; threadId?: string; onDone?: () => void }) {
  const theme = useTheme();
  const add = useFilesAddComment(item.id);
  const [body, setBody] = useState("");
  const submit = () =>
    add.mutate(
      { body: body.trim(), thread_id: threadId ?? null },
      {
        onSuccess: () => {
          setBody("");
          onDone?.();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't post the comment.")),
      },
    );
  return (
    <View style={{ gap: theme.space[2] }}>
      <TextArea value={body} onChangeText={setBody} maxLength={10000} placeholder={threadId ? "Reply" : "Add a comment"} accessibilityLabel={threadId ? "Reply" : "Comment"} />
      <View style={{ flexDirection: "row", justifyContent: "flex-end", gap: theme.space[2] }}>
        {onDone ? (
          <Button size="md" variant="ghost" onPress={onDone}>
            Cancel
          </Button>
        ) : null}
        <Button size="md" onPress={submit} loading={add.isPending} disabled={!body.trim()}>
          {threadId ? "Reply" : "Comment"}
        </Button>
      </View>
    </View>
  );
}

function CommentRow({ c }: { c: FilesComment }) {
  const theme = useTheme();
  const anchor = c.anchor?.page ? `Page ${c.anchor.page}` : c.anchor?.seconds !== undefined ? `${Math.floor(c.anchor.seconds / 60)}:${String(Math.floor(c.anchor.seconds % 60)).padStart(2, "0")}` : null;
  return (
    <View style={{ flexDirection: "row", gap: theme.space[3] }}>
      <Avatar name={personName(c.author)} src={c.author?.avatar_url ?? null} size="sm" />
      <View style={{ flex: 1, gap: theme.space[1] }}>
        <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: theme.space[2] }}>
          <Text role="label" weight={600}>
            {personName(c.author)}
          </Text>
          <Text role="caption" tone="muted">
            {ago(c.created_at)}
            {c.edited_at ? " · edited" : ""}
          </Text>
          {anchor ? <Badge>{anchor}</Badge> : null}
        </View>
        <Text selectable>{c.body}</Text>
      </View>
    </View>
  );
}

function Thread({ item, thread, canComment }: { item: FilesItem; thread: FilesComment; canComment: boolean }) {
  const theme = useTheme();
  const patch = useFilesPatchComment(item.id);
  const [replying, setReplying] = useState(false);
  const canResolve = canComment && (thread.mine || filesCanEdit(item));
  return (
    <View style={{ gap: theme.space[3], padding: theme.space[4], borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.lg, backgroundColor: theme.colors.surface }}>
      <CommentRow c={thread} />
      {(thread.replies ?? []).map((r) => (
        <View key={r.id} style={{ borderLeftWidth: 2, borderColor: theme.colors.border, paddingLeft: theme.space[3] }}>
          <CommentRow c={r} />
        </View>
      ))}
      {replying ? <Composer item={item} threadId={thread.id} onDone={() => setReplying(false)} /> : null}
      {!replying && canComment ? (
        <View style={{ flexDirection: "row", gap: theme.space[2] }}>
          <Button size="md" variant="ghost" leftIcon={Reply} onPress={() => setReplying(true)}>
            Reply
          </Button>
          {canResolve ? (
            <Button size="md" variant="ghost" leftIcon={Check} loading={patch.isPending} onPress={() => patch.mutate({ commentId: thread.id, resolved: !thread.resolved_at }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) })}>
              {thread.resolved_at ? "Reopen" : "Resolve"}
            </Button>
          ) : null}
        </View>
      ) : null}
    </View>
  );
}

/** Search filter chips: kind, owned by me, starred, shared with me. */
export function SearchFilterChips({ value, onChange }: { value: FilesSearchFilters; onChange: (f: FilesSearchFilters) => void }) {
  const theme = useTheme();
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: theme.space[2] }} accessibilityLabel="Search filters">
      <ToggleChip size="sm" icon={Star} pressed={!!value.starred} onPressedChange={(p) => onChange({ ...value, starred: p || undefined })}>
        Starred
      </ToggleChip>
      <ToggleChip size="sm" pressed={value.owner === "me"} onPressedChange={(p) => onChange({ ...value, owner: p ? "me" : undefined })}>
        Owned by me
      </ToggleChip>
      <ToggleChip size="sm" icon={Users} pressed={!!value.shared_with_me} onPressedChange={(p) => onChange({ ...value, shared_with_me: p || undefined })}>
        Shared with me
      </ToggleChip>
      {FILES_SEARCH_KINDS.map((k) => (
        <ToggleChip key={k.id} size="sm" pressed={value.kind === k.id} onPressedChange={(p) => onChange({ ...value, kind: p ? (k.id as FilesSearchKind) : undefined })}>
          {k.label}
        </ToggleChip>
      ))}
    </ScrollView>
  );
}
