"use client";

import { errorMessage, hasErrorCode, useAgentsAccept, useAgentsReject, type AgentsSuggestion, type AgentsTargetChanged } from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, InfoTip, toast } from "@scaffoldapp/ui-web";
import { ExternalLink } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { formatDateTime, formatRelative } from "@/lib/format";
import { STATUS_LABEL, STATUS_TONE } from "./labels";
import { SuggestionDiff, SuggestionInput } from "./SuggestionDiff";

/** Who, where and when, for an InfoTip (no subtitles). */
export function suggestionDetails(s: AgentsSuggestion): string {
  const parts = [`${s.agent.client} suggested this ${formatRelative(s.created_at)} in ${s.feature_label}.`, `Action: ${s.tool_title} (${s.tool}).`];
  if (s.status === "pending") parts.push(`It expires ${formatDateTime(s.expires_at)}.`);
  return parts.join(" ");
}

/** The change itself: a diff when the tool described before and after, else the input. */
export function SuggestionChange({ s }: { s: AgentsSuggestion }) {
  if (s.before || s.after) return <SuggestionDiff before={s.before} after={s.after} />;
  return <SuggestionInput input={s.input} />;
}

/**
 * Accept / Reject for one waiting suggestion. A target that changed since the agent
 * looked shows the new version and "Accept anyway".
 */
export function SuggestionActions({ s, size = "sm" }: { s: AgentsSuggestion; size?: "sm" | "auto" }) {
  const accept = useAgentsAccept();
  const reject = useAgentsReject();
  const [changed, setChanged] = useState<AgentsTargetChanged | null>(null);
  const busy = accept.isPending || reject.isPending;
  if (s.status !== "pending") return null;
  const doAccept = (force: boolean) =>
    accept.mutate(
      { id: s.id, force },
      {
        onSuccess: (next) => {
          setChanged(null);
          if (next.status === "accepted") toast.success("Done");
          else toast.error(next.error?.message ?? "Couldn't apply this change.");
        },
        onError: (e) => {
          if (hasErrorCode(e, "target_changed")) setChanged((e.details as unknown as AgentsTargetChanged | undefined) ?? { summary: "", before: "", after: "" });
          else toast.error(errorMessage(e, "Couldn't accept. Try again."));
        },
      },
    );
  return (
    <div className="flex flex-col gap-3">
      {changed && (
        <div className="flex flex-col gap-3">
          <Banner tone="warning">This changed since {s.agent.client} suggested it. Here is the change against the current version.</Banner>
          <SuggestionDiff before={changed.before} after={changed.after} />
        </div>
      )}
      <div className="flex flex-wrap items-center justify-end gap-2">
        <Button
          variant="ghost"
          size={size}
          disabled={busy}
          loading={reject.isPending}
          onClick={() => reject.mutate(s.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't reject. Try again.")) })}
        >
          Reject
        </Button>
        <Button size={size} disabled={busy} loading={accept.isPending} onClick={() => doAccept(!!changed)}>
          {changed ? "Accept anyway" : "Accept"}
        </Button>
      </div>
    </div>
  );
}

/** A suggestion in the list: summary (ⓘ who/where/when), the change, the target link, actions. */
export function SuggestionCard({ s }: { s: AgentsSuggestion }) {
  return (
    <Card padding="compact" className="flex flex-col gap-3">
      <div className="flex items-start gap-3">
        <h3 className="flex min-w-0 flex-1 items-start gap-2 type-body font-semibold text-fg">
          <Link href={`/agents/suggestions/${s.id}`} className="min-w-0 break-words underline-offset-2 hover:underline">
            {s.summary}
          </Link>
          <InfoTip content={suggestionDetails(s)} />
        </h3>
        {s.status === "pending" ? (
          <span className="shrink-0 type-caption text-fg-muted">{formatRelative(s.created_at)}</span>
        ) : (
          <Badge tone={STATUS_TONE[s.status]}>{STATUS_LABEL[s.status]}</Badge>
        )}
      </div>
      <SuggestionChange s={s} />
      {s.error && s.status !== "pending" && <Banner tone={s.status === "failed" ? "danger" : "neutral"}>{s.error.message}</Banner>}
      <div className="flex flex-wrap items-center justify-between gap-2">
        {s.link ? (
          <Button asChild variant="ghost" size="sm" leftIcon={<ExternalLink />}>
            <Link href={s.link}>Open</Link>
          </Button>
        ) : (
          <span />
        )}
        <SuggestionActions s={s} />
      </div>
    </Card>
  );
}
