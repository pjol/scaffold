"use client";

import {
  errorMessage,
  FORUM_LIMITS,
  forumCount,
  forumMentionIds,
  useCreateForumComment,
  useDeleteForumComment,
  useForumReplies,
  useMe,
  useModerateForumComment,
  useUpdateForumComment,
  type ForumComment,
  type ForumPost,
  type PagedQueryResult,
} from "@scaffoldapp/shared";
import { Button, cn, ConfirmDialog, EmptyState, ErrorState, IconButton, Menu, MenuItem, MenuSeparator, Skeleton, SkeletonGroup, SkeletonText, toast } from "@scaffoldapp/ui-web";
import { Ban, Flag, Link2, MessageSquare, Minus, MoreHorizontal, Pencil, Plus, Reply, RotateCcw, Trash2, XCircle } from "lucide-react";
import { useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { BlockDialog, ReportDialog } from "@/features/moderation";
import { formatDateTime, formatRelative } from "@/lib/format";
import { ForumMentionTextArea, type ForumMentionPerson } from "./MentionTextArea";
import { ForumBadges, ForumMarkdown, ForumModerationBanner, ForumVoteButtons } from "./parts";

/** Write a comment, a reply, or edit one. Held comments are posted and only their author sees them until review. */
export function ForumCommentForm({
  postId,
  parentId,
  editing,
  autoFocus,
  onDone,
}: {
  postId: string;
  parentId?: string;
  editing?: ForumComment;
  autoFocus?: boolean;
  onDone?: () => void;
}) {
  const create = useCreateForumComment();
  const update = useUpdateForumComment();
  const [body, setBody] = useState(editing?.body ?? "");
  const [people, setPeople] = useState<ForumMentionPerson[]>(() => editing?.mentions ?? []);
  const busy = create.isPending || update.isPending;
  const over = body.length > FORUM_LIMITS.commentChars;

  const submit = async () => {
    if (!body.trim() || over || busy) return;
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
    <div className="flex flex-col gap-2">
      <ForumMentionTextArea
        value={body}
        onChange={setBody}
        people={people}
        onPeopleChange={setPeople}
        aria-label={editing ? "Edit comment" : parentId ? "Reply" : "Comment"}
        placeholder={parentId ? "Write a reply" : "Write a comment"}
        minRows={parentId || editing ? 2 : 3}
        autoFocus={!!autoFocus}
        onSubmit={() => void submit()}
      />
      <div className="flex items-center justify-end gap-2">
        {over ? <span className="mr-auto type-caption text-danger-strong">{`${body.length - FORUM_LIMITS.commentChars} characters over`}</span> : null}
        {onDone && (parentId || editing) ? (
          <Button variant="ghost" size="sm" onClick={onDone} disabled={busy}>
            Cancel
          </Button>
        ) : null}
        <Button size="sm" loading={busy} disabled={!body.trim() || over} onClick={() => void submit()}>
          {editing ? "Save" : parentId ? "Reply" : "Comment"}
        </Button>
      </div>
    </div>
  );
}

function CommentMenu({ c, post, onEdit }: { c: ForumComment; post: ForumPost; onEdit: () => void }) {
  const me = useMe();
  const del = useDeleteForumComment();
  const moderate = useModerateForumComment();
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [reporting, setReporting] = useState(false);
  const [blocking, setBlocking] = useState(false);
  if (!c.author) return null;
  const mine = me.data?.id === c.author.id;
  const staff = !!me.data?.admin_role;
  const name = c.author.display_name || "this person";

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(`${window.location.origin}/forum/posts/${post.id}?comment=${c.id}`);
      toast.success("Link copied");
    } catch {
      toast.error("Couldn't copy the link.");
    }
  };
  const restore = () =>
    moderate.mutate({ id: c.id, postId: post.id, action: "restore" }, { onSuccess: () => toast.success("Comment restored"), onError: (e) => toast.error(errorMessage(e, "Couldn't restore it. Try again.")) });

  return (
    <>
      <Menu align="end" trigger={<IconButton label="Comment options" icon={<MoreHorizontal />} variant="ghost" size="sm" />}>
        <MenuItem icon={<Link2 />} onSelect={() => void copy()}>
          Copy link
        </MenuItem>
        {mine && c.status === "visible" ? (
          <MenuItem icon={<Pencil />} onSelect={onEdit}>
            Edit
          </MenuItem>
        ) : null}
        {!mine ? (
          <>
            <MenuItem icon={<Flag />} onSelect={() => setReporting(true)}>
              Report comment
            </MenuItem>
            <MenuItem icon={<Ban />} onSelect={() => setBlocking(true)}>
              {`Block ${name}`}
            </MenuItem>
          </>
        ) : null}
        {post.can_moderate && c.status === "visible" && !mine ? (
          <>
            <MenuSeparator />
            <MenuItem icon={<XCircle />} destructive onSelect={() => setConfirmRemove(true)}>
              Remove comment
            </MenuItem>
          </>
        ) : null}
        {staff && c.status === "removed" ? (
          <MenuItem icon={<RotateCcw />} onSelect={restore}>
            Restore comment
          </MenuItem>
        ) : null}
        {mine ? (
          <>
            <MenuSeparator />
            <MenuItem icon={<Trash2 />} destructive onSelect={() => setConfirmDelete(true)}>
              Delete
            </MenuItem>
          </>
        ) : null}
      </Menu>
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title="Delete this comment?"
        tone="danger"
        confirmLabel="Delete"
        onConfirm={async () => {
          await del.mutateAsync({ id: c.id, postId: post.id });
          toast.success("Comment deleted");
        }}
        errorText={(e) => errorMessage(e, "Couldn't delete the comment. Try again.")}
      >
        Replies stay under a placeholder. It can't be undone.
      </ConfirmDialog>
      <ConfirmDialog
        open={confirmRemove}
        onOpenChange={setConfirmRemove}
        title="Remove this comment?"
        tone="danger"
        confirmLabel="Remove"
        onConfirm={async () => {
          await moderate.mutateAsync({ id: c.id, postId: post.id, action: "remove" });
          toast.success("Comment removed");
        }}
        errorText={(e) => errorMessage(e, "Couldn't remove the comment. Try again.")}
      >
        Everyone else sees a placeholder. It's written to the audit log.
      </ConfirmDialog>
      {!mine ? (
        <>
          <ReportDialog open={reporting} onOpenChange={setReporting} targetKind="forum.comment" targetId={c.id} noun="comment" author={{ id: c.author.id, name }} />
          <BlockDialog open={blocking} onOpenChange={setBlocking} userId={c.author.id} name={name} />
        </>
      ) : null}
    </>
  );
}

