"use client";

import { FORUM_TOP_PERIODS, forumCount, type ForumPostSort, type ForumPostSummary, type ForumTopPeriod, type PagedQueryResult } from "@scaffoldapp/shared";
import { EmptyState, ErrorState, SegmentedControl, Select, Skeleton, SkeletonGroup, SkeletonText } from "@scaffoldapp/ui-web";
import { ExternalLink, MessageSquare, MessagesSquare } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { formatDateTime, formatRelative } from "@/lib/format";
import { ForumBadges, ForumVoteButtons } from "./parts";

export function forumPostPath(id: string): string {
  return `/forum/posts/${id}`;
}

export function forumCommunityPath(slug: string): string {
  return `/forum/c/${slug}`;
}

/** One post in a list: votes, title (a link post shows its domain), where and by whom, excerpt, comments. */
export function ForumPostRow({ post, showCommunity = true }: { post: ForumPostSummary; showCommunity?: boolean }) {
  const name = post.author?.display_name || "Deleted user";
  return (
    <article className="flex gap-3 py-4" aria-label={post.title}>
      <ForumVoteButtons kind="post" id={post.id} postId={post.id} authorId={post.author?.id} score={post.score} vote={post.viewer_vote} disabled={post.status !== "visible"} vertical />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 type-caption text-fg-muted">
          {showCommunity ? (
            <Link href={forumCommunityPath(post.community.slug)} className="font-semibold text-fg hover:underline">
              {post.community.title}
            </Link>
          ) : null}
          {post.author ? (
            <span className="flex min-w-0 items-center gap-1">
              <AppAvatar name={name} src={post.author.avatar_url} size="xs" />
              <span className="truncate">{name}</span>
            </span>
          ) : null}
          <time dateTime={post.created_at} title={formatDateTime(post.created_at)}>
            {formatRelative(post.created_at)}
          </time>
        </span>
        <Link href={forumPostPath(post.id)} className="rounded-sm type-body font-semibold text-fg hover:underline focus-visible:outline-2 focus-visible:outline-ring">
          {post.title}
        </Link>
        {post.kind === "link" && post.domain ? (
          <a href={post.url ?? undefined} target="_blank" rel="noopener noreferrer nofollow ugc" className="flex items-center gap-1 self-start type-caption text-primary-strong hover:underline">
            {post.domain}
            <ExternalLink className="size-3" aria-hidden />
          </a>
        ) : null}
        <ForumBadges pinned={post.pinned} locked={post.locked} status={post.status} labels={post.labels} />
        {post.excerpt ? <p className="line-clamp-2 max-w-prose break-words type-label text-fg-muted">{post.excerpt}</p> : null}
        <Link href={forumPostPath(post.id)} className="flex min-h-control items-center gap-1 self-start type-label text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
          <MessageSquare className="size-4" aria-hidden />
          {forumCount(post.comment_count, "comment", "comments")}
        </Link>
      </div>
    </article>
  );
}

function RowSkeleton() {
  return (
    <div className="flex gap-3 py-4" aria-hidden>
      <Skeleton className="h-20 w-8" />
      <div className="flex flex-1 flex-col gap-2">
        <Skeleton className="h-3 w-40" />
        <Skeleton className="h-5 w-3/4" />
        <SkeletonText lines={2} />
      </div>
    </div>
  );
}

/** Hot / New / Top, with the period for Top. */
export function ForumSortBar({ sort, period, onSort, onPeriod }: { sort: ForumPostSort; period: ForumTopPeriod; onSort: (s: ForumPostSort) => void; onPeriod: (t: ForumTopPeriod) => void }) {
  return (
    <div className="flex flex-wrap items-center gap-3">
      <SegmentedControl<ForumPostSort>
        aria-label="Sort posts"
        fullWidth={false}
        value={sort}
        onValueChange={onSort}
        options={[
          { value: "hot", label: "Hot" },
          { value: "new", label: "New" },
          { value: "top", label: "Top" },
        ]}
      />
      {sort === "top" ? (
        <Select aria-label="Period" value={period} onValueChange={(v) => onPeriod(v as ForumTopPeriod)} options={FORUM_TOP_PERIODS.map((p) => ({ value: p.value, label: p.label }))} className="w-40" />
      ) : null}
    </div>
  );
}

/** A page of posts with skeleton, error and empty states, then Load more. */
export function ForumPostList({ posts, showCommunity, empty }: { posts: PagedQueryResult<ForumPostSummary>; showCommunity?: boolean; empty?: ReactNode }) {
  const items = posts.data?.items ?? [];
  if (posts.isPending) {
    return (
      <SkeletonGroup label="Loading posts" className="flex flex-col divide-y divide-border">
        <RowSkeleton />
        <RowSkeleton />
        <RowSkeleton />
      </SkeletonGroup>
    );
  }
  if (posts.isError) {
    return <ErrorState compact title="Couldn't load posts" info={posts.error.message} onRetry={() => void posts.refetch()} retrying={posts.isFetching} />;
  }
  if (items.length === 0) return <>{empty ?? <EmptyState compact icon={<MessagesSquare />} title="No posts yet" />}</>;
  return (
    <>
      <ul className="flex flex-col divide-y divide-border">
        {items.map((p) => (
          <li key={p.id}>
            <ForumPostRow post={p} {...(showCommunity === undefined ? {} : { showCommunity })} />
          </li>
        ))}
      </ul>
      <LoadMore hasNextPage={posts.hasNextPage} isFetchingNextPage={posts.isFetchingNextPage} fetchNextPage={posts.fetchNextPage} />
    </>
  );
}
