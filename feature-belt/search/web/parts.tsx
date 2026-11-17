"use client";

import { parseSearchOperators, removeSearchOperator, searchHighlight, searchOperatorLabel, type SearchFilters } from "@scaffoldapp/shared";
import { X } from "lucide-react";

/** Text with the query's words marked (title or snippet of a result). */
export function Highlighted({ text, query, className }: { text: string; query: string; className?: string }) {
  const parts = searchHighlight(text, query);
  return (
    <span className={className}>
      {parts.map((p, i) =>
        p.match ? (
          <mark key={i} className="rounded-xs bg-primary-soft px-0.5 text-fg">
            {p.text}
          </mark>
        ) : (
          <span key={i}>{p.text}</span>
        ),
      )}
    </span>
  );
}

/** The query's operators as removable chips ("From Sam Lee ×", "Type page ×"). */
export function OperatorChips({
  query,
  filters,
  onChange,
  className,
}: {
  query: string;
  filters?: SearchFilters | undefined;
  onChange: (next: string) => void;
  className?: string;
}) {
  const { chips } = parseSearchOperators(query);
  if (chips.length === 0) return null;
  const names: Record<string, string[]> = {};
  for (const f of filters?.from ?? []) names[f.value] = f.names;
  return (
    <div role="group" aria-label="Filters in your search" className={className ?? "flex flex-wrap gap-2"}>
      {chips.map((c) => {
        const label = searchOperatorLabel(c, names);
        return (
          <span key={c.token} className="inline-flex h-8 items-center gap-1 rounded-pill border border-border-strong bg-surface pl-3 pr-1 type-label text-fg">
            <span className="max-w-56 truncate">{label}</span>
            <button
              type="button"
              aria-label={`Remove ${label}`}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => onChange(removeSearchOperator(query, c))}
              className="flex size-6 items-center justify-center rounded-pill text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring"
            >
              <X aria-hidden className="size-3.5" />
            </button>
          </span>
        );
      })}
    </div>
  );
}
