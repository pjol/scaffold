"use client";

import { cn } from "../lib/cn";
import { Tooltip } from "../components/Tooltip";
import { sourceAccessibleLabel, sourceProductName } from "./marks";
import { ProviderIcon } from "./ProviderIcon";

/** A row's source as features send it (shared `SourceRef`); app rows (`{ type: "app" }`, null) render nothing. */
export interface SourceBadgeSource {
  type?: string;
  provider?: string;
  product?: string | null;
  account?: string | null;
}

export interface SourceBadgeProps {
  /** The row's `source`. Nothing renders for the app's own content. */
  source: SourceBadgeSource | null | undefined;
  /** Name override ("Notion · Acme" rows may pass "Notion"). @default the product's name */
  label?: string;
  /** Show the account after the name ("Google Drive · sam@acme.com"). It is always in the accessible label and tooltip. */
  showAccount?: boolean;
  /** Icon only, for dense rows; the name shows in a tooltip on hover/focus. */
  iconOnly?: boolean;
  /** `sm` 24 px tall (rows, cards) · `md` 32 px (record headers). @default "sm" */
  size?: "sm" | "md";
  className?: string;
}

/** Marks connected content: provider icon + product name, compact. The app's own content carries no badge. */
export function SourceBadge({ source, label, showAccount, iconOnly, size = "sm", className }: SourceBadgeProps) {
  if (!source || source.type === "app" || !source.provider) return null;
  const provider = source.provider;
  const name = label || sourceProductName(provider, source.product);
  const full = sourceAccessibleLabel(provider, source.product, source.account, name);
  const iconSize = size === "sm" ? 14 : 16;
  if (iconOnly) {
    return (
      <Tooltip content={full.replace(/^From /, "")}>
        <span
          role="img"
          aria-label={full}
          tabIndex={0}
          className={cn(
            "inline-flex shrink-0 items-center justify-center rounded-sm text-fg-muted focus-visible:outline-2 focus-visible:outline-ring",
            size === "sm" ? "size-6" : "size-8",
            className,
          )}
        >
          <ProviderIcon provider={provider} product={source.product} size={iconSize} />
        </span>
      </Tooltip>
    );
  }
  return (
    <span
      title={full.replace(/^From /, "")}
      className={cn(
        "inline-flex min-w-0 max-w-full shrink items-center rounded-sm border border-border bg-surface font-medium leading-none text-fg-muted",
        size === "sm" ? "h-6 gap-1 px-1.5 text-caption" : "h-8 gap-1.5 px-2 text-label",
        className,
      )}
    >
      <ProviderIcon provider={provider} product={source.product} size={iconSize} className="text-fg" />
      <span className="sr-only">From </span>
      <span className="shrink-0 whitespace-nowrap">{name}</span>
      {source.account ? (
        showAccount ? (
          <span className="min-w-0 truncate">
            <span aria-hidden> · </span>
            <span className="sr-only">, </span>
            {source.account}
          </span>
        ) : (
          <span className="sr-only">, {source.account}</span>
        )
      ) : null}
    </span>
  );
}