function MoreReplies({ commentId, post, depth }: { commentId: string; post: ForumPost; depth: number }) {
  const replies = useForumReplies(commentId);
  return <CommentList comments={replies} post={post} depth={depth} />;
}

function CommentItem({ c, post, depth }: { c: ForumComment; post: ForumPost; depth: number }) {
  const me = useMe();
  const [collapsed, setCollapsed] = useState(false);
  const [replying, setReplying] = useState(false);
  const [editing, setEditing] = useState(false);
  const [more, setMore] = useState(false);
  const name = c.author?.display_name || "Unnamed";
  const mine = !!c.author && me.data?.id === c.author.id;
  const canReply = post.status === "visible" && c.status === "visible" && (!post.locked || post.can_moderate);
  const hasReplies = c.replies.length > 0 || c.more_replies > 0;
  const indent = depth < 6;

  return (
    <li id={`comment-${c.id}`} className="flex scroll-mt-24 flex-col gap-2">
      {!c.author ? (
        <div className="flex items-center gap-2">
          <p className="type-label text-fg-muted">{c.status === "deleted" ? "Comment deleted" : "Comment removed"}</p>
          {hasReplies ? (
            <IconButton label={collapsed ? "Show replies" : "Hide replies"} aria-expanded={!collapsed} icon={collapsed ? <Plus /> : <Minus />} variant="ghost" size="sm" onClick={() => setCollapsed((x) => !x)} />
          ) : null}
        </div>
      ) : (
        <>
          <div className="flex min-w-0 items-center gap-2 type-caption text-fg-muted">
            <AppAvatar name={name} src={c.author.avatar_url} size="xs" />
            <span className="truncate font-semibold text-fg">{name}</span>
            <time dateTime={c.created_at} title={formatDateTime(c.created_at)} className="shrink-0">
              {formatRelative(c.created_at)}
            </time>
            {c.edited_at ? <span className="shrink-0">Edited</span> : null}
            <ForumBadges status={c.status} labels={c.labels} />
            <IconButton
              label={collapsed ? "Expand comment" : "Collapse comment"}
              aria-expanded={!collapsed}
              icon={collapsed ? <Plus /> : <Minus />}
              variant="ghost"
              size="sm"
              className="ml-auto"
              onClick={() => setCollapsed((x) => !x)}
            />
          </div>
          {collapsed ? null : (
            <>
              {mine ? <ForumModerationBanner status={c.status} reasons={c.moderation_reasons} noun="comment" /> : null}
              {editing ? <ForumCommentForm postId={post.id} editing={c} autoFocus onDone={() => setEditing(false)} /> : <ForumMarkdown text={c.body} mentions={c.mentions} />}
              <div className="flex flex-wrap items-center gap-1">
                <ForumVoteButtons kind="comment" id={c.id} postId={post.id} authorId={c.author.id} score={c.score} vote={c.viewer_vote} disabled={c.status !== "visible"} />
                {canReply ? (
                  <Button variant="ghost" size="sm" leftIcon={<Reply />} className="text-fg-muted" onClick={() => setReplying((r) => !r)}>
                    Reply
                  </Button>
                ) : null}
                <CommentMenu c={c} post={post} onEdit={() => setEditing(true)} />
              </div>
              {replying ? <ForumCommentForm postId={post.id} parentId={c.id} autoFocus onDone={() => setReplying(false)} /> : null}
            </>
          )}
        </>
      )}
      {!collapsed && hasReplies ? (
        <div className={cn("flex flex-col gap-3", indent && "border-l border-border pl-3 sm:pl-4")}>
          {more ? (
            <MoreReplies commentId={c.id} post={post} depth={depth + 1} />
          ) : (
            <>
              <ul className="flex flex-col gap-5">
                {c.replies.map((r) => (
                  <CommentItem key={r.id} c={r} post={post} depth={depth + 1} />
                ))}
              </ul>
              {c.more_replies > 0 ? (
                <Button variant="ghost" size="sm" leftIcon={<MessageSquare />} className="self-start" onClick={() => setMore(true)}>
                  {forumCount(c.more_replies, "more reply", "more replies")}
                </Button>
              ) : null}
            </>
          )}
        </div>
      ) : null}
    </li>
  );
}

