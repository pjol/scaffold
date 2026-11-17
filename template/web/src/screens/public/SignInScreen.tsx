"use client";

import { useAuthSession } from "@scaffoldapp/shared";
import { Banner, Card, ErrorState, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect } from "react";
import { ClerkSignIn } from "@/components/auth/ClerkSignIn";
import { DevSignIn } from "@/components/auth/DevSignIn";
import { AUTH_MODE } from "@/lib/env";
import { safeRedirect } from "@/lib/redirect";

/** /sign-in (inside `AuthFrame`): Clerk's sign-in when a key is set, else the dev sign-in when dev auth is on, else "not configured". Signed-in visitors continue to `redirect_url`. */
export function SignInScreen() {
  const auth = useAuthSession();
  const router = useRouter();
  const params = useSearchParams();
  const target = safeRedirect(params.get("redirect_url"));
  const deleted = params.get("deleted") === "1";

  useEffect(() => {
    if (AUTH_MODE === "dev" && auth.isLoaded && auth.isSignedIn) router.replace(target);
  }, [auth.isLoaded, auth.isSignedIn, router, target]);

  return (
    <div className="flex w-full max-w-md flex-col gap-group">
      {deleted && <Banner tone="success">Your account was deleted.</Banner>}
      {AUTH_MODE === "none" ? (
        <ErrorState
          title="Sign-in isn't configured"
          info="Set NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY, or in development NEXT_PUBLIC_AUTH_DEV=true with a non-production NEXT_PUBLIC_APP_ENV."
        />
      ) : AUTH_MODE === "clerk" ? (
        <ClerkSignIn redirectTo={target} />
      ) : !auth.isLoaded ? (
        <SkeletonGroup label="Loading sign-in">
          <Card className="flex flex-col gap-subsection">
            <Skeleton className="mx-auto h-8 w-48" />
            <div className="flex flex-col gap-3">
              <Skeleton className="h-control" />
              <Skeleton className="h-control" />
            </div>
            <Skeleton className="h-56" />
          </Card>
        </SkeletonGroup>
      ) : (
        <DevSignIn onSignedIn={() => router.replace(target)} />
      )}
    </div>
  );
}
