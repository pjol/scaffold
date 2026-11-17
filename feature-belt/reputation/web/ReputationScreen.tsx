"use client";

import { useMyReputation } from "@scaffoldapp/shared";
import { ErrorState, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { Screen } from "@/components/shell/PageHeader";
import { ReputationDetail } from "./ReputationDetail";

/** /reputation: the caller's score explained, with requirements, stats and the 90-day curve. */
export function ReputationScreen() {
  const rep = useMyReputation();
  return (
    <div className="mx-auto w-full max-w-content px-gutter py-section">
      <Screen title="Reputation" info="A 0 to 100 score from your last 90 days: risk-adjusted returns, drawdowns, steady weeks, community standing and time active. Every number it uses is below.">
        {rep.isPending ? (
          <SkeletonGroup label="Loading your reputation" className="flex flex-col gap-section">
            <div className="flex items-center gap-4">
              <Skeleton shape="circle" className="size-20" />
              <Skeleton shape="text" className="h-6 w-40" />
            </div>
            <Skeleton className="h-72" />
            <Skeleton className="h-48" />
          </SkeletonGroup>
        ) : rep.isError ? (
          <ErrorState title="Couldn't load your reputation" info={rep.error.message} onRetry={() => void rep.refetch()} retrying={rep.isFetching} />
        ) : (
          <ReputationDetail rep={rep.data} />
        )}
      </Screen>
    </div>
  );
}
