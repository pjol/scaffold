"use client";

import { useSocialFeed, useSocialSuggestions } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState } from "@scaffoldapp/ui-web";
import { Rss, UserSearch } from "lucide-react";
import Link from "next/link";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { ActivityRow, ActivitySkeleton, PersonRow } from "./parts";

/** Shown under an empty feed: a few people to start with. */
function StartWith() {
  const s = useSocialSuggestions(5);
  if (!s.data || s.data.items.length === 0) return null;
  return (
    <Section title="People to follow">
      <ul aria-label="People to follow">
        {s.data.items.map((p) => (
          <PersonRow key={p.user_id} person={p} />
        ))}
      </ul>
    </Section>
  );
}

/** /following: what the people you follow did, newest first (refreshes every minute). */
export function FollowingScreen() {
  const feed = useSocialFeed();
  const items = feed.data?.items ?? [];
  const findPeople = (
    <Button asChild variant="secondary" size="sm" leftIcon={<UserSearch />}>
      <Link href="/people">Find people</Link>
    </Button>
  );
  return (
    <div className="mx-auto flex w-full max-w-content flex-col px-gutter py-section">
      <div className="mx-auto flex w-full max-w-prose flex-col gap-6">
        <PageHeader title="Following" info="Activity from the people you follow, as each of them chose to share it." action={findPeople} />
        {feed.isPending ? (
          <ActivitySkeleton rows={6} label="Loading the feed" />
        ) : feed.isError ? (
          <ErrorState title="Couldn't load the feed" info={feed.error.message} onRetry={() => void feed.refetch()} retrying={feed.isFetching} />
        ) : items.length === 0 && !feed.hasNextPage ? (
          <div className="flex flex-col gap-section">
            <EmptyState
              icon={<Rss />}
              title="Nothing from people you follow yet"
              action={
                <Button asChild variant="secondary" size="auto">
                  <Link href="/people">Find people</Link>
                </Button>
              }
            />
            <StartWith />
          </div>
        ) : (
          <>
            <ul aria-label="Activity">
              {items.map((it) => (
                <ActivityRow key={it.id} item={it} />
              ))}
            </ul>
            <LoadMore hasNextPage={feed.hasNextPage} isFetchingNextPage={feed.isFetchingNextPage} fetchNextPage={feed.fetchNextPage} label={items.length > 10 ? "You're all caught up" : undefined} />
          </>
        )}
      </div>
    </div>
  );
}
