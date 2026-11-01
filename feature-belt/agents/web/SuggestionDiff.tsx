"use client";

import { agentsDiff, agentsDiffHunks, agentsInputRows, type AgentsDiffLine } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import { useMemo, useState } from "react";

const LINE_CLASS: Record<AgentsDiffLine["op"], string> = {
  same: "text-fg-muted",
  del: "bg-danger-soft text-fg",
  add: "bg-success-soft text-fg",
};

const MARK: Record<AgentsDiffLine["op"], string> = { same: " ", del: "−", add: "+" };
const SR: Record<AgentsDiffLine["op"], string> = { same: "", del: "Removed: ", add: "Added: " };

/**
 * Before → after as a readable diff: removed lines on a danger tint with "−", added on a
 * success tint with "+", the changed words in bold. Long unchanged runs fold away.
 */
export function SuggestionDiff({ before, after, className }: { before: string; after: string; className?: string }) {
  const [expanded, setExpanded] = useState(false);
  const lines = useMemo(() => agentsDiff(before, after), [before, after]);
  const rows = expanded ? lines : agentsDiffHunks(lines);
  return (
    <div className={cn("overflow-hidden rounded-md border border-border", className)} role="group" aria-label="Changes">
      <ol className="flex flex-col py-1">
        {rows.map((row, i) =>
          "hidden" in row ? (
            <li key={`h${i}`}>
              <button
                type="button"
                onClick={() => setExpanded(true)}
                className="w-full px-3 py-1 text-left type-caption text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
              >
                {row.hidden} unchanged {row.hidden === 1 ? "line" : "lines"}
              </button>
            </li>
          ) : (
            <li key={i} className={cn("flex gap-2 px-3 py-0.5 type-label", LINE_CLASS[row.op])}>
              <span aria-hidden className={cn("w-3 shrink-0 select-none", row.op === "del" ? "text-danger-strong" : row.op === "add" ? "text-success-strong" : "")}>
                {MARK[row.op]}
              </span>
              <span className="min-w-0 flex-1 whitespace-pre-wrap break-words">
                <span className="sr-only">{SR[row.op]}</span>
                {row.parts.map((p, j) => (
                  <span key={j} className={p.op === "same" ? "" : cn("font-semibold underline decoration-2 underline-offset-2", p.op === "del" ? "decoration-danger-strong line-through" : "decoration-success-strong")}>
                    {p.text || (row.parts.length === 1 ? " " : "")}
                  </span>
                ))}
              </span>
            </li>
          ),
        )}
      </ol>
    </div>
  );
}

/** A suggestion without before/after: what the agent would send, as labelled rows. */
export function SuggestionInput({ input }: { input: Record<string, unknown> }) {
  const rows = agentsInputRows(input);
  if (rows.length === 0) return null;
  return (
    <dl className="grid grid-cols-1 gap-x-4 gap-y-2 rounded-md border border-border p-4 sm:grid-cols-[minmax(8rem,auto)_1fr]">
      {rows.map((r) => (
        <div key={r.key} className="contents">
          <dt className="type-label text-fg-muted first-letter:uppercase">{r.key}</dt>
          <dd className="min-w-0 whitespace-pre-wrap break-words type-body text-fg">{r.value}</dd>
        </div>
      ))}
    </dl>
  );
}
