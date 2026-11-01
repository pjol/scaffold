"use client";

import {
  buildMentions,
  errorMessage,
  FEED_LIMITS,
  feedKeys,
  mentionedPeople,
  threadComments,
  useCreateFeedComment,
  useDeleteFeedComment,
  useFeedComments,
  useFeedPost,
  useFeedTopic,
  useMe,
  useQueryClient,
  useReactToFeedComment,
  useUpdateFeedComment,
  type FeedComment,
  type FeedPost,
} from "@scaffoldapp/shared";
import { Button, Card, cn, ConfirmDialog, EmptyState, ErrorState, IconButton, Menu, MenuItem, MenuSeparator, Skeleton, SkeletonText, toast } from "@scaffoldapp/ui-web";
import { ArrowLeft, Ban, Flag, MessageCircle, MoreHorizontal, Pencil, Reply, Trash2, X } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useRef, useState, type RefObject } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { BlockDialog, ReportDialog } from "@/features/moderation";
import { formatDateTime, formatRelative } from "@/lib/format";
import { MentionTextArea, type MentionPerson } from "./MentionTextArea";
import { ReactButton, ReactionSummary, RichText } from "./parts";
import { PostCard, PostCardSkeleton } from "./PostCard";

const PLACEHOLDER: Record<Exclude<FeedComment["status"], "visible">, string> = {
  deleted: "Comment deleted",
  removed: "Removed by moderators",
  hidden: "Hidden",
};

function CommentForm({
  post,
  replyTo,
  onCancelReply,
  editing,
  onDone,
  inputRef,
}: {
  post: FeedPost;
  replyTo?: FeedComment | null;
  onCancelReply?: () => void;
  editing?: FeedComment;
  onDone?: () => void;
  inputRef?: RefObject<HTMLTextAreaElement | null>;
}) {
  const create = useCreateFeedComment();
  const update = useUpdateFeedComment();
  const [body, setBody] = useState(editing?.body ?? "");
  const [people, setPeople] = useState<MentionPerson[]>(() => (editing ? mentionedPeople(editing.mentions) : []));
  const busy = create.isPending || update.isPending;
  const over = body.length > FEED_LIMITS.commentChars;

  const submit = async () => {
    if (!body.trim() || over || busy) return;
    const mentions = buildMentions(body, people);
    try {
      if (editing) {
        await update.mutateAsync({ id: editing.id, body, mentions });
      } else {
        await create.mutateAsync({ postId: post.id, body, mentions, parent_id: replyTo?.parent_id ?? replyTo?.id ?? null });
        setBody("");
        setPeople([]);
      }
      onDone?.();
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't save your comment. Try again."));
    }
  };

  const replyName = replyTo?.author?.display_name;
  return (
    <form
      className="flex flex-col gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      {replyTo ? (
        <span className="flex min-h-control-sm items-center gap-2 type-caption text-fg-muted">
          <Reply className="size-4" aria-hidden />
          <span className="min-w-0 flex-1 truncate">Replying to {replyName || "a comment"}</span>
          <IconButton label="Cancel reply" icon={<X />} size="sm" variant="ghost" onClick={onCancelReply} />
        </span>
      ) : null}
      <MentionTextArea
        ref={inputRef}
        value={body}
        onChange={setBody}
        people={people}
        onPeopleChange={setPeople}
        placeholder={editing ? undefined : replyTo ? "Write a reply" : "Write a comment"}
        aria-label={editing ? "Comment text" : replyTo ? "Reply" : "Comment"}
        minRows={editing ? 2 : 1}
        autoFocus={!!editing}
        onSubmit={() => void submit()}
      />
      <div className="flex items-center justify-end gap-2">
        {over ? <span className="type-caption text-danger-strong">{FEED_LIMITS.commentChars - body.length}</span> : null}
        {editing ? (
          <Button type="button" variant="ghost" size="sm" onClick={onDone}>
            Cancel
          </Button>
        ) : null}
        <Button type="submit" size="sm" loading={busy} disabled={!body.trim() || over}>
          {editing ? "Save" : replyTo ? "Reply" : "Comment"}
        </Button>
      </div>
    </form>
  );
}

