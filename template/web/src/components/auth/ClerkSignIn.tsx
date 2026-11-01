"use client";

import { ClerkFailed, ClerkLoaded, ClerkLoading, SignIn } from "@clerk/nextjs";
import { Card, ErrorState, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";

/** Clerk's `<SignIn>` (sign-in or sign-up in one flow), themed through ClerkProvider's appearance. */
export function ClerkSignIn({ redirectTo }: { redirectTo: string }) {
  return (
    <>
      <ClerkLoading>
        <SkeletonGroup label="Loading sign-in">
          <Card className="flex flex-col gap-4">
            <Skeleton className="h-7 w-2/3" />
            <Skeleton className="h-10" />
            <Skeleton className="h-10" />
          </Card>
        </SkeletonGroup>
      </ClerkLoading>
      <ClerkFailed>
        <ErrorState compact title="Couldn't load sign-in" info="The sign-in service didn't respond. Check your connection." onRetry={() => window.location.reload()} />
      </ClerkFailed>
      <ClerkLoaded>
        <SignIn routing="path" path="/sign-in" withSignUp fallbackRedirectUrl={redirectTo} signUpFallbackRedirectUrl={redirectTo} />
      </ClerkLoaded>
    </>
  );
}
