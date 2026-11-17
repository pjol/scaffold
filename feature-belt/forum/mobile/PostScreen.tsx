import {
  errorMessage,
  forumAge,
  forumCount,
  forumMentionIds,
  useCreateForumComment,
  useDeleteForumComment,
  useDeleteForumPost,
  useForumComments,
  useForumPost,
  useForumPostTopic,
  useForumReplies,
  useMe,
  useModerateForumComment,
  useModerateForumPost,
  useUpdateForumComment,
  type ForumComment,
  type ForumCommentSort,
  type ForumPost,
  type ForumPostModAction,
} from "@scaffoldapp/shared";
import { Avatar, Banner, Button, ConfirmDialog, EmptyState, ErrorState, IconButton, ListRow, Screen, SegmentedControl, Sheet, Skeleton, Spinner, Text, Title, TitleSm, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, type Href } from "expo-router";
import { Ban, ChevronDown, ChevronRight, ExternalLink, FileQuestion, Flag, Lock, LockOpen, MessageSquare, MoreHorizontal, Pencil, Pin, PinOff, Reply, RotateCcw, Share2, Trash2, XCircle } from "lucide-react-native";
import { useRef, useState } from "react";
import { Pressable, RefreshControl, Share, View } from "react-native";
import { BlockDialog, ReportDialog } from "@/features/moderation";
import { shareableForumLink } from "./links";
import { ForumMentionInput, type ForumMentionPerson } from "./MentionInput";
import { forumCommunityHref, ForumBadges, ForumMarkdown, ForumModerationBanner, ForumVoteButtons, openForumUrl } from "./parts";

/** Write a comment or reply, or edit one, inline. */
function CommentComposer({ postId, parentId, editing, onDone }: { postId: string; parentId?: string; editing?: ForumComment; onDone?: () => void }) {
  const theme = useTheme();
  const create = useCreateForumComment();
  const update = useUpdateForumComment();
  const [body, setBody] = useState(editing?.body ?? "");
  const [people, setPeople] = useState<ForumMentionPerson[]>(editing?.mentions ?? []);
  const busy = create.isPending || update.isPending;
  const submit = async () => {
    if (!body.trim() || busy) return;
    const mentions = forumMentionIds(body, people);
    try {
      const res = editing
        ? await update.mutateAsync({ id: editing.id, postId, body, mentions })
        : await create.mutateAsync({ postId, body, mentions, ...(parentId ? { parent_id: parentId } : {}) });
      if (res.moderation.status === "held") toast.warning("Posted. It's waiting for review, so only you can see it for now.");
      else if (res.moderation.status === "removed") toast.error(`It can't be shown: ${res.moderation.reasons.join(". ") || "it breaks the rules"}.`);
      if (!editing) {
        setBody("");
        setPeople([]);
      }
      onDone?.();
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't save your comment. Try again."));
    }
  };
  return (
    <View style={{ gap: theme.space[2] }}>
      <ForumMentionInput
        value={body}
        onChange={setBody}
        people={people}
        onPeopleChange={setPeople}
        placeholder={parentId ? "Write a reply" : "Write a comment"}
        accessibilityLabel={editing ? "Edit comment" : parentId ? "Reply" : "Comment"}
        minHeight={theme.space[20]}
        autoFocus={!!(parentId || editing)}
      />
      <View style={{ flexDirection: "row", justifyContent: "flex-end", gap: theme.space[2] }}>
        {onDone && (parentId || editing) ? (
          <Button variant="ghost" size="md" onPress={onDone} disabled={busy}>
            Cancel
          </Button>
        ) : null}
        <Button size="md" loading={busy} disabled={!body.trim()} onPress={() => void submit()}>
          {editing ? "Save" : parentId ? "Reply" : "Comment"}
        </Button>
      </View>
    </View>
  );
}

