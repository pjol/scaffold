"use client";

import { reposInlineText, reposIsConnectedId, reposParseMarkdown, reposResolvePath, useReposBlob, type ReposMdBlock, type ReposMdInline, type ReposMention } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import Link from "next/link";
import { Fragment, useMemo, type ReactNode } from "react";
import { CodeView } from "./code";

export interface RepoMarkdownProps {
  source: string;
  /** For relative links: the repository, ref and the README's folder. */
  repoId: string;
  gitRef?: string | undefined;
  dir: string;
  className?: string;
  /** Names for <@id> mention tokens (comments, issues). */
  mentions?: ReposMention[] | undefined;
}

function slug(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s-]/gu, "")
    .trim()
    .replace(/\s+/g, "-");
}

/** A rendered README: GitHub-flavoured Markdown, raw HTML shown as text. */
export function RepoMarkdown({ source, repoId, gitRef, dir, className, mentions }: RepoMarkdownProps) {
  const blocks = useMemo(() => reposParseMarkdown(source), [source]);
  const ctx: Ctx = { repoId, gitRef, dir, mentions: new Map((mentions ?? []).map((m) => [m.id, m.display_name])), refs: !reposIsConnectedId(repoId) };
  return (
    <div className={cn("flex min-w-0 max-w-prose flex-col gap-4 type-body text-fg [overflow-wrap:anywhere]", className)}>
      {blocks.map((b, i) => (
        <Block key={i} block={b} ctx={ctx} />
      ))}
    </div>
  );
}

type Ctx = { repoId: string; gitRef?: string | undefined; dir: string; mentions?: Map<string, string>; refs?: boolean };

