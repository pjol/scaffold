"use client";

import { ArrowUpRight, X } from "lucide-react";
import type { ReactNode } from "react";
import { InfoTip } from "../components/InfoTip";
import { cn } from "../lib/cn";
import type { LinkComponent } from "../shell/nav";
import { ProviderIcon } from "./ProviderIcon";

/** A capability the person could connect (shared `ConnectionOffer`). */
export interface ConnectPromptOffer {
  capability: string;
  provider: string;
  label: string;
  connect_url: string;
}

export interface ConnectPromptProps {
  /** From `GET /v1/<feature>/sources` → `offers`. Nothing renders when empty. */
  offers: ConnectPromptOffer[] | undefined;
  /** App path to come back to after connecting (usually the current path); added as `return_to`. */
  returnTo?: string;
  /** @default "Connect your accounts" */
  title?: ReactNode;
  /** ⓘ next to the title. */
  info?: ReactNode;
  /** Router link (Next's `Link`). @default a plain anchor */
  linkAs?: LinkComponent;
  /** Shows a close button; the feature remembers the choice. */
  onDismiss?: () => void;
  className?: string;
}

/** Adds `return_to` to an offer's connect URL (app-relative paths only). */
export function withReturnTo(connectUrl: string, returnTo?: string): string {
  if (!returnTo || !returnTo.startsWith("/") || returnTo.startsWith("//")) return connectUrl;
  return `${connectUrl}${connectUrl.includes("?") ? "&" : "?"}return_to=${encodeURIComponent(returnTo)}`;
}

/**
 * A quiet offer to bring connected content into this view (empty states, the foot of a list):
 * one link per capability, never a nag. Renders nothing without offers.
 */
export function ConnectPrompt({ offers, returnTo, title = "Connect your accounts", info, linkAs, onDismiss, className }: ConnectPromptProps) {
  if (!offers || offers.length === 0) return null;
  const Anchor: LinkComponent = linkAs ?? (({ href, className: c, children, ...rest }) => <a href={href} className={c} {...rest}>{children}</a>);
  return (
    <section aria-label={typeof title === "string" ? title : "Connect your accounts"} className={cn("flex flex-col gap-2 rounded-lg border border-dashed border-border-strong p-3", className)}>
      <div className="flex min-h-8 items-center gap-2 px-1">
        <h3 className="flex min-w-0 flex-1 items-center gap-2 type-label font-semibold text-fg-muted">
          <span className="truncate">{title}</span>
          {info ? <InfoTip content={info} /> : null}
        </h3>
        {onDismiss ? (
          <button
            type="button"
            onClick={onDismiss}
            aria-label="Dismiss"
            className="relative inline-flex size-8 items-center justify-center rounded-sm text-fg-subtle hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring before:absolute before:-inset-1 before:content-['']"
          >
            <X className="size-4" aria-hidden />
          </button>
        ) : null}
      </div>
      <ul className="flex flex-col">
        {offers.map((o) => (
          <li key={o.capability}>
            <Anchor
              href={withReturnTo(o.connect_url, returnTo)}
              className="group flex min-h-tap items-center gap-3 rounded-md px-2 text-fg transition-colors duration-(--app-duration-fast) hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
            >
              <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-sunken">
                <ProviderIcon provider={o.provider} size={16} />
              </span>
              <span className="min-w-0 flex-1 truncate type-body font-medium">{o.label}</span>
              <span className="flex shrink-0 items-center gap-1 type-label font-semibold text-primary-strong">
                Connect
                <ArrowUpRight className="size-4 transition-transform duration-(--app-duration-fast) group-hover:translate-x-0.5 group-hover:-translate-y-0.5 motion-reduce:transition-none" aria-hidden />
              </span>
            </Anchor>
          </li>
        ))}
      </ul>
    </section>
  );
}
