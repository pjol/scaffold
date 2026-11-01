"use client";

import { useFeedPinned, useFeedPosts, useFeedTopic, useMarkFeedSeen, useMe, useRealtimeEvent } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, Presence, Stagger, StaggerItem } from "@scaffoldapp/ui-web";
import { ArrowUp, Newspaper, SearchX } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { Composer } from "./Composer";
import { PostCard, PostCardSkeleton } from "./PostCard";

/**
 * /feed: composer, pinned announcements, then posts newest first (keyset pages). New
 * posts from others show a "Show new posts" button instead of shifting the list; opening
 * the feed clears the unread dot. `?q=` searches.
 */
export function FeedScreen() {
  useFeedTopic();
  const me = useMe();
  const [q, setQ] = useUrlState("q", "");
  const pinned = useFeedPinned({ enabled: !q });
  const posts = useFeedPosts({ q: q || undefined });
  const markSeen = useMarkFeedSeen();
  const [incoming, setIncoming] = useState(0);
  const composer = useRef<HTMLDivElement>(null);
  const seen = useRef(false);

  useRealtimeEvent("feed.post", (e) => {
    if (e.data.author_id !== me.data?.id && e.data.kind === "post") setIncoming((n) => n + 1);
  });

  useEffect(() => {
    if (posts.isSuccess && !seen.current) {
      seen.current = true;
      markSeen.mutate();
    }
  }, [posts.isSuccess, markSeen]);

  const showNew = async () => {
    setIncoming(0);
    window.scrollTo({ top: 0, behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
    await posts.refetch();
    markSeen.mutate();
  };

  const focusComposer = () => composer.current?.querySelector("textarea")?.focus();
  const items = posts.data?.items ?? [];
  const announcements = q ? [] : (pinned.data?.items ?? []);

  return (
    <div className="mx-auto flex w-full max-w-content flex-col px-gutter py-section">
      <div className="mx-auto flex w-full max-w-prose flex-col gap-6">
        <PageHeader title="Feed" />
        <SearchInput value={q} onSearch={setQ} placeholder="Search posts" label="Search posts" />

        {!q ? (
          <Card padding="card" ref={composer}>
            <Composer />
          </Card>
        ) : null}

        <Presence show={incoming > 0}>
          <div className="sticky top-20 z-10 flex justify-center">
            <Button variant="primary" size="sm" leftIcon={<ArrowUp />} onClick={() => void showNew()} className="shadow-md">
              {incoming === 1 ? "Show 1 new post" : `Show ${incoming} new posts`}
            </Button>
          </div>
        </Presence>

        {announcements.length > 0 ? (
          <section aria-label="Announcements" className="flex flex-col gap-4">
            {announcements.map((p) => (
              <PostCard key={p.id} post={p} />
            ))}
          </section>
        ) : null}

        <section aria-label={q ? "Search results" : "Posts"} className="flex flex-col gap-4">
          {posts.isPending ? (
            <>
              <PostCardSkeleton />
              <PostCardSkeleton />
              <PostCardSkeleton />
            </>
          ) : posts.isError ? (
            <Card padding="compact">
              <ErrorState title="Couldn't load the feed" info={posts.error.message} onRetry={() => void posts.refetch()} retrying={posts.isFetching} />
            </Card>
          ) : items.length === 0 ? (
            <Card padding="compact">
              {q ? (
                <EmptyState
                  icon={<SearchX />}
                  title="No posts match"
                  action={
                    <Button variant="secondary" size="auto" onClick={() => setQ(null)}>
                      Clear search
                    </Button>
                  }
                />
              ) : (
                <EmptyState
                  icon={<Newspaper />}
                  title="No posts yet"
                  action={
                    <Button variant="secondary" size="auto" onClick={focusComposer}>
                      Write the first post
                    </Button>
                  }
                />
              )}
            </Card>
          ) : (
            <>
              <Stagger as="ul" className="flex flex-col gap-4">
                {items.map((p) => (
                  <StaggerItem as="li" key={p.id}>
                    <PostCard post={p} />
                  </StaggerItem>
                ))}
              </Stagger>
              <LoadMore hasNextPage={posts.hasNextPage} isFetchingNextPage={posts.isFetchingNextPage} fetchNextPage={posts.fetchNextPage} label={items.length > 10 ? "You're all caught up" : undefined} />
            </>
          )}
        </section>
      </div>
    </div>
  );
}
