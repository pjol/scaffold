"use client";

import { EmptyState } from "@scaffoldapp/ui-web";
import { CircleCheck, CircleSlash } from "lucide-react";
import { useSearchParams } from "next/navigation";

/**
 * `/payments/return?status=success|cancel|portal` (public): where hosted checkout sends people
 * who started it in the mobile app's in-app browser. That browser has no web session,
 * so this page reads nothing; the app shows the real status once the browser closes.
 * Clerk projects list it in proxy.ts PUBLIC_PATHS (see FEATURE.md → Stitching).
 */
export function HostedReturnScreen() {
  const status = useSearchParams().get("status");
  const canceled = status === "cancel";
  const title = canceled ? "Checkout canceled" : status === "portal" ? "Done" : "Checkout finished";
  return (
    <div className="mx-auto flex w-full max-w-form flex-1 flex-col justify-center px-gutter py-section">
      <EmptyState
        icon={canceled ? <CircleSlash /> : <CircleCheck />}
        tone={canceled ? "neutral" : "primary"}
        title={title}
        info={status === "portal" ? "Close this page to go back to the app." : "Close this page to go back to the app. The app shows whether the payment went through."}
      />
    </div>
  );
}
