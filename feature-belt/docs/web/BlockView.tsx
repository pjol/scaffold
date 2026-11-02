"use client";

import { docsListNumbers, docsTitle, orderBlocks, useDocsFileUrl, type DocsBlock, type DocsRefs, type DocsSpan } from "@scaffoldapp/shared";
import { Skeleton, cn } from "@scaffoldapp/ui-web";
import { Check, ChevronRight, Download, FileText, Globe, Lock } from "lucide-react";
import Link from "next/link";
import { useMemo, useState, type ReactNode } from "react";
import { ConnectedPageBlock } from "./connected/ConnectedPageBlock";
import { mentionLabel } from "./editor/dom";
import { formatBytes } from "./format";

/** Rich text as React (read-only). */
export function Spans({ spans, refs, pageHref }: { spans: readonly DocsSpan[]; refs: DocsRefs; pageHref?: ((id: string) => string | null) | undefined }) {
  return (
    <>
      {spans.map((s, i) => {
        if (s.mention) {
          const label = mentionLabel(s, refs);
          if (s.mention.type === "page") {
            const href = pageHref ? pageHref(s.mention.id) : `/docs/${s.mention.id}`;
            const noAccess = refs.pages[s.mention.id]?.title === null;
            return href && !noAccess ? (
              <Link key={i} href={href} className="underline decoration-border-strong underline-offset-2 hover:bg-sunken">
                {label}
              </Link>
            ) : (
              <span key={i} className="text-fg-subtle">
                {label}
              </span>
            );
          }
          return (
            <span key={i} className={s.mention.type === "user" ? "rounded-xs bg-primary-soft px-1 text-primary-strong" : "rounded-xs bg-sunken px-1 text-fg-muted"}>
              {label}
            </span>
          );
        }
        let node: ReactNode = s.text;
        if (s.code) node = <code className="rounded-xs bg-sunken px-1 font-mono">{node}</code>;
        const cls = cn(s.bold && "font-semibold", s.italic && "italic", s.underline && "underline underline-offset-2", s.strike && "line-through");
        if (s.link)
          return (
            <a key={i} href={s.link} target="_blank" rel="noopener noreferrer nofollow" className={cn("text-primary-strong underline underline-offset-2", cls)}>
              {node}
            </a>
          );
        return cls ? (
          <span key={i} className={cls}>
            {node}
          </span>
        ) : (
          <span key={i}>{node}</span>
        );
      })}
    </>
  );
}

interface ViewCtx {
  refs: DocsRefs;
  token: string | null;
  children: Map<string, DocsBlock[]>;
  pageHref: (id: string) => string | null;
  renderDatabase?: ((id: string) => ReactNode) | undefined;
  orgId: string | null;
}

function ImageView({ b, ctx }: { b: DocsBlock; ctx: ViewCtx }) {
  const file = useDocsFileUrl(b.props.url ? null : b.props.file_id, ctx.token);
  const src = b.props.url || file.data?.url;
  return (
    <figure className="flex flex-col gap-1" style={{ width: `${b.props.width ?? 100}%` }}>
      {src ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={src} referrerPolicy="no-referrer" alt={(b.props.caption ?? []).map((s) => s.text).join("") || b.props.name || "Image"} loading="lazy" className="w-full rounded-md border border-border" />
      ) : file.isError ? (
        <div className="flex h-24 items-center justify-center rounded-md border border-dashed border-border type-label text-fg-muted">This image isn't available.</div>
      ) : (
        <Skeleton className="h-40 w-full rounded-md" />
      )}
      {b.props.caption?.length ? (
        <figcaption className="type-label text-fg-muted">
          <Spans spans={b.props.caption} refs={ctx.refs} pageHref={ctx.pageHref} />
        </figcaption>
      ) : null}
    </figure>
  );
}

function FileView({ b, ctx }: { b: DocsBlock; ctx: ViewCtx }) {
  const [want, setWant] = useState(false);
  const file = useDocsFileUrl(want && !b.props.url ? b.props.file_id : null, ctx.token);
  const link = b.props.url || file.data?.url;
  if (want && link) {
    window.open(link, "_blank", "noopener,noreferrer");
    setWant(false);
  }
  return (
    <button type="button" onClick={() => setWant(true)} className="flex min-h-12 w-full items-center gap-3 rounded-md border border-border bg-surface px-3 py-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
      <FileText aria-hidden className="size-5 shrink-0 text-fg-muted" />
      <span className="min-w-0 flex-1 truncate type-body text-fg">{b.props.name ?? "File"}</span>
      <span className="shrink-0 type-caption text-fg-muted">{formatBytes(b.props.size ?? 0)}</span>
      <Download aria-hidden className="size-4 shrink-0 text-fg-muted" />
    </button>
  );
}