type Menu = { kind: "post" } | { kind: "comment"; c: ForumComment };
type Next =
  | { kind: "share" | "deletePost" | "removePost" | "reportPost" | "blockPost" }
  | { kind: "editComment" | "deleteComment" | "removeComment" | "reportComment" | "blockComment"; c: ForumComment }
  | null;

interface TreeProps {
  post: ForumPost;
  depth: number;
  replyTo: string | null;
  editing: string | null;
  onReply: (id: string | null) => void;
  onEdit: (id: string | null) => void;
  onMenu: (m: Menu) => void;
}

function MoreReplies({ commentId, ...props }: TreeProps & { commentId: string }) {
  const theme = useTheme();
  const replies = useForumReplies(commentId);
  if (replies.isPending) return <Spinner size={16} />;
  if (replies.isError) return <ErrorState compact title="Couldn't load replies" info={errorMessage(replies.error)} onRetry={() => void replies.refetch()} retrying={replies.isRefetching} />;
  return (
    <View style={{ gap: theme.space[4] }}>
      {(replies.data?.items ?? []).map((r) => (
        <CommentNode key={r.id} c={r} {...props} />
      ))}
      {replies.hasNextPage ? (
        <Button variant="ghost" size="md" loading={replies.isFetchingNextPage} onPress={() => void replies.fetchNextPage()}>
          Load more replies
        </Button>
      ) : null}
    </View>
  );
}

function CommentNode({ c, ...props }: TreeProps & { c: ForumComment }) {
  const theme = useTheme();
  const s = theme.space;
  const me = useMe();
  const { post, depth, replyTo, editing, onReply, onEdit, onMenu } = props;
  const [collapsed, setCollapsed] = useState(false);
  const [more, setMore] = useState(false);
  const mine = !!c.author && me.data?.id === c.author.id;
  const canReply = post.status === "visible" && c.status === "visible" && (!post.locked || post.can_moderate);
  const hasReplies = c.replies.length > 0 || c.more_replies > 0;
  const child = { ...props, depth: depth + 1 };
  return (
    <View style={{ gap: s[2] }}>
      {!c.author ? (
        <Text role="label" tone="muted">
          {c.status === "deleted" ? "Comment deleted" : "Comment removed"}
        </Text>
      ) : (
        <>
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={collapsed ? "Expand comment" : "Collapse comment"}
            accessibilityState={{ expanded: !collapsed }}
            onPress={() => setCollapsed((x) => !x)}
            style={{ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: theme.tap.base }}
          >
            <Avatar name={c.author.display_name || "?"} src={c.author.avatar_url} size="xs" />
            <Text role="label" weight={600} numberOfLines={1} style={{ flexShrink: 1 }}>
              {c.author.display_name || "Unnamed"}
            </Text>
            <Text role="caption" tone="muted">
              {`${forumAge(c.created_at)}${c.edited_at ? " · Edited" : ""}`}
            </Text>
            <View style={{ flex: 1 }} />
            {collapsed ? <ChevronRight size={16} color={theme.colors.textMuted} /> : <ChevronDown size={16} color={theme.colors.textMuted} />}
          </Pressable>
          {collapsed ? null : (
            <>
              <ForumBadges status={c.status} labels={c.labels} />
              {mine ? <ForumModerationBanner status={c.status} reasons={c.moderation_reasons} noun="comment" /> : null}
              {editing === c.id ? <CommentComposer postId={post.id} editing={c} onDone={() => onEdit(null)} /> : <ForumMarkdown text={c.body} mentions={c.mentions} />}
              <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
                <ForumVoteButtons kind="comment" id={c.id} postId={post.id} authorId={c.author.id} score={c.score} vote={c.viewer_vote} disabled={c.status !== "visible"} />
                {canReply ? (
                  <Button variant="ghost" size="md" leftIcon={Reply} onPress={() => onReply(replyTo === c.id ? null : c.id)}>
                    Reply
                  </Button>
                ) : null}
                <View style={{ flex: 1 }} />
                <IconButton label="Comment options" icon={MoreHorizontal} onPress={() => onMenu({ kind: "comment", c })} />
              </View>
              {replyTo === c.id ? <CommentComposer postId={post.id} parentId={c.id} onDone={() => onReply(null)} /> : null}
            </>
          )}
        </>
      )}
      {!collapsed && hasReplies ? (
        <View style={{ gap: s[4], paddingLeft: depth < 5 ? s[3] : 0, borderLeftWidth: depth < 5 ? 1 : 0, borderLeftColor: theme.colors.border }}>
          {more ? (
            <MoreReplies commentId={c.id} {...child} />
          ) : (
            <>
              {c.replies.map((r) => (
                <CommentNode key={r.id} c={r} {...child} />
              ))}
              {c.more_replies > 0 ? (
                <Button variant="ghost" size="md" leftIcon={MessageSquare} onPress={() => setMore(true)} style={{ alignSelf: "flex-start" }}>
                  {forumCount(c.more_replies, "more reply", "more replies")}
                </Button>
              ) : null}
            </>
          )}
        </View>
      ) : null}
    </View>
  );
}