const RICH_RE = /<@([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})>|(^|[^\w&/#])#(\d{1,9})\b/g;

/** Text with <@id> mentions as @names and #12 linked to the issue or pull request. */
function RichText({ text, ctx }: { text: string; ctx: Ctx }): ReactNode {
  if (!text.includes("<@") && !(ctx.refs && text.includes("#"))) return text;
  const out: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(RICH_RE)) {
    const at = m.index ?? 0;
    if (m[1]) {
      out.push(text.slice(last, at));
      const name = ctx.mentions?.get(m[1]);
      out.push(
        <span key={at} className="rounded-xs bg-primary-soft px-0.5 font-semibold text-primary-strong">
          @{name ?? "someone"}
        </span>,
      );
      last = at + m[0].length;
    } else if (m[3] && ctx.refs) {
      const lead = m[2] ?? "";
      out.push(text.slice(last, at) + lead);
      out.push(
        <Link key={at} href={`/repos/${ctx.repoId}/issues/${m[3]}`} className={linkClass}>
          #{m[3]}
        </Link>,
      );
      last = at + m[0].length;
    }
  }
  out.push(text.slice(last));
  return <>{out}</>;
}

/** An image stored in the repository, read from git (no outside fetch). */
function RepoImage({ ctx, path, alt }: { ctx: Ctx; path: string; alt: string }) {
  const blob = useReposBlob(ctx.repoId, { ref: ctx.gitRef, path });
  const href = `/repos/${ctx.repoId}/blob?${new URLSearchParams({ path, ...(ctx.gitRef ? { ref: ctx.gitRef } : {}) }).toString()}`;
  if (blob.isPending) return <span className="inline-block h-6 w-24 animate-pulse rounded-xs bg-sunken motion-reduce:animate-none" aria-label={alt || "Loading image"} />;
  const b = blob.data;
  if (!b || !b.image || b.too_large || b.encoding !== "base64") {
    return (
      <Link href={href} className={linkClass}>
        {alt || "Image"}
      </Link>
    );
  }
  const ext = (path.split(".").pop() ?? "png").toLowerCase();
  const mime = ext === "jpg" || ext === "jpeg" ? "image/jpeg" : ext === "gif" ? "image/gif" : ext === "webp" ? "image/webp" : ext === "ico" ? "image/x-icon" : ext === "bmp" ? "image/bmp" : "image/png";
  // eslint-disable-next-line @next/next/no-img-element -- a data URL from the repository, not an optimisable remote image
  return <img src={`data:${mime};base64,${b.content}`} alt={alt} className="inline max-h-[32rem] max-w-full rounded-xs align-middle" loading="lazy" />;
}

function repoHref(ctx: Ctx, href: string): { internal: boolean; href: string } {
  if (/^(https?:|mailto:)/i.test(href)) return { internal: false, href };
  if (href.startsWith("#")) return { internal: true, href };
  const path = reposResolvePath(ctx.dir, href);
  const q = new URLSearchParams({ path, ...(ctx.gitRef ? { ref: ctx.gitRef } : {}) });
  return { internal: true, href: `/repos/${ctx.repoId}/blob?${q.toString()}` };
}

function Block({ block: b, ctx }: { block: ReposMdBlock; ctx: Ctx }): ReactNode {
  switch (b.t) {
    case "heading": {
      const text = reposInlineText(b.c);
      const cls = b.depth === 1 ? "type-headline" : b.depth === 2 ? "type-title border-b border-border pb-2" : b.depth === 3 ? "type-title-sm" : "type-body font-semibold";
      const H = (`h${Math.min(b.depth + 1, 6)}` as "h2");
      return (
        <H id={slug(text)} className={cn("scroll-mt-24 text-fg first:mt-0", b.depth <= 2 ? "mt-4" : "mt-2", cls)}>
          <Inline nodes={b.c} ctx={ctx} />
        </H>
      );
    }
    case "paragraph":
      return (
        <p>
          <Inline nodes={b.c} ctx={ctx} />
        </p>
      );
    case "text":
      return (
        <p className="whitespace-pre-wrap">
          <RichText text={b.text} ctx={ctx} />
        </p>
      );
    case "code":
      return <CodeView code={b.text} language={b.lang || "plaintext"} numbers={false} className="rounded-md border border-border bg-sunken py-2" />;
    case "hr":
      return <hr className="border-border" />;
    case "quote":
      return (
        <blockquote className="flex flex-col gap-3 rounded-md bg-sunken px-4 py-3 text-fg-muted">
          {b.c.map((c, i) => (
            <Block key={i} block={c} ctx={ctx} />
          ))}
        </blockquote>
      );
    case "list": {
      const L = b.ordered ? "ol" : "ul";
      return (
        <L start={b.ordered ? b.start : undefined} className={cn("flex flex-col gap-1 pl-6", b.ordered ? "list-decimal" : "list-disc")}>
          {b.items.map((it, i) => (
            <li key={i} className={cn(it.task && "list-none -ml-6 flex gap-2")}>
              {it.task ? <input type="checkbox" checked={it.checked} readOnly disabled aria-label={it.checked ? "Done" : "Not done"} className="mt-1.5 accent-primary" /> : null}
              <div className="flex min-w-0 flex-col gap-2">
                {it.c.map((c, j) =>
                  c.t === "paragraph" ? (
                    <span key={j}>
                      <Inline nodes={c.c} ctx={ctx} />
                    </span>
                  ) : (
                    <Block key={j} block={c} ctx={ctx} />
                  ),
                )}
              </div>
            </li>
          ))}
        </L>
      );
    }
    case "table":
      return (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full border-collapse type-label">
            <thead className="bg-sunken">
              <tr>
                {b.header.map((h, i) => (
                  <th key={i} className="border-b border-border px-3 py-2 text-left font-semibold" style={{ textAlign: b.align[i] ?? "left" }}>
                    <Inline nodes={h} ctx={ctx} />
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {b.rows.map((row, r) => (
                <tr key={r} className="border-b border-border last:border-0">
                  {row.map((cell, c) => (
                    <td key={c} className="px-3 py-2 font-normal" style={{ textAlign: b.align[c] ?? "left" }}>
                      <Inline nodes={cell} ctx={ctx} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
  }
}

const linkClass = "text-primary-strong underline underline-offset-2 hover:no-underline focus-visible:outline-2 focus-visible:outline-ring";

function Inline({ nodes, ctx }: { nodes: ReposMdInline[]; ctx: Ctx }): ReactNode {
  return (
    <>
      {nodes.map((n, i) => {
        switch (n.t) {
          case "text":
            return (
              <Fragment key={i}>
                <RichText text={n.text} ctx={ctx} />
              </Fragment>
            );
          case "br":
            return <br key={i} />;
          case "strong":
            return (
              <strong key={i} className="font-semibold">
                <Inline nodes={n.c} ctx={ctx} />
              </strong>
            );
          case "em":
            return (
              <em key={i}>
                <Inline nodes={n.c} ctx={ctx} />
              </em>
            );
          case "del":
            return (
              <del key={i}>
                <Inline nodes={n.c} ctx={ctx} />
              </del>
            );
          case "code":
            return (
              <code key={i} className="rounded-xs bg-sunken px-1 py-0.5 font-mono text-label font-normal">
                {n.text}
              </code>
            );
          case "link": {
            const l = repoHref(ctx, n.href);
            return l.internal ? (
              <Link key={i} href={l.href} className={linkClass}>
                <Inline nodes={n.c} ctx={ctx} />
              </Link>
            ) : (
              <a key={i} href={l.href} target="_blank" rel="noopener noreferrer nofollow" className={linkClass}>
                <Inline nodes={n.c} ctx={ctx} />
              </a>
            );
          }
          case "image": {
            // Images in the repository come from git; outside images stay links (loading one would tell its host who reads the page).
            const l = repoHref(ctx, n.src);
            if (l.internal && !n.src.startsWith("#")) {
              return <RepoImage key={i} ctx={ctx} path={reposResolvePath(ctx.dir, n.src)} alt={n.alt} />;
            }
            return l.internal ? (
              <Link key={i} href={l.href} className={linkClass}>
                {n.alt || "Image"}
              </Link>
            ) : (
              <a key={i} href={l.href} target="_blank" rel="noopener noreferrer nofollow" className={linkClass}>
                {n.alt || "Image"}
              </a>
            );
          }
        }
      })}
    </>
  );
}
