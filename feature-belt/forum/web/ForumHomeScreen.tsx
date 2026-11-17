"use client";

import { useForumCommunities, useForumPosts, type ForumPostSort, type ForumTopPeriod } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, ListGroup, ListRow, SkeletonGroup, SkeletonRow } from "@scaffoldapp/ui-web";
import { MessagesSquare, SquarePen } from "lucide-react";
import Link from "next/link";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { forumCommunityPath, ForumPostList, ForumSortBar } from "./PostList";

function Communities() {
  const communities = useForumCommunities();
  const items = communities.data?.items ?? [];
  return (
    <Section title="Communities">
      {communities.isPending ? (
        <SkeletonGroup label="Loading communities" className="flex flex-col">
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </SkeletonGroup>
      ) : communities.isError ? (
        <ErrorState compact title="Couldn't load communities" info={communities.error.message} onRetry={() => void communities.refetch()} retrying={communities.isFetching} />
      ) : items.length === 0 ? (
        <EmptyState compact icon={<MessagesSquare />} title="No communities yet" />
      ) : (
        <ListGroup>
          <ul>
            {items.map((c) => (
              <li key={c.id}>
                <ListRow asChild title={c.title} trailing={c.posts_7d > 0 ? <span className="type-caption tabular-nums">{`${c.posts_7d} this week`}</span> : undefined}>
                  <Link href={forumCommunityPath(c.slug)} />
                </ListRow>
              </li>
            ))}
          </ul>
        </ListGroup>
      )}
    </Section>
  );
}

/** /forum: the hot feed across every community (or new, or top for a period), and the community list. */
export function ForumHomeScreen() {
  const [sort, setSort] = useUrlState<ForumPostSort>("sort", "hot");
  const [period, setPeriod] = useUrlState<ForumTopPeriod>("t", "week");
  const posts = useForumPosts({ sort, ...(sort === "top" ? { t: period } : {}) });
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <PageHeader
        title="Forum"
        action={
          <Button asChild size="auto" leftIcon={<SquarePen />}>
            <Link href="/forum/submit">New post</Link>
          </Button>
        }
      />
      <div className="grid gap-section lg:grid-cols-3">
        <section aria-label="Posts" className="flex min-w-0 flex-col gap-4 lg:col-span-2">
          <ForumSortBar sort={sort} period={period} onSort={setSort} onPeriod={setPeriod} />
          <ForumPostList
            posts={posts}
            empty={
              <EmptyState
                compact
                icon={<MessagesSquare />}
                title={sort === "top" ? "No posts in this period" : "No posts yet"}
                action={
                  <Button asChild variant="secondary" size="auto">
                    <Link href="/forum/submit">Write the first post</Link>
                  </Button>
                }
              />
            }
          />
        </section>
        <Communities />
      </div>
    </div>
  );
}
