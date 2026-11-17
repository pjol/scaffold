"use client";

import { useForumCommunity, useForumPosts, type ForumPostSort, type ForumTopPeriod } from "@scaffoldapp/shared";
import { Badge, Button, EmptyState, ErrorState, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { ChevronLeft, FileQuestion, Lock, MessagesSquare, SquarePen } from "lucide-react";
import Link from "next/link";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { ForumPostList, ForumPostRow, ForumSortBar } from "./PostList";

function BackToForum() {
  return (
    <Link href="/forum" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
      <ChevronLeft className="size-4" aria-hidden />
      Forum
    </Link>
  );
}

/** /forum/c/[slug]: one community's pinned posts and feed, its rules and moderators. */
export function CommunityScreen({ slug }: { slug: string }) {
  const community = useForumCommunity(slug);
  const [sort, setSort] = useUrlState<ForumPostSort>("sort", "hot");
  const [period, setPeriod] = useUrlState<ForumTopPeriod>("t", "week");
  const posts = useForumPosts({ community: slug, sort, ...(sort === "top" ? { t: period } : {}) }, { enabled: community.isSuccess });

  const frame = "mx-auto flex w-full max-w-content flex-col gap-2 px-gutter py-section";
  if (community.isPending) {
    return (
      <div className={frame}>
        <BackToForum />
        <SkeletonGroup label="Loading community" className="flex flex-col gap-section">
          <Skeleton className="h-10 w-64" />
          <Skeleton className="h-40" />
        </SkeletonGroup>
      </div>
    );
  }
  if (community.isError) {
    return (
      <div className={frame}>
        <BackToForum />
        {community.error.status === 404 ? (
          <EmptyState icon={<FileQuestion />} title="Community not found" />
        ) : (
          <ErrorState title="Couldn't load this community" info={community.error.message} onRetry={() => void community.refetch()} retrying={community.isFetching} />
        )}
      </div>
    );
  }

  const c = community.data;
  const canPost = !c.locked || c.can_moderate;
  return (
    <div className={frame}>
      <BackToForum />
      <div className="flex flex-col gap-section">
        <PageHeader
          title={
            <span className="flex min-w-0 items-center gap-2">
              <span className="truncate">{c.title}</span>
              {c.locked ? (
                <Badge tone="neutral">
                  <Lock className="size-3" aria-hidden />
                  Locked
                </Badge>
              ) : null}
            </span>
          }
          {...(c.description ? { info: c.description } : {})}
          action={
            canPost ? (
              <Button asChild size="auto" leftIcon={<SquarePen />}>
                <Link href={`/forum/submit?community=${encodeURIComponent(c.slug)}`}>New post</Link>
              </Button>
            ) : undefined
          }
        />
        <div className="grid gap-section lg:grid-cols-3">
          <div className="flex min-w-0 flex-col gap-section lg:col-span-2">
            {c.pinned.length > 0 ? (
              <Section title="Pinned">
                <ul className="flex flex-col divide-y divide-border">
                  {c.pinned.map((p) => (
                    <li key={p.id}>
                      <ForumPostRow post={p} showCommunity={false} />
                    </li>
                  ))}
                </ul>
              </Section>
            ) : null}
            <section aria-label="Posts" className="flex flex-col gap-4">
              <ForumSortBar sort={sort} period={period} onSort={setSort} onPeriod={setPeriod} />
              <ForumPostList
                posts={posts}
                showCommunity={false}
                empty={
                  <EmptyState
                    compact
                    icon={<MessagesSquare />}
                    title="No posts here yet"
                    {...(canPost
                      ? {
                          action: (
                            <Button asChild variant="secondary" size="auto">
                              <Link href={`/forum/submit?community=${encodeURIComponent(c.slug)}`}>Write the first post</Link>
                            </Button>
                          ),
                        }
                      : {})}
                  />
                }
              />
            </section>
          </div>
          <div className="flex flex-col gap-section">
            <Section title="Rules">
              <ol className="flex list-decimal flex-col gap-2 pl-5 type-label text-fg">
                {c.rules.map((r, i) => (
                  <li key={i}>{r}</li>
                ))}
              </ol>
            </Section>
            {c.moderators.length > 0 ? (
              <Section title="Moderators" info="They pin, lock and remove posts here. Staff review reports and held posts.">
                <ul className="flex flex-col gap-2">
                  {c.moderators.map((m) => (
                    <li key={m.id} className="flex items-center gap-2 type-label text-fg">
                      <AppAvatar name={m.display_name || "?"} src={m.avatar_url} size="xs" />
                      <span className="truncate">{m.display_name || "Unnamed"}</span>
                    </li>
                  ))}
                </ul>
              </Section>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  );
}