function CommentsSkeleton() {
  return (
    <SkeletonGroup label="Loading comments" className="flex flex-col gap-5">
      {[0, 1, 2].map((i) => (
        <div key={i} className="flex flex-col gap-2">
          <Skeleton className="h-4 w-40" />
          <SkeletonText lines={2} />
        </div>
      ))}
    </SkeletonGroup>
  );
}

/** A page of comments (top level, or one comment's replies), each with its thread. */
export function CommentList({ comments, post, depth = 0 }: { comments: PagedQueryResult<ForumComment>; post: ForumPost; depth?: number }) {
  const items = comments.data?.items ?? [];
  if (comments.isPending) return <CommentsSkeleton />;
  if (comments.isError) return <ErrorState compact title="Couldn't load comments" info={comments.error.message} onRetry={() => void comments.refetch()} retrying={comments.isFetching} />;
  if (items.length === 0) return depth === 0 ? <EmptyState compact icon={<MessageSquare />} title="No comments yet" /> : null;
  return (
    <>
      <ul className="flex flex-col gap-5">
        {items.map((c) => (
          <CommentItem key={c.id} c={c} post={post} depth={depth} />
        ))}
      </ul>
      <LoadMore hasNextPage={comments.hasNextPage} isFetchingNextPage={comments.isFetchingNextPage} fetchNextPage={comments.fetchNextPage} />
    </>
  );
}
