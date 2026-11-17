"use client";

import { docsTitle, useDocsFileUrl, type DocsFileValue, type DocsProperty, type DocsRow } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import { FileText } from "lucide-react";
import { useState, type ReactNode } from "react";
import { EditableValue, RowMenu } from "./cells";
import { isEmptyValue } from "./PropertyValue";
import type { ViewProps } from "./types";

/** The first image of a files property, for card previews. */
export function firstImage(row: DocsRow, propId: string | undefined): DocsFileValue | null {
  if (!propId) return null;
  const files = row.props[propId];
  if (!Array.isArray(files)) return null;
  for (const f of files as DocsFileValue[]) {
    if (f && typeof f === "object" && f.file_id && (f.mime ?? "").startsWith("image/")) return f;
    if (f && typeof f === "object" && f.file_id && /\.(png|jpe?g|gif|webp|avif)$/i.test(f.name)) return f;
  }
  return null;
}

/** A card's image from a files property (fetched only for cards that show one). */
export function CardImage({ file, fit, className, token }: { file: DocsFileValue; fit?: boolean | undefined; className?: string; token?: string | null | undefined }) {
  const url = useDocsFileUrl(file.file_id ?? null, token);
  const [failed, setFailed] = useState(false);
  return (
    <div className={cn("relative overflow-hidden bg-sunken", className)}>
      {url.data?.url && !failed ? (
        // Presigned links from storage: next/image can't optimise them.
        // eslint-disable-next-line @next/next/no-img-element
        <img src={url.data.url} alt="" loading="lazy" onError={() => setFailed(true)} className={cn("size-full", fit ? "object-contain" : "object-cover")} />
      ) : (
        <div className="flex size-full items-center justify-center text-fg-subtle">{url.isPending ? null : <FileText aria-hidden className="size-6" />}</div>
      )}
    </div>
  );
}

/** Properties shown on a card (values only, empty ones left out), each editable in place. */
export function CardProps({ row, props, shown }: { row: DocsRow; props: ViewProps; shown: DocsProperty[] }) {
  const list = shown.filter((p) => p.type !== "title" && (p.type === "button" || p.type === "created_time" || p.type === "last_edited_time" || p.type === "created_by" || p.type === "last_edited_by" || !isEmptyValue(row.props[p.id])));
  if (!list.length) return null;
  return (
    <div className="flex flex-col gap-0.5">
      {list.map((p) => (
        <div key={p.id} className="-mx-2 flex min-w-0 items-center" title={p.name}>
          <EditableValue p={p} row={row} props={props} className="min-h-7" />
        </div>
      ))}
    </div>
  );
}

/** A card's title row: icon, title (opens the row) and the row's ⋮ menu. */
export function CardTitle({ row, props, extra, className }: { row: DocsRow; props: ViewProps; extra?: ReactNode; className?: string }) {
  return (
    <div className={cn("flex items-start gap-1", className)}>
      <button
        type="button"
        className="flex min-h-7 min-w-0 flex-1 items-start gap-1.5 text-left type-label font-medium text-fg hover:underline focus-visible:outline-2 focus-visible:outline-ring"
        onClick={() => props.onOpenRow(row.id)}
      >
        {row.icon ? <span aria-hidden>{row.icon}</span> : null}
        <span className="min-w-0 break-words">{docsTitle(row.title)}</span>
      </button>
      {extra}
      <RowMenu row={row} props={props} />
    </div>
  );
}
