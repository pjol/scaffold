"use client";

import { docsFolds, docsListNumbers, docsTitle, orderBlocks, type DocsBlock, type DocsRefs, type DocsSpan } from "@scaffoldapp/shared";
import { Button, cn } from "@scaffoldapp/ui-web";
import { ArrowUpRight, Check, ChevronRight, FileText, Globe, Lock, RefreshCw } from "lucide-react";
import Link from "next/link";
import { useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { ConnectedPageBlock } from "./connected/ConnectedPageBlock";
import { colorClass } from "./editor/colors";
import { columnWidths } from "./editor/columns";
import { mentionLabel } from "./editor/dom";
import { renderTeX } from "./editor/katex";
import { AudioBody, BookmarkBody, CodeView, EmbedBody, EquationBody, FileBody, ImageBody, jumpToBlock, MediaFrame, PdfBody, TocBody, VideoBody } from "./editor/render";

function InlineEquation({ tex }: { tex: string }) {
  const ref = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    if (ref.current) renderTeX(ref.current, tex, false);
  }, [tex]);
  return (
    <span ref={ref} role="img" aria-label={`Equation: ${tex}`} className="mx-px">
      {tex}
    </span>
  );
}

/** Rich text as React (read-only). */
export function Spans({ spans, refs, pageHref }: { spans: readonly DocsSpan[]; refs: DocsRefs; pageHref?: ((id: string) => string | null) | undefined }) {
  return (
    <>
      {spans.map((s, i) => {
        if (s.equation) return <InlineEquation key={i} tex={s.text} />;
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
        const cls = cn(s.bold && "font-semibold", s.italic && "italic", s.underline && "underline underline-offset-2", s.strike && "line-through", colorClass(s.color, true));
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
  blocks: readonly DocsBlock[];
  refs: DocsRefs;
  token: string | null;
  children: Map<string, DocsBlock[]>;
  pageHref: (id: string) => string | null;
  renderDatabase?: ((id: string) => ReactNode) | undefined;
  orgId: string | null;
}

const HEADING_CLASS: Record<string, string> = { heading_1: "type-headline", heading_2: "type-title", heading_3: "type-title-sm", heading_4: "type-body-lg font-semibold" };

function Toggle({ b, ctx, depth }: { b: DocsBlock; ctx: ViewCtx; depth: number }) {
  const [open, setOpen] = useState(false);
  const heading = HEADING_CLASS[b.type];
  return (
    <div className={cn("flex flex-col", heading && "pt-3", colorClass(b.props.color))}>
      <div className="flex items-start">
        <button type="button" aria-expanded={open} aria-label={open ? "Collapse" : "Expand"} onClick={() => setOpen((o) => !o)} className={cn("flex w-6 shrink-0 items-center justify-center rounded-sm text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring", heading ? "h-10" : "h-8")}>
          <ChevronRight aria-hidden className={cn("size-4 transition-transform motion-reduce:transition-none", open && "rotate-90")} />
        </button>
        <p className={cn("min-w-0 flex-1 py-1", heading ?? "type-body")}>
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
  if (docsFolds(b.type, b.props)) return <Toggle b={b} ctx={ctx} depth={depth} />;
  const ownKids = b.type === "column_list" || b.type === "synced_block";
  const kids = !ownKids && (ctx.children.get(b.id)?.length ?? 0) > 0 ? (
    <div className="pl-6">
      <List parent={b.id} ctx={ctx} depth={depth + 1} />
    </div>
  ) : null;
  let body: ReactNode;
  switch (b.type) {
    case "heading_1":
      body = <h2 className="pt-6 pb-1 type-headline">{text}</h2>;
      break;
    case "heading_2":
      body = <h3 className="pt-5 pb-1 type-title">{text}</h3>;
      break;
    case "heading_3":
      body = <h4 className="pt-3 type-title-sm">{text}</h4>;
      break;
    case "heading_4":
      body = <h5 className="pt-2 type-body-lg font-semibold">{text}</h5>;
      break;
    case "bulleted_list":
      body = (
        <div className="flex items-start">
          <span aria-hidden className="flex h-8 w-6 shrink-0 items-center justify-center">
            <span className="size-1.5 rounded-pill bg-current" />
          </span>
          <p className="min-w-0 flex-1 py-1 type-body">{text}</p>
        </div>
      );
      break;
    case "numbered_list":
      body = (
        <div className="flex items-start">
          <span aria-hidden className="flex h-8 min-w-6 shrink-0 items-center justify-end pr-1.5 type-body tabular-nums">{number ?? 1}.</span>
          <p className="min-w-0 flex-1 py-1 type-body">{text}</p>
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
    case "quote":
      body = <blockquote className="border-l-2 border-current py-1 pl-4 type-body-lg">{text}</blockquote>;
      break;
    case "callout":
      body = (
        <div className={cn("flex items-start gap-2 rounded-md px-3 py-2", b.props.color ? colorClass(b.props.color) : "bg-sunken")}>
          <span aria-hidden className="flex size-8 shrink-0 items-center justify-center type-body-lg">
            {b.props.icon ?? "💡"}
          </span>
          <p className="min-w-0 flex-1 py-1 type-body">{text}</p>
        </div>
      );
      break;
    case "code":
      body = (
        <figure className="flex flex-col gap-1">
          <CodeView block={b} />
          <Caption b={b} ctx={ctx} />
        </figure>
      );
      break;
    case "equation":
      body = <EquationBody expression={b.props.expression ?? ""} />;
      break;
    case "divider":
      body = <hr className="my-3 border-current opacity-30" />;
      break;
    case "image":
    case "video":
      body = (
        <figure className="flex flex-col gap-1">
          <MediaFrame width={b.props.width} align={b.props.align}>
            {b.type === "image" ? <ImageBody block={b} token={ctx.token} /> : <VideoBody block={b} token={ctx.token} />}
          </MediaFrame>
          <Caption b={b} ctx={ctx} />
        </figure>
      );
      break;
    case "audio":
    case "pdf":
    case "file":
      body = (
        <figure className="flex flex-col gap-1">
          {b.type === "audio" ? <AudioBody block={b} token={ctx.token} /> : b.type === "pdf" ? <PdfBody block={b} token={ctx.token} /> : <FileBody block={b} token={ctx.token} />}
          <Caption b={b} ctx={ctx} />
        </figure>
      );
      break;
    case "bookmark":
      body = (
        <figure className="flex flex-col gap-1">
          <BookmarkBody block={b} token={ctx.token} />
          <Caption b={b} ctx={ctx} />
        </figure>
      );
      break;
    case "embed":
      body = (
        <figure className="flex flex-col gap-1">
          <MediaFrame width={b.props.width} align={b.props.align}>
            <EmbedBody block={b} />
          </MediaFrame>
          <Caption b={b} ctx={ctx} />
        </figure>
      );
      break;
    case "table_of_contents":
      body = <TocBody blocks={ctx.blocks} onJump={jumpToBlock} />;
      break;
    case "breadcrumb":
      body = null;
      break;
    case "button": {
      const link = (b.props.actions ?? []).find((a) => a.type === "link");
      body = (
        <div className="py-1">
          {link && link.type === "link" ? (
            <Button asChild size="sm" variant="secondary" rightIcon={<ArrowUpRight />}>
              <a href={link.url} target="_blank" rel="noopener noreferrer nofollow">
                {b.props.label || "Button"}
              </a>
            </Button>
          ) : (
            <Button size="sm" variant="secondary" disabled>
              {b.props.label || "Button"}
            </Button>
          )}
        </div>
      );
      break;
    }
    case "column_list": {
      const cols = ctx.children.get(b.id) ?? [];
      const widths = columnWidths(cols);
      body = (
        <div className="flex flex-col gap-2 sm:flex-row sm:gap-4">
          {cols.map((col, i) => (
            <div key={col.id} className="min-w-0 flex-1" style={{ flexGrow: widths[i]! * 100, flexBasis: 0 }}>
              <List parent={col.id} ctx={ctx} depth={depth + 2} />
            </div>
          ))}
        </div>
      );
      break;
    }
    case "synced_block":
      body = b.props.source_id ? (
        <p className="flex items-center gap-2 rounded-md border border-dashed border-border px-3 py-2 type-label text-fg-muted">
          <RefreshCw aria-hidden className="size-4" />
          Synced content from another page isn&apos;t shown here.
        </p>
      ) : (
        <List parent={b.id} ctx={ctx} depth={depth + 1} />
      );
      break;
    case "link_to_page":
    case "page": {
      const ref = ctx.refs.pages[b.props.page_id ?? ""];
      const href = b.props.page_id ? ctx.pageHref(b.props.page_id) : null;
      body =
        ref?.title && href ? (
          <Link href={href} data-page-id={b.props.page_id} className="flex min-h-control items-center gap-2 rounded-sm px-1 type-body text-fg underline decoration-border-strong underline-offset-4 hover:bg-sunken">
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
      body = <p className="py-1 type-body">{text}</p>;
  }
  const color = b.type !== "callout" ? b.props.color : undefined;
  return (
    <div data-block-id={b.id} className={cn("scroll-mt-24", (b.type === "image" || b.type === "video" || b.type === "embed") && "py-2", b.type === "database" && "py-3", color && colorClass(color), color?.endsWith("_background") && "px-1")}>
      {body}
      {kids}
    </div>
  );
}

function Caption({ b, ctx }: { b: DocsBlock; ctx: ViewCtx }) {
  if (!b.props.caption?.length) return null;
  return (
    <figcaption className={cn("type-label text-fg-muted", (b.type === "image" || b.type === "video") && (b.props.align === "left" ? "" : b.props.align === "right" ? "text-right" : "text-center"))}>
      <Spans spans={b.props.caption} refs={ctx.refs} pageHref={ctx.pageHref} />
    </figcaption>
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
  const ctx: ViewCtx = { blocks, refs, token, children, pageHref: pageHref ?? ((id) => `/docs/${id}`), renderDatabase, orgId };
  if (blocks.length === 0) return <p className="type-body text-fg-subtle">This page is empty.</p>;
  return (
    <div className="flex flex-col">
      <List parent="" ctx={ctx} depth={0} />
    </div>
  );
}
