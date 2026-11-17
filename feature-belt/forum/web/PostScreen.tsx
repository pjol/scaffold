"use client";

import {
  errorMessage,
  FORUM_LIMITS,
  forumCount,
  forumMentionIds,
  useDeleteForumPost,
  useForumComments,
  useForumPost,
  useForumPostTopic,
  useMe,
  useModerateForumPost,
  useUpdateForumPost,
  type ForumCommentSort,
  type ForumPost,
  type ForumPostModAction,
} from "@scaffoldapp/shared";
import { Banner, Button, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, IconButton, Input, Menu, MenuItem, MenuSeparator, Select, Skeleton, SkeletonGroup, SkeletonText, toast } from "@scaffoldapp/ui-web";
import { Ban, ChevronLeft, ExternalLink, FileQuestion, Flag, Link2, Lock, LockOpen, MoreHorizontal, Pencil, Pin, PinOff, RotateCcw, Trash2, XCircle } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { BlockDialog, ReportDialog } from "@/features/moderation";
import { formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { CommentList, ForumCommentForm } from "./CommentTree";
import { ForumMentionTextArea, type ForumMentionPerson } from "./MentionTextArea";
import { ForumBadges, ForumMarkdown, ForumModerationBanner, ForumVoteButtons } from "./parts";
import { forumCommunityPath } from "./PostList";

function EditPostDialog({ post, open, onOpenChange }: { post: ForumPost; open: boolean; onOpenChange: (open: boolean) => void }) {
  const update = useUpdateForumPost();
  const [title, setTitle] = useState(post.title);
  const [body, setBody] = useState(post.body);
  const [people, setPeople] = useState<ForumMentionPerson[]>(post.mentions);
  const tooShort = title.trim().length < FORUM_LIMITS.titleMin;

  const save = async () => {
    try {
      const res = await update.mutateAsync({ id: post.id, title: title.trim(), body, mentions: forumMentionIds(`${title}\n${body}`, people) });
      if (res.moderation.status === "held") toast.warning("Saved. It's waiting for review, so only you can see it for now.");
      else toast.success("Post saved");
      onOpenChange(false);
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't save the post. Try again."));
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !update.isPending && onOpenChange(o)}
      title="Edit post"
      size="lg"
      footer={
        <>
          <Button variant="ghost" size="auto" disabled={update.isPending} onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button size="auto" loading={update.isPending} disabled={tooShort} onClick={() => void save()}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Title">
          <Input value={title} maxLength={FORUM_LIMITS.titleMax} onChange={(e) => setTitle(e.target.value)} />
        </Field>
        <Field label="Text" info="Markdown: **bold**, _italic_, [links](https://…), lists and quotes. Type @ to mention someone.">
          <ForumMentionTextArea value={body} onChange={setBody} people={people} onPeopleChange={setPeople} maxLength={FORUM_LIMITS.postChars} minRows={6} />
        </Field>
      </div>
    </Dialog>
  );
}

function PostMenu({ post }: { post: ForumPost }) {
  const router = useRouter();
  const me = useMe();
  const del = useDeleteForumPost();
  const moderate = useModerateForumPost();
  const [editing, setEditing] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [reporting, setReporting] = useState(false);
  const [blocking, setBlocking] = useState(false);
  const mine = !!post.author && me.data?.id === post.author.id;
  const staff = !!me.data?.admin_role;
  const name = post.author?.display_name || "this person";

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(`${window.location.origin}/forum/posts/${post.id}`);
      toast.success("Link copied");
    } catch {
      toast.error("Couldn't copy the link.");
    }
  };
  const act = (action: ForumPostModAction, done: string) =>
    moderate.mutate({ id: post.id, action }, { onSuccess: () => toast.success(done), onError: (e) => toast.error(errorMessage(e, "Couldn't do that. Try again.")) });

  return (
    <>
      <Menu align="end" trigger={<IconButton label="Post options" icon={<MoreHorizontal />} variant="ghost" size="sm" />}>
        <MenuItem icon={<Link2 />} onSelect={() => void copy()}>
          Copy link
        </MenuItem>
        {mine && post.status === "visible" ? (
          <MenuItem icon={<Pencil />} onSelect={() => setEditing(true)}>
            Edit
          </MenuItem>
        ) : null}
        {!mine && post.author ? (
          <>
            <MenuItem icon={<Flag />} onSelect={() => setReporting(true)}>
              Report post
            </MenuItem>
            <MenuItem icon={<Ban />} onSelect={() => setBlocking(true)}>
              {`Block ${name}`}
            </MenuItem>
          </>
        ) : null}
        {post.can_moderate && post.status === "visible" ? (
          <>
            <MenuSeparator />
            <MenuItem icon={post.pinned ? <PinOff /> : <Pin />} onSelect={() => act(post.pinned ? "unpin" : "pin", post.pinned ? "Post unpinned" : "Post pinned")}>
              {post.pinned ? "Unpin" : "Pin to the community"}
            </MenuItem>
            <MenuItem icon={post.locked ? <LockOpen /> : <Lock />} onSelect={() => act(post.locked ? "reopen" : "lock", post.locked ? "Comments reopened" : "Comments locked")}>
              {post.locked ? "Reopen comments" : "Lock comments"}
            </MenuItem>
            {!mine ? (
              <MenuItem icon={<XCircle />} destructive onSelect={() => setConfirmRemove(true)}>
                Remove post
              </MenuItem>
            ) : null}
          </>
        ) : null}
        {staff && post.status === "removed" ? (
          <MenuItem icon={<RotateCcw />} onSelect={() => act("restore", "Post restored")}>
            Restore post
          </MenuItem>
        ) : null}
        {mine && post.status !== "deleted" ? (
          <>
            <MenuSeparator />
            <MenuItem icon={<Trash2 />} destructive onSelect={() => setConfirmDelete(true)}>
              Delete
            </MenuItem>
          </>
        ) : null}
      </Menu>
      {editing ? <EditPostDialog post={post} open={editing} onOpenChange={setEditing} /> : null}
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title="Delete this post?"
        tone="danger"
        confirmLabel="Delete"
        onConfirm={async () => {
          await del.mutateAsync(post.id);
          toast.success("Post deleted");
          router.push(forumCommunityPath(post.community.slug));
        }}
        errorText={(e) => errorMessage(e, "Couldn't delete the post. Try again.")}
      >
        Its comments stay under a placeholder. It can't be undone.
      </ConfirmDialog>
      <ConfirmDialog
        open={confirmRemove}
        onOpenChange={setConfirmRemove}
        title="Remove this post?"
        tone="danger"
        confirmLabel="Remove"
        onConfirm={async () => {
          await moderate.mutateAsync({ id: post.id, action: "remove" });
          toast.success("Post removed");
        }}
        errorText={(e) => errorMessage(e, "Couldn't remove the post. Try again.")}
      >
        It disappears for everyone but its author and staff. It's written to the audit log.
      </ConfirmDialog>
      {!mine && post.author ? (
        <>
          <ReportDialog open={reporting} onOpenChange={setReporting} targetKind="forum.post" targetId={post.id} noun="post" author={{ id: post.author.id, name }} />
          <BlockDialog open={blocking} onOpenChange={setBlocking} userId={post.author.id} name={name} onBlocked={() => router.push("/forum")} />
        </>
      ) : null}
    </>
  );
}

