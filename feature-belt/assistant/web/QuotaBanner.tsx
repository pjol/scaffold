"use client";

import { assistantLimitReached, type AssistantUsage } from "@scaffoldapp/shared";
import { Banner } from "@scaffoldapp/ui-web";

const time = new Intl.DateTimeFormat("en-US", { hour: "numeric", minute: "2-digit" });

/** Shown when today's message or token limit is used up, with the reset time. */
export function QuotaBanner({ usage }: { usage: AssistantUsage | undefined }) {
  const reached = assistantLimitReached(usage);
  if (!usage || !reached) return null;
  const resets = time.format(new Date(usage.resets_at));
  return (
    <Banner tone="warning">
      {reached === "messages" ? `You've sent today's ${usage.message_limit} messages.` : "You've reached today's usage limit."} The assistant is back at {resets}.
    </Banner>
  );
}
