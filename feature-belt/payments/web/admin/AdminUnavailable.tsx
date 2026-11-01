"use client";

import { isPaymentsUnconfigured, type ApiError } from "@scaffoldapp/shared";
import { ErrorState } from "@scaffoldapp/ui-web";
import { PaymentsUnavailable } from "../PaymentsUnavailable";

/** Error state for admin payment views: the unconfigured state, or what failed + Retry. */
export function AdminError({ error, title, onRetry, retrying }: { error: ApiError; title: string; onRetry: () => void; retrying: boolean }) {
  if (isPaymentsUnconfigured(error)) return <PaymentsUnavailable compact />;
  return <ErrorState compact title={title} info={error.message} onRetry={onRetry} retrying={retrying} />;
}
