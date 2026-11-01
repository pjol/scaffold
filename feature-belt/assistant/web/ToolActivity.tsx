"use client";

import { assistantToolLabel, type AssistantActivity } from "@scaffoldapp/shared";
import { Spinner, cn } from "@scaffoldapp/ui-web";
import { CircleAlert, CircleCheck } from "lucide-react";

/** Tool calls behind a reply, one compact line each: status, what ran, its one-line result. */
export function ToolActivity({ activity }: { activity: AssistantActivity[] }) {
  if (activity.length === 0) return null;
  return (
    <ul aria-label="Tools used" className="flex flex-col gap-1">
      {activity.map((a) => (
        <li key={a.call_id} className="flex min-h-6 items-center gap-2 type-caption text-fg-muted">
          <span className="flex size-4 shrink-0 items-center justify-center">
            {a.status === "running" ? (
              <Spinner size={14} label={`Running ${assistantToolLabel(a.name)}`} />
            ) : a.status === "ok" ? (
              <CircleCheck className="size-4 text-success-strong" aria-label="Done" />
            ) : (
              <CircleAlert className="size-4 text-danger-strong" aria-label="Failed" />
            )}
          </span>
          <span className="font-semibold text-fg">{assistantToolLabel(a.name)}</span>
          {a.summary ? <span className={cn("min-w-0 truncate", a.status === "error" && "text-danger-strong")}>{a.summary}</span> : null}
          {a.status !== "running" && a.duration_ms > 0 ? <span className="ml-auto shrink-0 tabular-nums text-fg-subtle">{formatDuration(a.duration_ms)}</span> : null}
        </li>
      ))}
    </ul>
  );
}

function formatDuration(ms: number): string {
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`;
}