function CommentItem({ comment, post, onReply, highlighted }: { comment: FeedComment; post: FeedPost; onReply: (c: FeedComment) => void; highlighted: boolean }) {
  const me = useMe();
  const qc = useQueryClient();
  const react = useReactToFeedComment();
  const del = useDeleteFeedComment();
  const [editing, setEditing] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [reporting, setReporting] = useState(false);
  const [blocking, setBlocking] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (highlighted) ref.current?.scrollIntoView({ block: "center" });
  }, [highlighted]);

  if (comment.status !== "visible" || !comment.author) {
    return <p className="flex min-h-control items-center type-label italic text-fg-muted">{PLACEHOLDER[comment.status === "visible" ? "hidden" : comment.status]}</p>;
  }
  const author = comment.author;
  const name = author.display_name || "Deleted user";
  const mine = me.data?.id === author.id;
  const canDelete = mine || me.data?.id === post.author.id;
  const hidden = () => void qc.invalidateQueries({ queryKey: feedKeys.all() });

  return (
    <div ref={ref} id={`comment-${comment.id}`} className={cn("flex gap-3 rounded-md", highlighted && "bg-primary-soft p-2")}>
      <AppAvatar name={name} src={author.avatar_url} size="sm" />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate type-label font-semibold text-fg">{name}</span>
          <time dateTime={comment.created_at} title={formatDateTime(comment.created_at)} className="type-caption text-fg-muted">
            {formatRelative(comment.created_at)}
          </time>
          {comment.edited_at ? <span className="type-caption text-fg-muted">· Edited</span> : null}
          <span className="flex-1" />
          <Menu align="end" trigger={<IconButton label="Comment options" icon={<MoreHorizontal />} size="sm" variant="ghost" />}>
            {mine ? (
              <MenuItem icon={<Pencil />} onSelect={() => setEditing(true)}>
                Edit
              </MenuItem>
            ) : (
              <>
                <MenuItem icon={<Flag />} onSelect={() => setReporting(true)}>
                  Report comment
                </MenuItem>
                <MenuItem icon={<Ban />} destructive onSelect={() => setBlocking(true)}>
                  Block {name}
                </MenuItem>
              </>
            )}
            {canDelete ? (
              <>
                <MenuSeparator />
                <MenuItem icon={<Trash2 />} destructive onSelect={() => setConfirmDelete(true)}>
                  Delete
                </MenuItem>
              </>
            ) : null}
          </Menu>
        </span>
        {editing ? (
          <CommentForm post={post} editing={comment} onDone={() => setEditing(false)} />
        ) : (
          <>
            <RichText text={comment.body} mentions={comment.mentions} />
            <div className="-ml-3 flex items-center gap-1">
              <ReactButton
                mine={comment.my_reaction}
                onReact={(reaction) => react.mutate({ comment, reaction }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save your reaction. Try again.")) })}
              />
              <Button variant="ghost" size="sm" leftIcon={<Reply />} className="text-fg-muted" onClick={() => onReply(comment)}>
                Reply
              </Button>
              <span className="flex-1" />
              <ReactionSummary reactions={comment.reactions} total={comment.reaction_count} />
            </div>
          </>
        )}
      </div>
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title="Delete this comment?"
        tone="danger"
        confirmLabel="Delete"
        onConfirm={async () => {
          await del.mutateAsync({ id: comment.id, postId: post.id });
          toast.success("Comment deleted");
        }}
        errorText={(e) => errorMessage(e, "Couldn't delete the comment. Try again.")}
      >
        It can't be undone.
      </ConfirmDialog>
      {!mine ? (
        <>
          <ReportDialog
            open={reporting}
            onOpenChange={setReporting}
            targetKind="feed.comment"
            targetId={comment.id}
            noun="comment"
            author={{ id: author.id, name }}
            onReported={({ blocked }) => blocked && hidden()}
          />
          <BlockDialog open={blocking} onOpenChange={setBlocking} userId={author.id} name={name} onBlocked={hidden} />
        </>
      ) : null}
    </div>
  );
}

function Comments({ post }: { post: FeedPost }) {
  const comments = useFeedComments(post.id);
  const [replyTo, setReplyTo] = useState<FeedComment | null>(null);
  const input = useRef<HTMLTextAreaElement | null>(null);
  const highlight = useSearchParams().get("comment");
  const threads = threadComments(comments.data?.items ?? []);

  const reply = (c: FeedComment) => {
    setReplyTo(c);
    requestAnimationFrame(() => input.current?.focus());
  };

  return (
    <section aria-label="Comments" className="flex flex-col gap-3">
      <h2 className="type-title-sm text-fg">Comments</h2>
      <Card padding="card" className="flex flex-col gap-4">
        {comments.isPending ? (
          <div className="flex flex-col gap-4" aria-busy aria-label="Loading comments">
            {[0, 1].map((k) => (
              <div key={k} className="flex gap-3">
                <Skeleton shape="circle" className="size-8 shrink-0" />
                <div className="flex flex-1 flex-col gap-2">
                  <Skeleton className="h-4 w-28" />
                  <SkeletonText lines={2} />
                </div>
              </div>
            ))}
          </div>
        ) : comments.isError ? (
          <ErrorState compact title="Couldn't load comments" info={comments.error.message} onRetry={() => void comments.refetch()} retrying={comments.isFetching} />
        ) : threads.length === 0 ? (
          <EmptyState compact icon={<MessageCircle />} title="No comments yet" />
        ) : (
          <ul className="flex flex-col gap-4">
            {threads.map(({ comment, replies }) => (
              <li key={comment.id} className="flex flex-col gap-3">
                <CommentItem comment={comment} post={post} onReply={reply} highlighted={highlight === comment.id} />
                {replies.length > 0 ? (
                  <ul className="ml-4 flex flex-col gap-3 border-l border-border pl-4" aria-label="Replies">
                    {replies.map((r) => (
                      <li key={r.id}>
                        <CommentItem comment={r} post={post} onReply={reply} highlighted={highlight === r.id} />
                      </li>
                    ))}
                  </ul>
                ) : null}
              </li>
            ))}
          </ul>
        )}
        {comments.hasNextPage ? <LoadMore hasNextPage={comments.hasNextPage} isFetchingNextPage={comments.isFetchingNextPage} fetchNextPage={comments.fetchNextPage} /> : null}
        <div className="border-t border-border pt-4">
          <CommentForm post={post} replyTo={replyTo} onCancelReply={() => setReplyTo(null)} onDone={() => setReplyTo(null)} inputRef={input} />
        </div>
      </Card>
    </section>
  );
}

/** /feed/posts/[id]: the post, then its comments with one level of replies. Notification links open here. */
export function PostScreen({ id }: { id: string }) {
  useFeedTopic();
  const post = useFeedPost(id);

  return (
    <div className="mx-auto flex w-full max-w-content flex-col px-gutter py-section">
      <div className="mx-auto flex w-full max-w-prose flex-col gap-section">
        <div className="flex flex-col gap-4">
          <Button asChild variant="ghost" size="sm" leftIcon={<ArrowLeft />} className="self-start">
            <Link href="/feed">Feed</Link>
          </Button>
          {post.isPending ? <PostCardSkeleton /> : post.data && !post.isError ? <PostCard post={post.data} detail /> : null}
        </div>
        {post.isPending ? null : post.isError ? (
          <Card padding="compact">
            {post.error.status === 404 ? (
              <EmptyState
                icon={<MessageCircle />}
                title="This post isn't available"
                action={
                  <Button asChild variant="secondary" size="auto">
                    <Link href="/feed">Back to the feed</Link>
                  </Button>
                }
              />
            ) : (
              <ErrorState title="Couldn't load the post" info={post.error.message} onRetry={() => void post.refetch()} retrying={post.isFetching} />
            )}
          </Card>
        ) : (
          <Comments post={post.data} />
        )}
      </div>
    </div>
  );
}
