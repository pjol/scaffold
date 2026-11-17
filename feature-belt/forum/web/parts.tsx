"use client";

import {
  errorMessage,
  filePreviewParseMarkdown,
  filePreviewSafeHref,
  forumLabelText,
  forumMentionParts,
  useForumVote,
  useMe,
  type FilePreviewMdBlock,
  type FilePreviewMdInline,
  type ForumAuthor,
  type ForumContentStatus,
  type ForumVoteValue,
} from "@scaffoldapp/shared";
import { Badge, Banner, cn, IconButton, toast } from "@scaffoldapp/ui-web";
import { ArrowBigDown, ArrowBigUp, Lock, Pin } from "lucide-react";
import { useMemo } from "react";

function Plain({ text, mentions }: { text: string; mentions: readonly ForumAuthor[] }) {
  return (
    <>
      {forumMentionParts(text, mentions).map((p, i) =>
        p.mention ? (
          <span key={i} className="font-semibold text-primary-strong">
            {p.text}
          </span>
        ) : (
          <span key={i}>{p.text}</span>
        ),
      )}
    </>
  );
}

function Inline({ nodes, mentions }: { nodes: FilePreviewMdInline[]; mentions: readonly ForumAuthor[] }) {
  return (
    <>
      {nodes.map((n, i) => {
        switch (n.type) {
          case "text":
            return <Plain key={i} text={n.text} mentions={mentions} />;
          case "code":
            return (
              <code key={i} className="rounded-xs bg-sunken px-1 font-mono">
                {n.text}
              </code>
            );
          case "strong":
            return (
              <strong key={i} className="font-semibold">
                <Inline nodes={n.children} mentions={mentions} />
              </strong>
            );
          case "em":
            return (
              <em key={i}>
                <Inline nodes={n.children} mentions={mentions} />
              </em>
            );
          case "link": {
            const href = filePreviewSafeHref(n.href);
            return href ? (
              <a key={i} href={href} target="_blank" rel="noopener noreferrer nofollow ugc" className="text-primary-strong underline underline-offset-2">
                <Inline nodes={n.children} mentions={mentions} />
              </a>
            ) : (
              <Inline key={i} nodes={n.children} mentions={mentions} />
            );
          }
        }
      })}
    </>
  );
}

