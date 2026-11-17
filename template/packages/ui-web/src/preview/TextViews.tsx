"use client";

import {
  filePreviewCsvDelimiter,
  filePreviewParseCsv,
  filePreviewParseMarkdown,
  type FilePreviewMdBlock,
  type FilePreviewMdInline,
} from "@scaffoldapp/shared/preview";
import type { FilePreviewToken, FilePreviewTokenRole } from "@scaffoldapp/shared/preview/highlight";
import { Fragment, memo, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { EmptyState } from "../components/EmptyState";
import { cn } from "../lib/cn";

/** Token colours: roles map onto semantic colour tokens, so a restyle recolours code too. */
const TOKEN_CLASS: Record<FilePreviewTokenRole, string> = {
  keyword: "text-primary-strong",
  string: "text-success-strong",
  number: "text-warning-strong",
  comment: "text-fg-muted italic",
  function: "text-info-strong",
  type: "text-accent-strong",
  attr: "text-warning-strong",
  meta: "text-danger-strong",
  deletion: "text-danger-strong",
  addition: "text-success-strong",
  plain: "",
};

const MAX_LINES = 5000;

function plain(text: string): FilePreviewToken[][] {
  return text
    .replace(/\r\n?/g, "\n")
    .replace(/\n$/, "")
    .split("\n")
    .map((l) => (l ? [{ text: l, role: "plain" as const }] : []));
}

/** Lines of tokens: plain at once, highlighted when the highlighter (loaded on first use) is ready. */
function useHighlighted(text: string, language: string): FilePreviewToken[][] {
  const base = useMemo(() => plain(text), [text]);
  const [lines, setLines] = useState(base);
  useEffect(() => {
    setLines(base);
    if (language === "plaintext") return;
    let live = true;
    void import("@scaffoldapp/shared/preview/highlight").then(
      (m) => live && setLines(m.filePreviewHighlight(text, language)),
      () => undefined,
    );
    return () => {
      live = false;
    };
  }, [text, language, base]);
  return lines;
}

const TokenLine = memo(function TokenLine({ tokens }: { tokens: FilePreviewToken[] }) {
  if (tokens.length === 0) return <>{" "}</>;
  return (
    <>
      {tokens.map((t, i) => (
        <span key={i} className={TOKEN_CLASS[t.role] || undefined}>
          {t.text}
        </span>
      ))}
    </>
  );
});

/** Code or text with line numbers, highlighted for known languages; long files show their first 5,000 lines. */
export function CodeView({ text, language, className, wrap = false }: { text: string; language: string; className?: string; wrap?: boolean }) {
  const all = useHighlighted(text, language);
  const lines = all.length > MAX_LINES ? all.slice(0, MAX_LINES) : all;
  const gutter = String(lines.length).length;
  if (text.trim() === "") return <EmptyState compact title="This file is empty" />;
  return (
    <div className="flex flex-col gap-2">
      <div className={cn("overflow-auto rounded-lg border border-border bg-surface", className)}>
        <pre className={cn("py-3 font-mono type-label text-fg", !wrap && "min-w-max")}>
          {lines.map((l, i) => (
            <div key={i} className="flex">
              <span aria-hidden className="shrink-0 pr-4 pl-4 text-right text-fg-subtle tabular-nums select-none" style={{ width: `${gutter + 3}ch` }}>
                {i + 1}
              </span>
              <code className={cn("pr-4", wrap ? "break-all whitespace-pre-wrap" : "whitespace-pre")}>
                <TokenLine tokens={l} />
              </code>
            </div>
          ))}
        </pre>
      </div>
      {all.length > MAX_LINES ? (
        <p className="type-caption text-fg-muted">
          Showing {MAX_LINES.toLocaleString()} of {all.length.toLocaleString()} lines.
        </p>
      ) : null}
    </div>
  );
}

function Inline({ nodes }: { nodes: FilePreviewMdInline[] }): ReactNode {
  return nodes.map((n, i) => {
    switch (n.type) {
      case "text":
        return <Fragment key={i}>{n.text}</Fragment>;
      case "code":
        return (
          <code key={i} className="rounded-xs bg-sunken px-1 font-mono">
            {n.text}
          </code>
        );
      case "strong":
        return (
          <strong key={i} className="font-semibold">
            <Inline nodes={n.children} />
          </strong>
        );
      case "em":
        return (
          <em key={i}>
            <Inline nodes={n.children} />
          </em>
        );
      case "link":
        return (
          <a key={i} href={n.href} target="_blank" rel="noopener noreferrer nofollow" className="text-primary-strong underline underline-offset-2">
            <Inline nodes={n.children} />
          </a>
        );
    }
  });
}

const HEADING = ["type-headline", "type-title", "type-title-sm", "type-body-lg font-semibold", "type-body font-semibold", "type-body font-semibold"];

function MdBlock({ block }: { block: FilePreviewMdBlock }) {
  switch (block.type) {
    case "heading": {
      const Tag = `h${block.level}` as "h1";
      return (
        <Tag className={cn(HEADING[block.level - 1], "text-fg [&:not(:first-child)]:mt-4")}>
          <Inline nodes={block.children} />
        </Tag>
      );
    }
    case "paragraph":
      return (
        <p className="type-body text-fg">
          <Inline nodes={block.children} />
        </p>
      );
    case "list": {
      const Tag = block.ordered ? "ol" : "ul";
      return (
        <Tag className={cn("flex flex-col gap-1 pl-6 type-body text-fg", block.ordered ? "list-decimal" : "list-disc")} start={block.ordered ? block.start : undefined}>
          {block.items.map((item, i) => (
            <li key={i}>
              <Inline nodes={item} />
            </li>
          ))}
        </Tag>
      );
    }
    case "quote":
      return (
        <blockquote className="rounded-md bg-sunken px-4 py-3 type-body text-fg-muted">
          <Inline nodes={block.children} />
        </blockquote>
      );
    case "code":
      return (
        <pre className="overflow-x-auto rounded-md bg-sunken p-4 font-mono type-label text-fg">
          <code>{block.text}</code>
        </pre>
      );
    case "table":
      return (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full border-collapse type-label">
            <thead className="bg-sunken">
              <tr>
                {block.header.map((c, i) => (
                  <th key={i} className="px-3 py-2 text-left font-semibold text-fg">
                    <Inline nodes={c} />
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row, r) => (
                <tr key={r} className="border-t border-border">
                  {row.map((c, i) => (
                    <td key={i} className="px-3 py-2 text-fg">
                      <Inline nodes={c} />
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

/** Markdown drawn as components from a parsed tree (never HTML from the file; links only http(s), mailto and relative). */
export function MarkdownView({ text, className }: { text: string; className?: string }) {
  const blocks = useMemo(() => filePreviewParseMarkdown(text), [text]);
  if (blocks.length === 0) return <EmptyState compact title="This file is empty" />;
  return (
    <div className={cn("overflow-auto rounded-lg border border-border bg-surface p-card", className)}>
      <article className="flex max-w-prose flex-col gap-3">
        {blocks.map((b, i) => (
          <MdBlock key={i} block={b} />
        ))}
      </article>
    </div>
  );
}

const ROW_H = 36;

/**
 * A table that only draws the rows in view, so 100,000-row sheets scroll smoothly. Column
 * widths come from the header and the first 500 rows.
 */
export function VirtualTable({ header, rows, className, label }: { header: string[]; rows: string[][]; className?: string; label: string }) {
  const scroller = useRef<HTMLDivElement>(null);
  const [range, setRange] = useState({ top: 0, height: 600 });
  const cols = Math.max(header.length, ...rows.slice(0, 500).map((r) => r.length));
  const widths = useMemo(() => {
    const w: number[] = [];
    for (let c = 0; c < cols; c++) {
      let len = (header[c] ?? "").length;
      for (let r = 0; r < Math.min(rows.length, 500); r++) len = Math.max(len, (rows[r]?.[c] ?? "").length);
      w.push(Math.min(360, Math.max(72, len * 9 + 32)));
    }
    return w;
  }, [header, rows, cols]);
  const numW = Math.max(48, String(rows.length).length * 9 + 24);
  const total = widths.reduce((a, b) => a + b, numW);

  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const update = () => setRange({ top: el.scrollTop, height: el.clientHeight });
    update();
    el.addEventListener("scroll", update, { passive: true });
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(update) : null;
    ro?.observe(el);
    return () => {
      el.removeEventListener("scroll", update);
      ro?.disconnect();
    };
  }, []);

  const first = Math.max(0, Math.floor(range.top / ROW_H) - 10);
  const last = Math.min(rows.length, Math.ceil((range.top + range.height) / ROW_H) + 10);
  const cell = "truncate border-r border-border px-3 leading-9 last:border-r-0";

  return (
    <div ref={scroller} role="table" aria-label={label} aria-rowcount={rows.length + 1} className={cn("overflow-auto rounded-lg border border-border bg-surface type-label", className)}>
      <div style={{ width: total, minWidth: "100%" }}>
        <div role="row" aria-rowindex={1} className="sticky top-0 z-10 flex border-b border-border bg-sunken font-semibold text-fg">
          <div role="columnheader" className={cn(cell, "text-right text-fg-subtle")} style={{ width: numW, minWidth: numW }}>
            <span className="sr-only">Row</span>
          </div>
          {widths.map((w, c) => (
            <div key={c} role="columnheader" title={header[c]} className={cell} style={{ width: w, minWidth: w }}>
              {header[c] ?? ""}
            </div>
          ))}
        </div>
        <div className="relative" style={{ height: rows.length * ROW_H }}>
          {rows.slice(first, last).map((r, i) => {
            const index = first + i;
            return (
              <div key={index} role="row" aria-rowindex={index + 2} className="absolute inset-x-0 flex border-b border-border text-fg" style={{ top: index * ROW_H, height: ROW_H }}>
                <div role="rowheader" className={cn(cell, "text-right text-fg-subtle tabular-nums")} style={{ width: numW, minWidth: numW }}>
                  {index + 1}
                </div>
                {widths.map((w, c) => (
                  <div key={c} role="cell" title={r[c]} className={cell} style={{ width: w, minWidth: w }}>
                    {r[c] ?? ""}
                  </div>
                ))}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

/** CSV or TSV as a table (header row first); up to 100,000 rows. */
export function CsvView({ text, name, className }: { text: string; name: string; className?: string }) {
  const { header, rows, truncated } = useMemo(() => {
    const parsed = filePreviewParseCsv(text, 100_001, filePreviewCsvDelimiter(name, text));
    const [h, ...rest] = parsed.rows;
    return { header: h ?? [], rows: rest, truncated: parsed.truncated };
  }, [text, name]);
  if (header.length === 0) return <EmptyState compact title="This file is empty" />;
  return (
    <div className="flex flex-col gap-2">
      <VirtualTable header={header} rows={rows} className={className} label={name} />
      <p className="type-caption text-fg-muted">
        {truncated ? `Showing the first ${rows.length.toLocaleString()} rows.` : `${rows.length.toLocaleString()} ${rows.length === 1 ? "row" : "rows"}`}
      </p>
    </div>
  );
}