function BackLink({ href, label }: { href: string; label: string }) {
  return (
    <Link href={href} className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
      <ChevronLeft className="size-4" aria-hidden />
      {label}
    </Link>
  );
}

/** /forum/posts/[id]: the post, its comment composer and the comment tree (sort in the URL); `?comment=` scrolls to one comment. */
export function PostScreen({ id }: { id: string }) {
  useForumPostTopic(id);
  const me = useMe();
  const post = useForumPost(id);
  const [sort, setSort] = useUrlState<ForumCommentSort>("sort", "top");
  const comments = useForumComments(post.isSuccess ? id : null, sort);
  const target = useSearchParams().get("comment");

  useEffect(() => {
    if (!target || !comments.isSuccess) return;
    document.getElementById(`comment-${target}`)?.scrollIntoView({ block: "center" });
  }, [target, comments.isSuccess]);

  const frame = "mx-auto flex w-full max-w-content flex-col gap-2 px-gutter py-section";
  if (post.isPending) {
    return (
      <div className={frame}>
        <SkeletonGroup label="Loading post" className="flex max-w-prose flex-col gap-4">
          <Skeleton className="h-4 w-48" />
          <Skeleton className="h-8 w-3/4" />
          <SkeletonText lines={4} />
        </SkeletonGroup>
      </div>
    );
  }
  if (post.isError) {
    return (
      <div className={frame}>
        <BackLink href="/forum" label="Forum" />
        {post.error.status === 404 ? (
          <EmptyState icon={<FileQuestion />} title="Post not found" />
        ) : (
          <ErrorState title="Couldn't load this post" info={post.error.message} onRetry={() => void post.refetch()} retrying={post.isFetching} />
        )}
      </div>
    );
  }

  const p = post.data;
  const name = p.author?.display_name || "Deleted user";
  const mine = !!p.author && me.data?.id === p.author.id;
  const canComment = (p.status === "visible" || (p.status === "held" && mine)) && (!p.locked || p.can_moderate);

  return (
    <div className={frame}>
      <BackLink href={forumCommunityPath(p.community.slug)} label={p.community.title} />
      <div className="flex max-w-prose flex-col gap-section">
        <article className="flex flex-col gap-4">
          {p.moderation_reasons.length > 0 || p.status === "held" || p.status === "removed" ? <ForumModerationBanner status={p.status} reasons={p.moderation_reasons} noun="post" /> : null}
          <header className="flex gap-3">
            <ForumVoteButtons kind="post" id={p.id} postId={p.id} authorId={p.author?.id} score={p.score} vote={p.viewer_vote} disabled={p.status !== "visible"} vertical />
            <div className="flex min-w-0 flex-1 flex-col gap-2">
              <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 type-caption text-fg-muted">
                {p.author ? (
                  <span className="flex min-w-0 items-center gap-1">
                    <AppAvatar name={name} src={p.author.avatar_url} size="xs" />
                    <span className="truncate font-semibold text-fg">{name}</span>
                  </span>
                ) : null}
                <time dateTime={p.created_at} title={formatDateTime(p.created_at)}>
                  {formatRelative(p.created_at)}
                </time>
                {p.edited_at ? <span title={formatDateTime(p.edited_at)}>Edited</span> : null}
              </span>
              <h1 className="break-words type-title text-fg">{p.title}</h1>
              <ForumBadges pinned={p.pinned} locked={p.locked} status={p.status} labels={p.labels} />
            </div>
            <PostMenu post={p} />
          </header>
          {p.kind === "link" && p.url ? (
            <a
              href={p.url}
              target="_blank"
              rel="noopener noreferrer nofollow ugc"
              className="flex min-h-row items-center gap-3 rounded-md border border-border px-3 type-label text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              <ExternalLink className="size-4 shrink-0 text-fg-muted" aria-hidden />
              <span className="min-w-0 flex-1 truncate">{p.url}</span>
              <span className="hidden type-caption text-fg-muted sm:inline">{p.domain}</span>
            </a>
          ) : null}
          {p.status === "deleted" ? <p className="type-body text-fg-muted">The author deleted this post.</p> : <ForumMarkdown text={p.body} mentions={p.mentions} />}
        </article>

        <section aria-label="Comments" className="flex flex-col gap-4">
          <div className="flex items-center gap-3">
            <h2 className="flex-1 type-title-sm text-fg">{forumCount(p.comment_count, "comment", "comments")}</h2>
            <Select
              aria-label="Sort comments"
              value={sort}
              onValueChange={(v) => setSort(v as ForumCommentSort)}
              options={[
                { value: "top", label: "Top" },
                { value: "new", label: "Newest" },
                { value: "old", label: "Oldest" },
              ]}
              className="w-36"
            />
          </div>
          {canComment ? <ForumCommentForm postId={p.id} /> : p.locked ? <Banner tone="info">Comments are locked on this post.</Banner> : null}
          <CommentList comments={comments} post={p} />
        </section>
      </div>
    </div>
  );
}