function Toggle({ b, ctx, depth }: { b: DocsBlock; ctx: ViewCtx; depth: number }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex flex-col">
      <div className="flex items-start">
        <button type="button" aria-expanded={open} aria-label={open ? "Collapse" : "Expand"} onClick={() => setOpen((o) => !o)} className="flex h-8 w-6 shrink-0 items-center justify-center rounded-sm text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
          <ChevronRight aria-hidden className={cn("size-4 transition-transform motion-reduce:transition-none", open && "rotate-90")} />
        </button>
        <p className="min-w-0 flex-1 py-1 type-body text-fg">
          <Spans spans={b.props.text ?? []} refs={ctx.refs} pageHref={ctx.pageHref} />
        </p>
      </div>
      {open ? (
        <div className="pl-6">
          <List parent={b.id} ctx={ctx} depth={depth + 1} />
        </div>
      ) : null}
    </div>
  );
}

function BlockItem({ b, ctx, depth, number }: { b: DocsBlock; ctx: ViewCtx; depth: number; number: number | undefined }) {
  const text = <Spans spans={b.props.text ?? []} refs={ctx.refs} pageHref={ctx.pageHref} />;
  const kids = b.type !== "toggle" && (ctx.children.get(b.id)?.length ?? 0) > 0 ? (
    <div className="pl-6">
      <List parent={b.id} ctx={ctx} depth={depth + 1} />
    </div>
  ) : null;
  let body: ReactNode;
  switch (b.type) {
    case "heading_1":
      body = <h2 className="pt-6 pb-1 type-headline text-fg">{text}</h2>;
      break;
    case "heading_2":
      body = <h3 className="pt-5 pb-1 type-title text-fg">{text}</h3>;
      break;
    case "heading_3":
      body = <h4 className="pt-3 type-title-sm text-fg">{text}</h4>;
      break;
    case "bulleted_list":
      body = (
        <div className="flex items-start">
          <span aria-hidden className="flex h-8 w-6 shrink-0 items-center justify-center">
            <span className="size-1.5 rounded-pill bg-fg" />
          </span>
          <p className="min-w-0 flex-1 py-1 type-body text-fg">{text}</p>
        </div>
      );
      break;
    case "numbered_list":
      body = (
        <div className="flex items-start">
          <span aria-hidden className="flex h-8 min-w-6 shrink-0 items-center justify-end pr-1.5 type-body tabular-nums">{number ?? 1}.</span>
          <p className="min-w-0 flex-1 py-1 type-body text-fg">{text}</p>
        </div>
      );
      break;
    case "to_do":
      body = (
        <div className="flex items-start">
          <span className="flex h-8 w-6 shrink-0 items-center justify-center">
            <span role="img" aria-label={b.props.checked ? "Done" : "Not done"} className={cn("flex size-4 items-center justify-center rounded-xs border [&_svg]:size-3", b.props.checked ? "border-primary bg-primary text-primary-fg" : "border-border-strong bg-surface")}>
              {b.props.checked ? <Check aria-hidden strokeWidth={3} /> : null}
            </span>
          </span>
          <p className={cn("min-w-0 flex-1 py-1 type-body", b.props.checked ? "text-fg-muted line-through" : "text-fg")}>{text}</p>
        </div>
      );
      break;
    case "toggle":
      return <Toggle b={b} ctx={ctx} depth={depth} />;
    case "quote":
      body = <blockquote className="border-l-2 border-border-strong py-1 pl-4 type-body-lg text-fg">{text}</blockquote>;
      break;
    case "callout":
      body = (
        <div className="flex items-start gap-2 rounded-md bg-sunken px-3 py-2">
          <span aria-hidden className="flex size-8 shrink-0 items-center justify-center type-body-lg">
            {b.props.icon ?? "💡"}
          </span>
          <p className="min-w-0 flex-1 py-1 type-body text-fg">{text}</p>
        </div>
      );
      break;
    case "code":
      body = (
        <pre className="overflow-x-auto rounded-md bg-sunken px-3 py-3 font-mono type-label text-fg">
          <code>{b.props.code}</code>
        </pre>
      );
      break;
    case "divider":
      body = <hr className="my-3 border-border" />;
      break;
    case "image":
      body = <ImageView b={b} ctx={ctx} />;
      break;
    case "file":
      body = <FileView b={b} ctx={ctx} />;
      break;
    case "bookmark":
      body = (
        <a href={b.props.url} target="_blank" rel="noopener noreferrer nofollow" className="flex min-h-12 items-center gap-3 rounded-md border border-border bg-surface px-3 py-2 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
          <Globe aria-hidden className="size-5 shrink-0 text-fg-muted" />
          <span className="min-w-0 flex-1 truncate type-body text-fg">{b.props.title || b.props.url}</span>
        </a>
      );
      break;
    case "page": {
      const ref = ctx.refs.pages[b.props.page_id ?? ""];
      const href = b.props.page_id ? ctx.pageHref(b.props.page_id) : null;
      body =
        ref?.title && href ? (
          <Link href={href} className="flex min-h-control items-center gap-2 rounded-sm px-1 type-body text-fg underline decoration-border-strong underline-offset-4 hover:bg-sunken">
            <span aria-hidden>{ref.icon ?? <FileText className="size-4 text-fg-muted" />}</span>
            {docsTitle(ref.title)}
          </Link>
        ) : (
          <span className="flex min-h-control items-center gap-2 px-1 type-body text-fg-subtle">
            <Lock aria-hidden className="size-4" />
            No access
          </span>
        );
      break;
    }
    case "table":
      body = (
        <div className="overflow-x-auto">
          <table className="w-full border-collapse type-label">
            <tbody>
              {(b.props.rows ?? []).map((row, r) => (
                <tr key={r}>
                  {row.map((cell, c) => {
                    const head = (b.props.header_row && r === 0) || (b.props.header_col && c === 0);
                    const Cell = head ? "th" : "td";
                    return (
                      <Cell key={c} className={cn("border border-border px-2 py-1 text-left align-top", head && "bg-sunken font-semibold")}>
                        <Spans spans={cell} refs={ctx.refs} pageHref={ctx.pageHref} />
                      </Cell>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
      break;
    case "database":
      body = b.props.database_id && ctx.renderDatabase ? ctx.renderDatabase(b.props.database_id) : null;
      break;
    case "connected_page":
      // Published pages show only the link: each signed-in viewer opens it with their own connection.
      body = ctx.token ? (
        <a href={b.props.url} target="_blank" rel="noopener noreferrer nofollow" className="flex min-h-12 items-center gap-3 rounded-md border border-border bg-surface px-3 py-2 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
          <Globe aria-hidden className="size-5 shrink-0 text-fg-muted" />
          <span className="min-w-0 flex-1 truncate type-body text-fg">{b.props.title || "Untitled"}</span>
        </a>
      ) : (
        <ConnectedPageBlock block={b} orgId={ctx.orgId} />
      );
      break;
    default:
      body = <p className="py-1 type-body text-fg">{text}</p>;
  }
  return (
    <div className={cn(b.type === "image" && "py-2", b.type === "database" && "py-3")}>
      {body}
      {kids}
    </div>
  );
}

function List({ parent, ctx, depth }: { parent: string; ctx: ViewCtx; depth: number }) {
  const kids = ctx.children.get(parent) ?? [];
  const numbers = docsListNumbers(kids);
  if (depth > 32) return null;
  return (
    <>
      {kids.map((b) => (
        <BlockItem key={b.id} b={b} ctx={ctx} depth={depth} number={numbers.get(b.id)} />
      ))}
    </>
  );
}

/** Read-only page content: published pages and saved versions. */
export function BlockView({
  blocks,
  refs,
  token = null,
  pageHref,
  renderDatabase,
  orgId = null,
}: {
  blocks: readonly DocsBlock[];
  refs: DocsRefs;
  token?: string | null;
  pageHref?: (id: string) => string | null;
  renderDatabase?: (id: string) => ReactNode;
  /** The workspace connected-page blocks open in (its connection policy). */
  orgId?: string | null;
}) {
  const children = useMemo(() => orderBlocks(new Map(blocks.map((b) => [b.id, b]))).children, [blocks]);
  const ctx: ViewCtx = { refs, token, children, pageHref: pageHref ?? ((id) => `/docs/${id}`), renderDatabase, orgId };
  if (blocks.length === 0) return <p className="type-body text-fg-subtle">This page is empty.</p>;
  return (
    <div className="flex flex-col">
      <List parent="" ctx={ctx} depth={0} />
    </div>
  );
}
