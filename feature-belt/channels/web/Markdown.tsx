"use client";

import { isChannelsEmojiOnly, parseChannelsMarkdown, type ChannelsBlock, type ChannelsInline, type ChannelsMention, type ChannelsUser } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import Link from "next/link";
import { Fragment, memo, useMemo, type ReactNode } from "react";

export interface MarkdownProps {
  body: string;
  /** The message's `mentions`: names for `<@id>` tokens. */
  mentions?: readonly ChannelsUser[];
  meId?: string | undefined;
  className?: string;
}

/** Same-origin links open in the app; the rest in a new tab. Only http(s) and mailto reach here. */
function MessageLink({ href, children }: { href: string; children: ReactNode }) {
  const internal = typeof window !== "undefined" && href.startsWith(window.location.origin + "/");
  const cls = "text-primary-strong underline underline-offset-2 break-words hover:no-underline focus-visible:outline-2 focus-visible:outline-ring";
  if (internal) {
    return (
      <Link href={href.slice(window.location.origin.length)} className={cls}>
        {children}
      </Link>
    );
  }
  return (
    <a href={href} target="_blank" rel="noopener noreferrer nofollow" className={cls}>
      {children}
    </a>
  );
}

function MentionChip({ mention, names, meId }: { mention: ChannelsMention; names: Map<string, string>; meId: string | undefined }) {
  const label =
    mention.kind === "user" ? `@${names.get(mention.id) ?? "someone"}` : mention.kind === "group" ? `@${mention.handle}` : `@${mention.kind}`;
  const me = (mention.kind === "user" && mention.id === meId) || mention.kind === "here" || mention.kind === "channel";
  return (
    <span className={cn("rounded-xs px-0.5 font-semibold", me ? "bg-warning-soft text-warning-strong" : "bg-primary-soft text-primary-strong")}>{label}</span>
  );
}

function Inline({ nodes, names, meId }: { nodes: ChannelsInline[]; names: Map<string, string>; meId: string | undefined }) {
  return (
    <>
      {nodes.map((n, i) => {
        switch (n.t) {
          case "text":
            return <Fragment key={i}>{n.text}</Fragment>;
          case "br":
            return <br key={i} />;
          case "bold":
            return (
              <strong key={i} className="font-semibold">
                <Inline nodes={n.children} names={names} meId={meId} />
              </strong>
            );
          case "italic":
            return (
              <em key={i}>
                <Inline nodes={n.children} names={names} meId={meId} />
              </em>
            );
          case "strike":
            return (
              <s key={i}>
                <Inline nodes={n.children} names={names} meId={meId} />
              </s>
            );
          case "code":
            return (
              <code key={i} className="rounded-xs border border-border bg-sunken px-1 font-mono text-danger-strong">
                {n.text}
              </code>
            );
          case "link":
            return (
              <MessageLink key={i} href={n.href}>
                <Inline nodes={n.children} names={names} meId={meId} />
              </MessageLink>
            );
          case "mention":
            return <MentionChip key={i} mention={n.mention} names={names} meId={meId} />;
          case "emoji":
            return (
              <span key={i} role="img" aria-label={n.name.replace(/_/g, " ")}>
                {n.text}
              </span>
            );
        }
      })}
    </>
  );
}

function Blocks({ blocks, names, meId }: { blocks: ChannelsBlock[]; names: Map<string, string>; meId: string | undefined }) {
  return (
    <>
      {blocks.map((b, i) => {
        switch (b.t) {
          case "p":
            return (
              <p key={i} className="break-words">
                <Inline nodes={b.children} names={names} meId={meId} />
              </p>
            );
          case "code":
            return (
              <pre key={i} className="overflow-x-auto rounded-md border border-border bg-sunken px-3 py-2 font-mono type-label text-fg">
                <code>{b.text}</code>
              </pre>
            );
          case "quote":
            return (
              <blockquote key={i} className="flex flex-col gap-1 border-l-4 border-border-strong pl-3 text-fg-muted">
                <Blocks blocks={b.children} names={names} meId={meId} />
              </blockquote>
            );
          case "list": {
            const Tag = b.ordered ? "ol" : "ul";
            return (
              <Tag key={i} start={b.ordered ? b.start : undefined} className={cn("flex flex-col gap-0.5 pl-6", b.ordered ? "list-decimal" : "list-disc")}>
                {b.items.map((item, j) => (
                  <li key={j} className="break-words">
                    <Inline nodes={item} names={names} meId={meId} />
                  </li>
                ))}
              </Tag>
            );
          }
        }
      })}
    </>
  );
}

/** A message body: the safe markdown subset rendered as React elements (never HTML). */
export const Markdown = memo(function Markdown({ body, mentions = [], meId, className }: MarkdownProps) {
  const blocks = useMemo(() => parseChannelsMarkdown(body), [body]);
  const names = useMemo(() => new Map(mentions.map((u) => [u.id, u.display_name])), [mentions]);
  const jumbo = useMemo(() => isChannelsEmojiOnly(body), [body]);
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5 text-fg", jumbo ? "type-headline" : "type-body", className)}>
      <Blocks blocks={blocks} names={names} meId={meId} />
    </div>
  );
});
