"use client";

import type { FilePreviewOfficeFormat } from "@scaffoldapp/shared/preview";
import { useEffect, useState, type ReactNode } from "react";
import { Skeleton } from "../components/Skeleton";
import { cn } from "../lib/cn";
import { fetchBytes, useLoaded } from "./load";
import { parseOffice, type DocBlock, type OfficeDoc, type Run } from "./office";
import { VirtualTable } from "./TextViews";

/** Largest office file read in the browser. */
const MAX_OFFICE_BYTES = 40 * 1024 * 1024;

function Runs({ runs }: { runs: Run[] }): ReactNode {
  return runs.map((r, i) => {
    const cls = cn(r.b && "font-semibold", r.i && "italic", r.u && "underline", r.s && "line-through", "whitespace-pre-wrap");
    return r.href ? (
      <a key={i} href={r.href} target="_blank" rel="noopener noreferrer nofollow" className={cn(cls, "text-primary-strong underline underline-offset-2")}>
        {r.text}
      </a>
    ) : (
      <span key={i} className={cls || undefined}>
        {r.text}
      </span>
    );
  });
}

const HEADING = ["type-headline", "type-title", "type-title-sm", "type-body-lg font-semibold", "type-body font-semibold", "type-body font-semibold"];

function Block({ b }: { b: DocBlock }) {
  switch (b.type) {
    case "heading": {
      const Tag = `h${Math.min(6, Math.max(1, b.level))}` as "h2";
      return (
        <Tag className={cn(HEADING[b.level - 1], "text-fg [&:not(:first-child)]:mt-4")}>
          <Runs runs={b.runs} />
        </Tag>
      );
    }
    case "paragraph":
      return (
        <p className="type-body text-fg">
          <Runs runs={b.runs} />
        </p>
      );
    case "list": {
      const Tag = b.ordered ? "ol" : "ul";
      return (
        <Tag className={cn("flex flex-col gap-1 pl-6 type-body text-fg", b.ordered ? "list-decimal" : "list-disc")}>
          {b.items.map((runs, i) => (
            <li key={i}>
              <Runs runs={runs} />
            </li>
          ))}
        </Tag>
      );
    }
    case "table":
      return (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full border-collapse type-label">
            <tbody>
              {b.rows.map((row, r) => (
                <tr key={r} className="border-t border-border first:border-t-0">
                  {row.map((cell, c) => (
                    <td key={c} className="border-l border-border px-3 py-2 align-top text-fg first:border-l-0">
                      <Runs runs={cell} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case "image":
      return (
        // eslint-disable-next-line @next/next/no-img-element -- a blob URL of an image inside the document
        <img src={b.src} alt={b.alt} className="max-h-[480px] max-w-full self-start rounded-md object-contain" />
      );
  }
}

function letters(n: number): string {
  let s = "";
  for (let i = n + 1; i > 0; i = Math.floor((i - 1) / 26)) s = String.fromCharCode(65 + ((i - 1) % 26)) + s;
  return s;
}

function Sheets({ doc, name, className }: { doc: Extract<OfficeDoc, { kind: "sheet" }>; name: string; className?: string }) {
  const [index, setIndex] = useState(0);
  const sheet = doc.sheets[index];
  if (!sheet || doc.sheets.every((s) => s.rows.length === 0)) return <p className="rounded-lg border border-border bg-surface p-card type-body text-fg-muted">This spreadsheet is empty.</p>;
  const cols = Math.max(1, ...sheet.rows.slice(0, 500).map((r) => r.length));
  const header = Array.from({ length: cols }, (_, i) => letters(i));
  return (
    <div className="flex flex-col gap-2">
      <VirtualTable header={header} rows={sheet.rows} className={className} label={`${name}, ${sheet.name}`} />
      <div className="flex items-center gap-1 overflow-x-auto" role="tablist" aria-label="Sheets">
        {doc.sheets.map((s, i) => (
          <button
            key={i}
            type="button"
            role="tab"
            aria-selected={i === index}
            onClick={() => setIndex(i)}
            className={cn(
              "shrink-0 rounded-md px-3 py-1.5 type-label outline-none focus-visible:ring-2 focus-visible:ring-ring",
              i === index ? "bg-primary-soft font-semibold text-primary-strong" : "text-fg-muted hover:bg-sunken",
            )}
          >
            {s.name}
          </button>
        ))}
      </div>
      {sheet.truncated ? <p className="type-caption text-fg-muted">Showing the first 5,000 rows and 200 columns.</p> : null}
    </div>
  );
}

function Slides({ doc, className }: { doc: Extract<OfficeDoc, { kind: "slides" }>; className?: string }) {
  if (doc.slides.length === 0) return <p className="rounded-lg border border-border bg-surface p-card type-body text-fg-muted">This presentation has no slides.</p>;
  return (
    <ol className={cn("flex flex-col gap-3 overflow-auto", className)} aria-label="Slides">
      {doc.slides.map((s, i) => (
        <li key={i} className="flex gap-3 rounded-lg border border-border bg-surface p-card">
          <span className="w-6 shrink-0 text-right type-caption text-fg-subtle tabular-nums">{i + 1}</span>
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <h3 className="type-title-sm text-fg">{s.title || `Slide ${i + 1}`}</h3>
            {s.paragraphs.length ? (
              <ul className="flex flex-col gap-1">
                {s.paragraphs.map((p, j) => (
                  <li key={j} className="type-body whitespace-pre-wrap text-fg" style={{ paddingLeft: `${p.level * 16}px` }}>
                    {p.text}
                  </li>
                ))}
              </ul>
            ) : null}
            {s.images.length ? (
              <div className="flex flex-wrap gap-2">
                {s.images.map((src, j) => (
                  // eslint-disable-next-line @next/next/no-img-element -- a blob URL of an image inside the slides
                  <img key={j} src={src} alt="" className="h-28 max-w-full rounded-md border border-border object-contain" />
                ))}
              </div>
            ) : null}
          </div>
        </li>
      ))}
    </ol>
  );
}

/** A docx/xlsx/pptx/odt/ods/odp file read in the browser: text with formatting, tables and images; sheets as tables; slides as an outline. */
export default function OfficeView({ url, name, format, size, className, fallback }: { url: string; name: string; format: FilePreviewOfficeFormat; size?: number; className?: string; fallback: (reason: string) => ReactNode }) {
  const tooBig = (size ?? 0) > MAX_OFFICE_BYTES;
  const state = useLoaded(tooBig ? null : url, async (signal) => {
    const { bytes } = await fetchBytes(url, MAX_OFFICE_BYTES, signal);
    return parseOffice(bytes, format);
  });
  const images = state.status === "ready" ? state.data.images : null;
  useEffect(() => () => images?.forEach((u) => URL.revokeObjectURL(u)), [images]);

  if (tooBig) return fallback("too_large");
  if (state.status === "loading") return <Skeleton className={cn("h-96 w-full rounded-lg", className)} />;
  if (state.status === "error") return fallback(state.error === "too_large" ? "too_large" : "unreadable");
  const { doc } = state.data;
  if (doc.kind === "sheet") return <Sheets doc={doc} name={name} className={className} />;
  if (doc.kind === "slides") return <Slides doc={doc} className={className} />;
  if (doc.blocks.length === 0) return <p className="rounded-lg border border-border bg-surface p-card type-body text-fg-muted">This document is empty.</p>;
  return (
    <div className={cn("overflow-auto rounded-lg border border-border bg-surface p-card", className)}>
      <article className="mx-auto flex max-w-prose flex-col gap-3">
        {doc.blocks.map((b, i) => (
          <Block key={i} b={b} />
        ))}
        {doc.truncated ? <p className="type-caption text-fg-muted">Showing the first 5,000 paragraphs.</p> : null}
      </article>
    </div>
  );
}
