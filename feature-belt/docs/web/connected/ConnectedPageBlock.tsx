"use client";

import { connectionFixPath, docsConnectedHref, docsPlatformName, docsTitle, useDocsResolveConnected, type DocsBlock } from "@scaffoldapp/shared";
import { ProviderIcon, Skeleton, SourceBadge, withReturnTo } from "@scaffoldapp/ui-web";
import { ExternalLink, Lock } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { BlockView } from "../BlockView";

function Frame({ block, children, action, badge }: { block: DocsBlock; children: ReactNode; action?: ReactNode; badge?: ReactNode }) {
  const platform = docsPlatformName(block.props.provider);
  return (
    <div className="flex flex-col gap-2 rounded-md border border-border bg-surface p-3">
      <div className="flex min-w-0 items-center gap-2">
        {badge ?? (
          <span aria-hidden className="flex size-6 shrink-0 items-center justify-center text-fg-muted">
            <ProviderIcon provider={block.props.provider ?? ""} product={block.props.provider === "google" ? "docs" : null} size={16} />
          </span>
        )}
        <span className="min-w-0 flex-1 truncate type-label font-semibold text-fg" title={`${block.props.title || "Untitled"} · ${platform}`}>
          {block.props.title || "Untitled"}
        </span>
        {action}
      </div>
      {children}
    </div>
  );
}

/**
 * A connected-page block: a Notion page or Google Doc shown live to each viewer through
 * their own connection. Viewers without one are offered to connect; viewers whose account
 * can't open it can ask for access on the platform.
 */
export function ConnectedPageBlock({ block, orgId }: { block: DocsBlock; orgId: string | null }) {
  const pathname = usePathname() ?? "/docs";
  const res = useDocsResolveConnected(block.props, orgId);
  const platform = docsPlatformName(block.props.provider);
  const openOnPlatform = block.props.url ? (
    <a
      href={block.props.url}
      target="_blank"
      rel="noopener noreferrer nofollow"
      className="flex shrink-0 items-center gap-1 rounded-sm px-1.5 py-1 type-caption text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-tap"
    >
      <ExternalLink aria-hidden className="size-3.5" />
      <span className="hidden sm:inline">Open in {platform}</span>
      <span className="sr-only sm:hidden">Open in {platform}</span>
    </a>
  ) : null;
  if (res.isPending) {
    return (
      <Frame block={block}>
        <Skeleton className="h-4 w-3/4" />
        <Skeleton className="h-4 w-1/2" />
      </Frame>
    );
  }
  if (res.isError) {
    return (
      <Frame block={block} action={openOnPlatform}>
        <p className="type-label text-fg-muted">Couldn't load it from {platform}.</p>
      </Frame>
    );
  }
  const r = res.data;
  const quiet = "type-label font-semibold text-primary-strong underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-ring";
  switch (r.state) {
    case "ok": {
      const item = r.item!;
      return (
        <Frame
          block={block}
          badge={<SourceBadge source={item.source} iconOnly className="shrink-0" />}
          action={
            <Link href={item.link} className="shrink-0 rounded-sm px-1.5 py-1 type-caption font-semibold text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-tap">
              Open
            </Link>
          }
        >
          {r.blocks.length ? (
            <div className="max-h-80 overflow-hidden border-l-2 border-border pl-3">
              <BlockView blocks={r.blocks} refs={r.refs ?? { users: {}, pages: {} }} orgId={orgId} pageHref={(id) => docsConnectedHref(item.connection_id, id)} />
            </div>
          ) : (
            <p className="type-label text-fg-subtle">{docsTitle(item.title)} is empty.</p>
          )}
          {r.more ? (
            <Link href={item.link} className={quiet}>
              Continue reading
            </Link>
          ) : null}
        </Frame>
      );
    }
    case "connect":
      return (
        <Frame block={block} action={openOnPlatform}>
          <p className="type-label text-fg-muted">
            Connect {platform} to view this page here.{" "}
            <Link href={withReturnTo(r.offer?.connect_url ?? connectionFixPath(r.offer?.capability), pathname)} className={quiet}>
              Connect {platform}
            </Link>
          </p>
        </Frame>
      );
    case "reconnect":
      return (
        <Frame block={block} action={openOnPlatform}>
          <p className="type-label text-fg-muted">
            Your {platform} connection needs to be renewed.{" "}
            <Link href={withReturnTo(r.fix_url ?? connectionFixPath(), pathname)} className={quiet}>
              Reconnect
            </Link>
          </p>
        </Frame>
      );
    case "blocked":
      return (
        <Frame block={block}>
          <p className="flex items-center gap-1.5 type-label text-fg-muted">
            <Lock aria-hidden className="size-4" />
            Your workspace doesn't allow {platform} here.
          </p>
        </Frame>
      );
    default:
      return (
        <Frame block={block}>
          <p className="type-label text-fg-muted">
            Your {platform} account can't open this page.{" "}
            {block.props.url ? (
              <a href={block.props.url} target="_blank" rel="noopener noreferrer nofollow" className={quiet}>
                Ask for access
              </a>
            ) : null}
          </p>
        </Frame>
      );
  }
}
