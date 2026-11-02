"use client";

import { Lock, Plus } from "lucide-react";
import type { ReactNode } from "react";
import { Skeleton } from "../components/Skeleton";
import { cn } from "../lib/cn";
import type { LinkComponent } from "../shell/nav";
import { sourceProductName, type SourceStatus } from "./marks";
import { ProviderIcon } from "./ProviderIcon";

/** One connected account in a feature's sidebar. */
export interface ConnectedSectionItem {
  /** The source id (connection id). */
  id: string;
  provider: string;
  product?: string | null;
  /** "Google Drive". @default the product's name */
  label?: string;
  /** "sam@acme.com", "acme-org", "Acme". */
  account?: string | null;
  status?: SourceStatus;
  /** Opens this source in the feature. Without it the row calls `onSelect`. */
  href?: string;
  /** Where "Reconnect" goes when status is needs_reauth. */
  fixUrl?: string | null;
}

export interface ConnectedSectionProps {
  items: ConnectedSectionItem[] | undefined;
  /** @default "Connected" */
  title?: string;
  /** The open source (`aria-current`). */
  activeId?: string | null;
  onSelect?: (id: string) => void;
  /** Router link (Next's `Link`). @default a plain anchor */
  linkAs?: LinkComponent;
  /** Where the header's + goes (Settings → Connections with the feature's capability). */
  addHref?: string;
  /** Replaces the + with your own header action. */
  action?: ReactNode;
  /** Rows to show while sources load. */
  loading?: boolean;
  className?: string;
}

/** Builds ConnectedSection items from a feature's sources (shared `FeatureSource`); app sources are skipped. */
export function connectedSectionItems(
  sources: Array<{ id: string; type: string; provider?: string; product?: string; account?: string; label: string; status?: SourceStatus; fix_url?: string }> | undefined,
  hrefFor?: (id: string) => string,
): ConnectedSectionItem[] {
  return (sources ?? [])
    .filter((s) => s.type === "connected" && s.provider)
    .map((s) => ({
      id: s.id,
      provider: s.provider!,
      ...(s.product ? { product: s.product } : {}),
      label: s.label,
      ...(s.account ? { account: s.account } : {}),
      ...(s.status ? { status: s.status } : {}),
      ...(s.fix_url ? { fixUrl: s.fix_url } : {}),
      ...(hrefFor ? { href: hrefFor(s.id) } : {}),
    }));
}

/**
 * The sidebar's "Connected" section: one row per connected account for this feature
 * ("Google Drive · sam@acme.com") with its status, and a Reconnect link when it needs one.
 * Renders nothing when there are no items and nothing loading.
 */
export function ConnectedSection({ items, title = "Connected", activeId, onSelect, linkAs, addHref, action, loading, className }: ConnectedSectionProps) {
  const list = items ?? [];
  if (!loading && list.length === 0) return null;
  const Anchor: LinkComponent = linkAs ?? (({ href, className: c, children, ...rest }) => <a href={href} className={c} {...rest}>{children}</a>);
  return (
    <section aria-label={title} className={cn("flex flex-col gap-0.5", className)}>
      <div className="flex min-h-8 items-center gap-1 px-1">
        <h3 className="min-w-0 flex-1 truncate px-1 type-caption font-semibold text-fg-muted">{title}</h3>
        {action ??
          (addHref ? (
            <Anchor
              href={addHref}
              aria-label="Connect an account"
              className="relative inline-flex size-6 items-center justify-center rounded-sm text-fg-subtle hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring before:absolute before:-inset-2 before:content-['']"
            >
              <Plus className="size-4" aria-hidden />
            </Anchor>
          ) : null)}
      </div>
      {loading && list.length === 0 ? (
        <div className="flex flex-col gap-1 px-2" aria-busy>
          <Skeleton className="h-6" />
          <Skeleton className="h-6 w-3/4" />
        </div>
      ) : (
        <ul className="flex flex-col">
          {list.map((it) => {
            const name = it.label || sourceProductName(it.provider, it.product);
            const active = it.id === activeId;
            const blocked = it.status === "blocked";
            const rowClass = cn(
              "flex min-w-0 flex-1 items-center gap-2 rounded-sm px-2 py-1 text-left focus-visible:outline-2 focus-visible:outline-ring",
              blocked ? "cursor-default text-fg-subtle" : "text-fg hover:bg-sunken",
              active && "bg-sunken",
            );
            const body = (
              <>
                <span aria-hidden className="flex size-5 shrink-0 items-center justify-center">
                  <ProviderIcon provider={it.provider} product={it.product} size={16} />
                </span>
                <span className="min-w-0 flex-1 truncate type-label">
                  {name}
                  {it.account && !name.endsWith(it.account) ? <span className="text-fg-muted"> · {it.account}</span> : null}
                </span>
                {blocked ? <Lock aria-label="Blocked by your workspace" className="size-4 shrink-0 text-fg-subtle" /> : null}
                {it.status === "needs_reauth" ? <span aria-hidden className="size-2 shrink-0 rounded-pill bg-warning" /> : null}
              </>
            );
            return (
              <li key={it.id} className="flex min-h-8 items-center gap-1">
                {blocked ? (
                  <span className={rowClass} title="Your workspace doesn't allow this connection">
                    {body}
                  </span>
                ) : it.href ? (
                  <Anchor href={it.href} className={rowClass} {...(active ? { "aria-current": "page" as const } : {})}>
                    {body}
                  </Anchor>
                ) : (
                  <button type="button" className={rowClass} aria-current={active ? "page" : undefined} onClick={() => onSelect?.(it.id)}>
                    {body}
                  </button>
                )}
                {it.status === "needs_reauth" && it.fixUrl ? (
                  <Anchor href={it.fixUrl} className="shrink-0 rounded-sm px-1.5 py-1 type-caption font-semibold text-warning-strong hover:bg-warning-soft focus-visible:outline-2 focus-visible:outline-ring">
                    Reconnect
                  </Anchor>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