function Block({ b, mentions }: { b: FilePreviewMdBlock; mentions: readonly ForumAuthor[] }) {
  switch (b.type) {
    case "heading":
      return (
        <p className={cn("text-fg", b.level <= 2 ? "type-title-sm" : "font-semibold")}>
          <Inline nodes={b.children} mentions={mentions} />
        </p>
      );
    case "paragraph":
      return (
        <p className="whitespace-pre-wrap break-words">
          <Inline nodes={b.children} mentions={mentions} />
        </p>
      );
    case "list": {
      const items = b.items.map((it, i) => (
        <li key={i} className="break-words">
          <Inline nodes={it} mentions={mentions} />
        </li>
      ));
      return b.ordered ? (
        <ol start={b.start} className="flex list-decimal flex-col gap-1 pl-6">
          {items}
        </ol>
      ) : (
        <ul className="flex list-disc flex-col gap-1 pl-6">{items}</ul>
      );
    }
    case "quote":
      return (
        <blockquote className="rounded-md bg-sunken px-3 py-2 text-fg-muted">
          <Inline nodes={b.children} mentions={mentions} />
        </blockquote>
      );
    case "code":
      return (
        <pre className="overflow-x-auto rounded-md bg-sunken p-3 font-mono type-label text-fg">
          <code>{b.text}</code>
        </pre>
      );
    case "table":
      return (
        <div className="overflow-x-auto">
          <table className="type-label">
            <thead>
              <tr>
                {b.header.map((h, i) => (
                  <th key={i} className="border-b border-border px-2 py-1 text-left font-semibold">
                    <Inline nodes={h} mentions={mentions} />
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {b.rows.map((r, i) => (
                <tr key={i}>
                  {r.map((c, j) => (
                    <td key={j} className="border-b border-border px-2 py-1">
                      <Inline nodes={c} mentions={mentions} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case "rule":
      return <hr className="border-border" />;
  }
}

/** Markdown from a post or comment, drawn as components from a parsed tree (never HTML); "@Name" of mentioned people stands out. */
export function ForumMarkdown({ text, mentions = [], className }: { text: string; mentions?: readonly ForumAuthor[]; className?: string }) {
  const blocks = useMemo(() => filePreviewParseMarkdown(text, 400), [text]);
  if (blocks.length === 0) return null;
  return (
    <div className={cn("flex max-w-prose flex-col gap-3 type-body text-fg", className)}>
      {blocks.map((b, i) => (
        <Block key={i} b={b} mentions={mentions} />
      ))}
    </div>
  );
}

/** Up and down votes with the score between; tapping your vote again clears it. Your own items show the score only. */
export function ForumVoteButtons({
  kind,
  id,
  postId,
  authorId,
  score,
  vote,
  disabled: off,
  vertical,
}: {
  kind: "post" | "comment";
  id: string;
  postId: string;
  authorId: string | null | undefined;
  score: number;
  vote: ForumVoteValue;
  disabled?: boolean;
  vertical?: boolean;
}) {
  const v = useForumVote();
  const me = useMe();
  const disabled = off || (!!authorId && me.data?.id === authorId);
  const cast = (value: 1 | -1) =>
    v.mutate({ target_kind: kind, target_id: id, postId, value: vote === value ? 0 : value }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save your vote. Try again.")) });
  return (
    <div className={cn("flex shrink-0 items-center", vertical ? "flex-col gap-0.5" : "gap-1")}>
      <IconButton
        label={vote === 1 ? "Remove upvote" : "Upvote"}
        aria-pressed={vote === 1}
        icon={<ArrowBigUp className={vote === 1 ? "fill-current" : undefined} />}
        variant="ghost"
        size="sm"
        disabled={disabled}
        className={vote === 1 ? "text-primary-strong" : "text-fg-muted"}
        onClick={() => cast(1)}
      />
      <span className={cn("min-w-6 text-center type-label font-semibold tabular-nums", vote === 0 ? "text-fg" : vote === 1 ? "text-primary-strong" : "text-danger-strong")}>{score}</span>
      <IconButton
        label={vote === -1 ? "Remove downvote" : "Downvote"}
        aria-pressed={vote === -1}
        icon={<ArrowBigDown className={vote === -1 ? "fill-current" : undefined} />}
        variant="ghost"
        size="sm"
        disabled={disabled}
        className={vote === -1 ? "text-danger-strong" : "text-fg-muted"}
        onClick={() => cast(-1)}
      />
    </div>
  );
}

/** Pinned, locked, status and label badges. */
export function ForumBadges({ pinned, locked, status, labels }: { pinned?: boolean; locked?: boolean; status: ForumContentStatus; labels: string[] }) {
  if (!pinned && !locked && status === "visible" && labels.length === 0) return null;
  return (
    <span className="flex flex-wrap items-center gap-1">
      {pinned ? (
        <Badge tone="primary" size="sm">
          <Pin className="size-3" aria-hidden />
          Pinned
        </Badge>
      ) : null}
      {locked ? (
        <Badge tone="neutral" size="sm">
          <Lock className="size-3" aria-hidden />
          Locked
        </Badge>
      ) : null}
      {status === "held" ? (
        <Badge tone="warning" size="sm">
          In review
        </Badge>
      ) : null}
      {status === "removed" ? (
        <Badge tone="danger" size="sm">
          Removed
        </Badge>
      ) : null}
      {labels.map((l) => (
        <Badge key={l} tone="neutral" size="sm">
          {forumLabelText(l)}
        </Badge>
      ))}
    </span>
  );
}

/** Why a held or removed item isn't public, shown to its author and staff. */
export function ForumModerationBanner({ status, reasons, noun }: { status: ForumContentStatus; reasons: string[]; noun: "post" | "comment" }) {
  if (status !== "held" && status !== "removed") return null;
  const why = reasons.length > 0 ? ` ${reasons.join(". ")}.` : "";
  return status === "held" ? (
    <Banner tone="warning">{`Waiting for review: only you and moderators can see this ${noun}.${why}`}</Banner>
  ) : (
    <Banner tone="danger">{`Removed by moderators: only you and staff can see this ${noun}.${why}`}</Banner>
  );
}