/** /forum/posts/[id]: the post, a comment composer and the comment tree; actions in a sheet. */
export function PostScreen({ id }: { id: string }) {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  useForumPostTopic(id);
  const me = useMe();
  const post = useForumPost(id);
  const [sort, setSort] = useState<ForumCommentSort>("top");
  const comments = useForumComments(post.isSuccess ? id : null, sort);
  const [replyTo, setReplyTo] = useState<string | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  const [menu, setMenu] = useState<Menu | null>(null);
  const [dialog, setDialog] = useState<Next>(null);
  const next = useRef<Next>(null);
  const delPost = useDeleteForumPost();
  const modPost = useModerateForumPost();
  const delComment = useDeleteForumComment();
  const modComment = useModerateForumComment();

  if (post.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <View style={{ gap: s[3] }} accessibilityLabel="Loading post" accessibilityRole="progressbar">
          <Skeleton width="40%" height={12} />
          <Skeleton width="85%" height={24} />
          <Skeleton height={14} />
          <Skeleton width="70%" height={14} />
        </View>
      </Screen>
    );
  }
  if (post.isError) {
    return (
      <Screen edges={["bottom"]}>
        {post.error.status === 404 ? (
          <EmptyState icon={FileQuestion} title="Post not found" />
        ) : (
          <ErrorState title="Couldn't load this post" info={errorMessage(post.error)} onRetry={() => void post.refetch()} retrying={post.isRefetching} />
        )}
      </Screen>
    );
  }

  const p = post.data;
  const mine = !!p.author && me.data?.id === p.author.id;
  const staff = !!me.data?.admin_role;
  const name = p.author?.display_name || "Deleted user";
  const canComment = (p.status === "visible" || (p.status === "held" && mine)) && (!p.locked || p.can_moderate);
  const choose = (n: Next) => {
    next.current = n;
    setMenu(null);
  };
  const act = (action: ForumPostModAction, done: string) =>
    modPost.mutate({ id: p.id, action }, { onSuccess: () => toast.success(done), onError: (e) => toast.error(errorMessage(e, "Couldn't do that. Try again.")) });
  const items = comments.data?.items ?? [];
  const tree: TreeProps = { post: p, depth: 0, replyTo, editing, onReply: setReplyTo, onEdit: setEditing, onMenu: setMenu };
  const target = menu?.kind === "comment" ? menu.c : null;
  const dialogComment = dialog && "c" in dialog ? dialog.c : null;

  return (
    <Screen
      edges={["bottom"]}
      gap={s[8]}
      keyboard
      refreshControl={
        <RefreshControl
          refreshing={post.isRefetching || comments.isRefetching}
          onRefresh={() => {
            void post.refetch();
            void comments.refetch();
          }}
        />
      }
    >
      <View style={{ gap: s[3] }}>
        {p.moderation_reasons.length > 0 || p.status === "held" || p.status === "removed" ? <ForumModerationBanner status={p.status} reasons={p.moderation_reasons} noun="post" /> : null}
        <View style={{ flexDirection: "row", alignItems: "center", flexWrap: "wrap", gap: s[2] }}>
          <Pressable accessibilityRole="link" hitSlop={8} onPress={() => router.push(forumCommunityHref(p.community.slug))}>
            <Text role="caption" weight={600}>
              {p.community.title}
            </Text>
          </Pressable>
          {p.author ? <Avatar name={name} src={p.author.avatar_url} size="xs" /> : null}
          <Text role="caption" tone="muted" numberOfLines={1} style={{ flexShrink: 1 }}>
            {`${p.author ? `${name} · ` : ""}${forumAge(p.created_at)}${p.edited_at ? " · Edited" : ""}`}
          </Text>
        </View>
        <Title>{p.title}</Title>
        <ForumBadges pinned={p.pinned} locked={p.locked} status={p.status} labels={p.labels} />
        {p.kind === "link" && p.url ? (
          <Pressable
            accessibilityRole="link"
            onPress={() => openForumUrl(p.url ?? "")}
            style={{ minHeight: theme.tap.base, flexDirection: "row", alignItems: "center", gap: s[3], paddingHorizontal: s[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border }}
          >
            <ExternalLink size={18} color={c.textMuted} strokeWidth={2} />
            <Text role="label" numberOfLines={1} style={{ flex: 1 }}>
              {p.domain ?? p.url}
            </Text>
          </Pressable>
        ) : null}
        {p.status === "deleted" ? (
          <Text role="body" tone="muted">
            The author deleted this post.
          </Text>
        ) : (
          <ForumMarkdown text={p.body} mentions={p.mentions} />
        )}
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <ForumVoteButtons kind="post" id={p.id} postId={p.id} authorId={p.author?.id} score={p.score} vote={p.viewer_vote} disabled={p.status !== "visible"} />
          <View style={{ flex: 1 }} />
          <IconButton label="Post options" icon={MoreHorizontal} onPress={() => setMenu({ kind: "post" })} />
        </View>
      </View>

      <View style={{ gap: s[4] }}>
        <TitleSm>{forumCount(p.comment_count, "comment", "comments")}</TitleSm>
        <SegmentedControl<ForumCommentSort>
          accessibilityLabel="Sort comments"
          value={sort}
          onValueChange={setSort}
          options={[
            { value: "top", label: "Top" },
            { value: "new", label: "Newest" },
            { value: "old", label: "Oldest" },
          ]}
        />
        {canComment ? <CommentComposer postId={p.id} /> : p.locked ? <Banner tone="info" title="Comments are locked on this post." /> : null}
        {comments.isPending ? (
          <View style={{ gap: s[3] }} accessibilityLabel="Loading comments" accessibilityRole="progressbar">
            <Skeleton width="40%" height={12} />
            <Skeleton height={14} />
          </View>
        ) : comments.isError ? (
          <ErrorState compact title="Couldn't load comments" info={errorMessage(comments.error)} onRetry={() => void comments.refetch()} retrying={comments.isRefetching} />
        ) : items.length === 0 ? (
          <EmptyState compact icon={MessageSquare} title="No comments yet" />
        ) : (
          <View style={{ gap: s[5] }}>
            {items.map((cm) => (
              <CommentNode key={cm.id} c={cm} {...tree} />
            ))}
            {comments.hasNextPage ? (
              <Button variant="ghost" loading={comments.isFetchingNextPage} onPress={() => void comments.fetchNextPage()}>
                Load more comments
              </Button>
            ) : null}
          </View>
        )}
      </View>

      <Sheet
        open={menu !== null}
        onClose={() => setMenu(null)}
        onDismissed={() => {
          const n = next.current;
          next.current = null;
          if (!n) return;
          if (n.kind === "share") void Share.share({ message: shareableForumLink(`/forum/posts/${p.id}`) }).catch(() => toast.error("Couldn't open sharing. Try again."));
          else if (n.kind === "editComment") setEditing(n.c.id);
          else setDialog(n);
        }}
        title={target ? "Comment" : "Post"}
      >
        <View style={{ gap: s[1] }}>
          {target ? (
            <>
              {target.author && me.data?.id === target.author.id ? (
                <>
                  {target.status === "visible" ? <ListRow title="Edit" leading={<Pencil size={20} color={c.textMuted} />} onPress={() => choose({ kind: "editComment", c: target })} /> : null}
                  <ListRow title="Delete" destructive leading={<Trash2 size={20} color={c.dangerStrong} />} onPress={() => choose({ kind: "deleteComment", c: target })} />
                </>
              ) : target.author ? (
                <>
                  <ListRow title="Report comment" leading={<Flag size={20} color={c.textMuted} />} onPress={() => choose({ kind: "reportComment", c: target })} />
                  <ListRow title={`Block ${target.author.display_name || "this person"}`} leading={<Ban size={20} color={c.textMuted} />} onPress={() => choose({ kind: "blockComment", c: target })} />
                  {p.can_moderate && target.status === "visible" ? (
                    <ListRow title="Remove comment" destructive leading={<XCircle size={20} color={c.dangerStrong} />} onPress={() => choose({ kind: "removeComment", c: target })} />
                  ) : null}
                </>
              ) : null}
              {staff && target.status === "removed" ? (
                <ListRow
                  title="Restore comment"
                  leading={<RotateCcw size={20} color={c.textMuted} />}
                  onPress={() => {
                    setMenu(null);
                    modComment.mutate(
                      { id: target.id, postId: p.id, action: "restore" },
                      { onSuccess: () => toast.success("Comment restored"), onError: (e) => toast.error(errorMessage(e, "Couldn't restore it. Try again.")) },
                    );
                  }}
                />
              ) : null}
            </>
          ) : (
            <>
              <ListRow title="Share post" leading={<Share2 size={20} color={c.textMuted} />} onPress={() => choose({ kind: "share" })} />
              {mine && p.status === "visible" ? (
                <ListRow
                  title="Edit"
                  leading={<Pencil size={20} color={c.textMuted} />}
                  onPress={() => {
                    setMenu(null);
                    router.push(`/forum/submit?edit=${p.id}` as Href);
                  }}
                />
              ) : null}
              {!mine && p.author ? (
                <>
                  <ListRow title="Report post" leading={<Flag size={20} color={c.textMuted} />} onPress={() => choose({ kind: "reportPost" })} />
                  <ListRow title={`Block ${name}`} leading={<Ban size={20} color={c.textMuted} />} onPress={() => choose({ kind: "blockPost" })} />
                </>
              ) : null}
              {p.can_moderate && p.status === "visible" ? (
                <>
                  <ListRow
                    title={p.pinned ? "Unpin" : "Pin to the community"}
                    leading={p.pinned ? <PinOff size={20} color={c.textMuted} /> : <Pin size={20} color={c.textMuted} />}
                    onPress={() => {
                      setMenu(null);
                      act(p.pinned ? "unpin" : "pin", p.pinned ? "Post unpinned" : "Post pinned");
                    }}
                  />
                  <ListRow
                    title={p.locked ? "Reopen comments" : "Lock comments"}
                    leading={p.locked ? <LockOpen size={20} color={c.textMuted} /> : <Lock size={20} color={c.textMuted} />}
                    onPress={() => {
                      setMenu(null);
                      act(p.locked ? "reopen" : "lock", p.locked ? "Comments reopened" : "Comments locked");
                    }}
                  />
                  {!mine ? <ListRow title="Remove post" destructive leading={<XCircle size={20} color={c.dangerStrong} />} onPress={() => choose({ kind: "removePost" })} /> : null}
                </>
              ) : null}
              {staff && p.status === "removed" ? (
                <ListRow
                  title="Restore post"
                  leading={<RotateCcw size={20} color={c.textMuted} />}
                  onPress={() => {
                    setMenu(null);
                    act("restore", "Post restored");
                  }}
                />
              ) : null}
              {mine && p.status !== "deleted" ? <ListRow title="Delete" destructive leading={<Trash2 size={20} color={c.dangerStrong} />} onPress={() => choose({ kind: "deletePost" })} /> : null}
            </>
          )}
        </View>
      </Sheet>

      <ConfirmDialog
        open={dialog?.kind === "deletePost"}
        onClose={() => setDialog(null)}
        title="Delete this post?"
        tone="danger"
        confirmLabel="Delete"
        onConfirm={async () => {
          await delPost.mutateAsync(p.id);
          toast.success("Post deleted");
          router.back();
        }}
        errorText={(e) => errorMessage(e, "Couldn't delete the post. Try again.")}
      >
        <Text role="body">Its comments stay under a placeholder. It can't be undone.</Text>
      </ConfirmDialog>
      <ConfirmDialog
        open={dialog?.kind === "removePost"}
        onClose={() => setDialog(null)}
        title="Remove this post?"
        tone="danger"
        confirmLabel="Remove"
        onConfirm={async () => {
          await modPost.mutateAsync({ id: p.id, action: "remove" });
          toast.success("Post removed");
        }}
        errorText={(e) => errorMessage(e, "Couldn't remove the post. Try again.")}
      >
        <Text role="body">It disappears for everyone but its author and staff. It's written to the audit log.</Text>
      </ConfirmDialog>
      <ConfirmDialog
        open={dialog?.kind === "deleteComment"}
        onClose={() => setDialog(null)}
        title="Delete this comment?"
        tone="danger"
        confirmLabel="Delete"
        onConfirm={async () => {
          if (dialogComment) await delComment.mutateAsync({ id: dialogComment.id, postId: p.id });
          toast.success("Comment deleted");
        }}
        errorText={(e) => errorMessage(e, "Couldn't delete the comment. Try again.")}
      >
        <Text role="body">Replies stay under a placeholder. It can't be undone.</Text>
      </ConfirmDialog>
      <ConfirmDialog
        open={dialog?.kind === "removeComment"}
        onClose={() => setDialog(null)}
        title="Remove this comment?"
        tone="danger"
        confirmLabel="Remove"
        onConfirm={async () => {
          if (dialogComment) await modComment.mutateAsync({ id: dialogComment.id, postId: p.id, action: "remove" });
          toast.success("Comment removed");
        }}
        errorText={(e) => errorMessage(e, "Couldn't remove the comment. Try again.")}
      >
        <Text role="body">Everyone else sees a placeholder. It's written to the audit log.</Text>
      </ConfirmDialog>
      {p.author && !mine ? (
        <>
          <ReportDialog open={dialog?.kind === "reportPost"} onClose={() => setDialog(null)} targetKind="forum.post" targetId={p.id} noun="post" author={{ id: p.author.id, name }} />
          <BlockDialog open={dialog?.kind === "blockPost"} onClose={() => setDialog(null)} userId={p.author.id} name={name} onBlocked={() => router.back()} />
        </>
      ) : null}
      {dialogComment?.author ? (
        <>
          <ReportDialog
            open={dialog?.kind === "reportComment"}
            onClose={() => setDialog(null)}
            targetKind="forum.comment"
            targetId={dialogComment.id}
            noun="comment"
            author={{ id: dialogComment.author.id, name: dialogComment.author.display_name || "this person" }}
          />
          <BlockDialog open={dialog?.kind === "blockComment"} onClose={() => setDialog(null)} userId={dialogComment.author.id} name={dialogComment.author.display_name || "this person"} />
        </>
      ) : null}
    </Screen>
  );
}
