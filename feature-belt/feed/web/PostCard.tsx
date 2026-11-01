"use client";

import { errorMessage, feedKeys, isPinned, useDeleteFeedPost, useMe, useQueryClient, useReactToFeedPost, type FeedPost } from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, Dialog, IconButton, Menu, MenuItem, MenuSeparator, Skeleton, SkeletonText, toast } from "@scaffoldapp/ui-web";
import { Ban, Flag, Link2, Megaphone, MessageCircle, MoreHorizontal, Pencil, Pin, Trash2 } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { BlockDialog, ReportDialog } from "@/features/moderation";
import { formatDateTime, formatRelative } from "@/lib/format";
import { Composer } from "./Composer";
import { MediaGrid, ReactButton, ReactionSummary, RichText } from "./parts";

export function postPath(id: string): string {
  return `/feed/posts/${id}`;
}

function hostOf(url: string): string {
  const m = /^https?:\/\/([^/?#]+)/i.exec(url);
  return m?.[1] ?? url;
}

/** One post: author line, text, link, media, reactions and the comments link (left out on the post's own page). */
export function PostCard({ post, detail }: { post: FeedPost; detail?: boolean }) {
  const me = useMe();
  const qc = useQueryClient();
  const react = useReactToFeedPost();
  const del = useDeleteFeedPost();
  const [editing, setEditing] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [reporting, setReporting] = useState(false);
  const [blocking, setBlocking] = useState(false);
  const mine = me.data?.id === post.author.id;
  const name = post.author.display_name || "Deleted user";
  const pinned = isPinned(post);
  const announcement = post.kind === "announcement";

  const copyLink = async () => {
    try {
      await navigator.clipboard.writeText(`${window.location.origin}${postPath(post.id)}`);
      toast.success("Link copied");
    } catch {
      toast.error("Couldn't copy the link.");
    }
  };

  const hidden = () => void qc.invalidateQueries({ queryKey: feedKeys.all() });

  return (
    <Card padding="card" variant={pinned ? "raised" : "surface"} className="flex flex-col gap-3" aria-label={`Post by ${name}`}>
      <div className="flex items-start gap-3">
        <AppAvatar name={name} src={post.author.avatar_url} size="md" />
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <span className="truncate type-label font-semibold text-fg">{name}</span>
            {announcement ? (
              <Badge tone="primary" size="sm">
                {pinned ? <Pin className="size-3" aria-hidden /> : <Megaphone className="size-3" aria-hidden />}
                {pinned ? "Pinned" : "Announcement"}
              </Badge>
            ) : null}
          </span>
          <span className="type-caption text-fg-muted">
            <time dateTime={post.created_at} title={formatDateTime(post.created_at)}>
              {formatRelative(post.created_at)}
            </time>
            {post.edited_at ? <span title={formatDateTime(post.edited_at)}> · Edited</span> : null}
          </span>
        </div>
        <Menu align="end" trigger={<IconButton label="Post options" icon={<MoreHorizontal />} variant="ghost" size="sm" />}>
          <MenuItem icon={<Link2 />} onSelect={() => void copyLink()}>
            Copy link
          </MenuItem>
          {mine ? (
            <>
              <MenuItem icon={<Pencil />} onSelect={() => setEditing(true)}>
                Edit
              </MenuItem>
              <MenuSeparator />
              <MenuItem icon={<Trash2 />} destructive onSelect={() => setConfirmDelete(true)}>
                Delete
              </MenuItem>
            </>
          ) : (
            <>
              <MenuSeparator />
              <MenuItem icon={<Flag />} onSelect={() => setReporting(true)}>
                Report post
              </MenuItem>
              <MenuItem icon={<Ban />} destructive onSelect={() => setBlocking(true)}>
                Block {name}
              </MenuItem>
            </>
          )}
        </Menu>
      </div>

      <RichText text={post.body} mentions={post.mentions} />

      {post.link_url ? (
        <a
          href={post.link_url}
          target="_blank"
          rel="noopener noreferrer nofollow ugc"
          className="flex min-h-row items-center gap-3 rounded-md border border-border px-3 type-label text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          <Link2 className="size-4 shrink-0 text-fg-muted" aria-hidden />
          <span className="min-w-0 flex-1 truncate">{post.link_url}</span>
          <span className="hidden type-caption text-fg-muted sm:inline">{hostOf(post.link_url)}</span>
        </a>
      ) : null}

      {post.media.length > 0 ? <MediaGrid media={post.media} /> : null}

      <div className="flex flex-wrap items-center gap-2 border-t border-border pt-2">
        <ReactButton
          mine={post.my_reaction}
          onReact={(reaction) => react.mutate({ post, reaction }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save your reaction. Try again.")) })}
        />
        {detail ? null : (
          <Button asChild variant="ghost" size="sm" leftIcon={<MessageCircle />} className="text-fg-muted">
            <Link href={postPath(post.id)}>{post.comment_count === 1 ? "1 comment" : post.comment_count > 0 ? `${post.comment_count} comments` : "Comment"}</Link>
          </Button>
        )}
        <span className="flex-1" />
        <ReactionSummary reactions={post.reactions} total={post.reaction_count} />
      </div>

      <Dialog open={editing} onOpenChange={setEditing} title="Edit post" size="lg">
        {editing ? <Composer post={post} autoFocus onDone={() => setEditing(false)} /> : null}
      </Dialog>
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title="Delete this post?"
        tone="danger"
        confirmLabel="Delete"
        onConfirm={async () => {
          await del.mutateAsync(post.id);
          toast.success("Post deleted");
        }}
        errorText={(e) => errorMessage(e, "Couldn't delete the post. Try again.")}
      >
        Its comments and reactions go with it. It can't be undone.
      </ConfirmDialog>
      {!mine ? (
        <>
          <ReportDialog
            open={reporting}
            onOpenChange={setReporting}
            targetKind="feed.post"
            targetId={post.id}
            noun="post"
            author={{ id: post.author.id, name }}
            onReported={({ blocked }) => blocked && hidden()}
          />
          <BlockDialog open={blocking} onOpenChange={setBlocking} userId={post.author.id} name={name} onBlocked={hidden} />
        </>
      ) : null}
    </Card>
  );
}

/** Skeleton shaped like a post card. */
export function PostCardSkeleton() {
  return (
    <Card padding="card" className="flex flex-col gap-3" aria-hidden>
      <div className="flex items-center gap-3">
        <Skeleton shape="circle" className="size-10 shrink-0" />
        <span className="flex flex-1 flex-col gap-2">
          <Skeleton className="h-4 w-32" />
          <Skeleton className="h-3 w-20" />
        </span>
      </div>
      <SkeletonText lines={2} />
    </Card>
  );
}
