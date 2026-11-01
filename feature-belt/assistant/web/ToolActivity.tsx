"use client";

import {
  assistantSuggestionHref,
  assistantToolInputText,
  assistantToolLabel,
  errorMessage,
  useDecideAssistantToolCall,
  type AssistantActivity,
} from "@scaffoldapp/shared";
import { Badge, Button, Spinner, cn, toast } from "@scaffoldapp/ui-web";
import { Ban, CircleAlert, CircleCheck, ClipboardCheck, ShieldQuestion } from "lucide-react";
import Link from "next/link";

/**
 * Tool calls behind a reply, one compact line each: status, the connected service it came
 * from (if any), what ran, its one-line result. A change in a connected service that waits
 * for the user shows its complete input with Approve and Decline.
 */
export function ToolActivity({ activity, conversationId, messageId }: { activity: AssistantActivity[]; conversationId: string; messageId: string }) {
  if (activity.length === 0) return null;
  return (
    <ul aria-label="Tools used" className="flex flex-col gap-1">
      {activity.map((a) =>
        a.status === "awaiting_approval" ? (
          <ApprovalCard key={a.call_id} activity={a} conversationId={conversationId} messageId={messageId} />
        ) : (
          <li key={a.call_id} className="flex min-h-6 items-center gap-2 type-caption text-fg-muted">
            <span className="flex size-4 shrink-0 items-center justify-center">
              <StatusIcon activity={a} />
            </span>
            {a.source ? (
              <Badge size="sm" tone="neutral">
                {a.source}
              </Badge>
            ) : null}
            <span className="shrink-0 font-semibold text-fg">{assistantToolLabel(a.name)}</span>
            {a.status === "suggested" && a.suggestion_id ? (
              <Link href={assistantSuggestionHref(a.suggestion_id)} className="min-w-0 truncate font-semibold text-info-strong underline-offset-2 hover:underline">
                Suggested — waiting for your review
              </Link>
            ) : a.summary ? (
              <span className={cn("min-w-0 truncate", a.status === "error" && "text-danger-strong")}>{a.summary}</span>
            ) : null}
            {a.status !== "running" && a.duration_ms > 0 ? <span className="ml-auto shrink-0 tabular-nums text-fg-subtle">{formatDuration(a.duration_ms)}</span> : null}
          </li>
        ),
      )}
    </ul>
  );
}

function StatusIcon({ activity: a }: { activity: AssistantActivity }) {
  switch (a.status) {
    case "running":
      return <Spinner size={14} label={`Running ${assistantToolLabel(a.name)}`} />;
    case "ok":
      return <CircleCheck className="size-4 text-success-strong" aria-label="Done" />;
    case "denied":
      return <Ban className="size-4 text-fg-subtle" aria-label="Not run" />;
    case "suggested":
      return <ClipboardCheck className="size-4 text-info-strong" aria-label="Suggested" />;
    default:
      return <CircleAlert className="size-4 text-danger-strong" aria-label="Failed" />;
  }
}

function ApprovalCard({ activity: a, conversationId, messageId }: { activity: AssistantActivity; conversationId: string; messageId: string }) {
  const decide = useDecideAssistantToolCall();
  const label = assistantToolLabel(a.name);
  const send = (approved: boolean) =>
    decide.mutate(
      { conversationId, messageId, callId: a.call_id, approved },
      { onError: (e) => toast.error(errorMessage(e, "Couldn't send your answer. Try again.")) },
    );
  return (
    <li className="my-1 flex flex-col gap-3 rounded-lg border border-border-strong bg-surface p-4" aria-live="polite">
      <p className="flex items-start gap-2 type-label text-fg">
        <ShieldQuestion className="mt-0.5 size-4 shrink-0 text-warning-strong" aria-hidden />
        <span>
          Allow <span className="font-semibold">{label}</span> in <span className="font-semibold">{a.source ?? "the connected service"}</span>? This changes something there.
        </span>
      </p>
      <pre className="max-h-48 overflow-auto rounded-md bg-sunken px-3 py-2 font-mono type-caption whitespace-pre-wrap text-fg [overflow-wrap:anywhere]">
        {assistantToolInputText(a.input)}
      </pre>
      <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <Button size="sm" variant="secondary" disabled={decide.isPending} onClick={() => send(false)}>
          Decline
        </Button>
        <Button size="sm" loading={decide.isPending && decide.variables?.approved === true} disabled={decide.isPending} onClick={() => send(true)}>
          Approve
        </Button>
      </div>
    </li>
  );
}

function formatDuration(ms: number): string {
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`;
}
