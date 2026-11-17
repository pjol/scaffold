"use client";

import { errorMessage, useLeaderboardProfile, useSetLeaderboardVisibility, type LeaderboardVisibility } from "@scaffoldapp/shared";
import { Card, ErrorState, ListGroup, ListRow, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { Check } from "lucide-react";
import { PageHeader, Section } from "@/components/shell/PageHeader";

/** Settings → Leaderboard: appear under your alias, your name and photo, or not at all. */
export function LeaderboardSettingsScreen() {
  const profile = useLeaderboardProfile();
  const set = useSetLeaderboardVisibility();
  const choose = (v: LeaderboardVisibility) =>
    set.mutate(v, { onSuccess: () => toast.success(v === "hidden" ? "You're off the leaderboards" : "Saved"), onError: (e) => toast.error(errorMessage(e, "Couldn't save. Try again.")) });

  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Leaderboard" />
      <Section title="How you appear" info="Changes show on the boards within a minute.">
        {profile.isPending ? (
          <SkeletonGroup label="Loading" className="flex flex-col gap-2">
            <Skeleton className="h-12" />
            <Skeleton className="h-12" />
            <Skeleton className="h-12" />
          </SkeletonGroup>
        ) : profile.isError ? (
          <Card>
            <ErrorState compact title="Couldn't load your leaderboard settings" info={profile.error.message} onRetry={() => void profile.refetch()} />
          </Card>
        ) : (
          <ListGroup>
            {(
              [
                ["alias", `As ${profile.data.alias}`],
                ["name", profile.data.display_name ? `As ${profile.data.display_name}, with your photo` : "With your name and photo"],
                ["hidden", "Hide me from leaderboards"],
              ] as [LeaderboardVisibility, string][]
            ).map(([v, label]) => (
              <ListRow
                key={v}
                title={label}
                interactive
                chevron={false}
                current={profile.data.visibility === v}
                aria-pressed={profile.data.visibility === v}
                trailing={profile.data.visibility === v ? <Check className="size-5 text-primary-strong" aria-hidden /> : undefined}
                onClick={() => profile.data.visibility !== v && choose(v)}
              />
            ))}
          </ListGroup>
        )}
      </Section>
    </div>
  );
}
