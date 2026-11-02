"use client";

import { AlertCircle, Lock } from "lucide-react";
import { Chip } from "../components/Chip";
import { cn } from "../lib/cn";
import { sourcePlatformName, sourceProductName, type SourceStatus } from "./marks";
import { ProviderIcon } from "./ProviderIcon";

/** One chip. `id` is "all", "app", a connection id, or `provider:<id>` when grouped by platform. */
export interface SourceFilterOption {
  id: string;
  label: string;
  /** Connected sources show the platform's icon. */
  provider?: string;
  product?: string | null;
  count?: number;
  status?: SourceStatus;
}

export interface SourceFilterProps {
  options: SourceFilterOption[];
  value: string;
  onChange: (id: string) => void;
  /** Accessible name of the group. @default "Source" */
  label?: string;
  /** @default "sm" */
  size?: "sm" | "md";
  className?: string;
}

/**
 * Source chips: All · <App> · each connected source (or platform). One row that scrolls sideways
 * on narrow screens. Hidden when there is nothing to choose between (no connected source).
 */
export function SourceFilter({ options, value, onChange, label = "Source", size = "sm", className }: SourceFilterProps) {
  if (!options.some((o) => o.provider)) return null;
  return (
    <div role="group" aria-label={label} className={cn("-mx-1 flex min-w-0 gap-2 overflow-x-auto px-1 py-0.5 [scrollbar-width:none]", className)}>
      {options.map((o) => {
        const selected = o.id === value;
        const statusText = o.status === "needs_reauth" ? "needs reconnecting" : o.status === "blocked" ? "blocked by your workspace" : undefined;
        return (
          <Chip
            key={o.id}
            size={size}
            selected={selected}
            aria-pressed={selected}
            {...(statusText ? { title: `${o.label} (${statusText})` } : {})}
            onClick={() => onChange(o.id)}
            icon={o.provider ? <ProviderIcon provider={o.provider} product={o.product} size={16} /> : undefined}
            {...(o.count !== undefined ? { count: o.count } : {})}
            className="max-w-64"
          >
            <span className="truncate">{o.label}</span>
            {statusText ? <span className="sr-only">, {statusText}</span> : null}
            {o.status === "needs_reauth" ? <AlertCircle aria-hidden className="text-warning-strong" /> : null}
            {o.status === "blocked" ? <Lock aria-hidden className="text-fg-subtle" /> : null}
          </Chip>
        );
      })}
    </div>
  );
}

/** A feature source as `GET /v1/<feature>/sources` returns it (shared `FeatureSource`). */
export interface SourceFilterSource {
  id: string;
  type: string;
  provider?: string;
  product?: string;
  account?: string;
  label: string;
  status?: SourceStatus;
}

export interface SourceFilterOptionsInput {
  /** The app's own content, e.g. the app name or "Forum". */
  appLabel: string;
  /** The feature's sources (the app's own entry is optional; it is always added). */
  sources: SourceFilterSource[] | undefined;
  /** Per-chip counts by option id. */
  counts?: Record<string, number>;
  /** `connection` (default): one chip per connected account · `provider`: one per platform (`provider:<id>`). */
  groupBy?: "connection" | "provider";
  /** @default "All" */
  allLabel?: string;
}

/** Builds SourceFilter options: All · <App> · connected sources, labelled "Google Drive · sam@acme.com" (or by platform). */
export function sourceFilterOptions({ appLabel, sources, counts, groupBy = "connection", allLabel = "All" }: SourceFilterOptionsInput): SourceFilterOption[] {
  const withCount = (o: SourceFilterOption): SourceFilterOption => (counts && counts[o.id] !== undefined ? { ...o, count: counts[o.id] } : o);
  const out: SourceFilterOption[] = [withCount({ id: "all", label: allLabel }), withCount({ id: "app", label: appLabel })];
  const connected = (sources ?? []).filter((s) => s.type === "connected" && s.provider);
  if (groupBy === "provider") {
    const seen = new Set<string>();
    for (const s of connected) {
      if (seen.has(s.provider!)) continue;
      seen.add(s.provider!);
      out.push(withCount({ id: `provider:${s.provider}`, label: sourcePlatformName(s.provider!), provider: s.provider! }));
    }
    return out;
  }
  const names = connected.map((s) => s.label || sourceProductName(s.provider!, s.product));
  for (const [i, s] of connected.entries()) {
    const name = names[i]!;
    // Name the account only when two sources would otherwise read the same.
    const ambiguous = names.filter((n) => n === name).length > 1;
    out.push(
      withCount({
        id: s.id,
        label: ambiguous && s.account ? `${name} · ${s.account}` : name,
        provider: s.provider!,
        ...(s.product ? { product: s.product } : {}),
        ...(s.status ? { status: s.status } : {}),
      }),
    );
  }
  return out;
}
