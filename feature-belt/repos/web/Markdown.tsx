"use client";

import { reposInlineText, reposParseMarkdown, reposResolvePath, type ReposMdBlock, type ReposMdInline } from "@scaffoldapp/shared";
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
}

function slug(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s-]/gu, "")
    .trim()
    .replace(/\s+/g, "-");
}

/** A rendered README: GitHub-flavoured Markdown, raw HTML shown as text. */
export function RepoMarkdown({ source, repoId, gitRef, dir, className }: RepoMarkdownProps) {
  const blocks = useMemo(() => reposParseMarkdown(source), [source]);
  const ctx = { repoId, gitRef, dir };
  return (
    <div className={cn("flex min-w-0 max-w-prose flex-col gap-4 type-body text-fg [overflow-wrap:anywhere]", className)}>
      {blocks.map((b, i) => (
        <Block key={i} block={b} ctx={ctx} />
      ))}
    </div>
  );
}

type Ctx = { repoId: string; gitRef?: string | undefined; dir: string };

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
      return <p className="whitespace-pre-wrap">{b.text}</p>;
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
            return <Fragment key={i}>{n.text}</Fragment>;
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
            // Images aren't loaded (an outside image would tell its host who reads the README); link to them instead.
            const l = repoHref(ctx, n.src);
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
