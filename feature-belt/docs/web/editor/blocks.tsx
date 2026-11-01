"use client";

import { DOCS_CODE_LANGUAGES, docsTitle, useDocsFileUrl, type DocsBlock, type DocsSpan } from "@scaffoldapp/shared";
import { Select, Skeleton, cn } from "@scaffoldapp/ui-web";
import { Download, FileText, Globe, Lock, Plus, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { InlineDatabase } from "../database/DatabaseView";
import { formatBytes } from "../format";
import { useEditor } from "./context";
import { RichText } from "./RichText";

/** Non-text blocks in the editor. */
export function BlockBody({ block }: { block: DocsBlock }) {
  switch (block.type) {
    case "code":
      return <CodeBlock block={block} />;
    case "divider":
      return (
        <div className="flex h-6 items-center" aria-label="Divider" role="separator">
          <hr className="w-full border-border" />
        </div>
      );
    case "image":
      return <ImageBlock block={block} />;
    case "file":
      return <FileBlock block={block} />;
    case "bookmark":
      return <BookmarkBlock block={block} />;
    case "page":
      return <PageLinkBlock block={block} />;
    case "table":
      return <TableBlock block={block} />;
    case "database":
      return block.props.database_id ? <InlineDatabase databaseId={block.props.database_id} /> : null;
    default:
      return null;
  }
}

function Caption({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  const caption = block.props.caption ?? [];
  if (!ctx.editable && caption.length === 0) return null;
  return (
    <RichText
      ref={(h) => ctx.register(block.id, h)}
      spans={caption}
      refs={ctx.refs}
      editable={ctx.editable}
      placeholder="Add a caption"
      ariaLabel="Caption"
      className="py-1 type-label text-fg-muted"
      onChange={(spans) => ctx.apply([{ op: "update", id: block.id, props: { caption: spans } }], undefined, "merge", `caption:${block.id}`)}
      onKeyDown={(e) => {
        if (e.key === "Enter" && !e.shiftKey) {
          e.preventDefault();
          ctx.onEnter(block, false);
        } else if (e.key === "Escape" || (e.key === "ArrowUp" && !e.shiftKey) || (e.key === "ArrowDown" && !e.shiftKey)) {
          ctx.onTextKeyDown(block, e);
        }
      }}
      onFocus={() => ctx.onFocusBlock(block)}
      onOpenPage={ctx.openPage}
    />
  );
}

function CodeBlock({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  const ref = useRef<HTMLTextAreaElement>(null);
  const code = block.props.code ?? "";
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [code]);
  useEffect(() => {
    ctx.register(block.id, {
      get el() {
        return null;
      },
      focus(at = "end") {
        const el = ref.current;
        if (!el) return;
        el.focus();
        const p = at === "start" ? 0 : at === "end" ? el.value.length : at;
        el.setSelectionRange(p, p);
      },
      caret() {
        const el = ref.current;
        return el ? { start: el.selectionStart, end: el.selectionEnd } : null;
      },
    });
    return () => ctx.register(block.id, null);
  }, [ctx, block.id]);
  const set = (v: string) => ctx.apply([{ op: "update", id: block.id, props: { code: v } }], undefined, "merge", `code:${block.id}`);
  return (
    <div className="relative rounded-md bg-sunken">
      <div className="flex items-center justify-between gap-2 px-3 pt-2">
        {ctx.editable ? (
          <Select
            aria-label="Language"
            className="h-8 w-36"
            value={block.props.language ?? "plain"}
            options={DOCS_CODE_LANGUAGES.map((l) => ({ value: l, label: l === "plain" ? "Plain text" : l }))}
            onValueChange={(v) => ctx.apply([{ op: "update", id: block.id, props: { language: v } }])}
          />
        ) : (
          <span className="type-caption text-fg-muted">{block.props.language === "plain" ? "Plain text" : block.props.language}</span>
        )}
      </div>
      <textarea
        ref={ref}
        value={code}
        readOnly={!ctx.editable}
        spellCheck={false}
        aria-label="Code"
        rows={1}
        className="block w-full resize-none overflow-hidden bg-transparent px-3 pt-2 pb-3 font-mono type-label text-fg outline-none"
        onChange={(e) => set(e.target.value)}
        onFocus={() => ctx.onFocusBlock(block)}
        onKeyDown={(e) => {
          const el = e.currentTarget;
          if (e.key === "Tab" && !e.shiftKey) {
            e.preventDefault();
            const s = el.selectionStart;
            const next = `${code.slice(0, s)}  ${code.slice(el.selectionEnd)}`;
            set(next);
            requestAnimationFrame(() => el.setSelectionRange(s + 2, s + 2));
          } else if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            ctx.onEnter(block, false);
          } else if (e.key === "Backspace" && code === "") {
            e.preventDefault();
            ctx.apply([{ op: "update", id: block.id, type: "paragraph", props: { text: [] } }], { block: block.id, start: 0, end: 0 });
            ctx.focusBlock(block.id, "start");
          } else if (e.key === "Escape" || (e.key === "ArrowUp" && el.selectionStart === 0) || (e.key === "ArrowDown" && el.selectionEnd === code.length)) {
            ctx.onTextKeyDown(block, e as unknown as React.KeyboardEvent<HTMLDivElement>);
          } else if ((e.key === "z" || e.key === "y") && (e.metaKey || e.ctrlKey)) {
            ctx.onTextKeyDown(block, e as unknown as React.KeyboardEvent<HTMLDivElement>);
          }
        }}
      />
    </div>
  );
}

function ImageBlock({ block }: { block: DocsBlock }) {
  const file = useDocsFileUrl(block.props.file_id);
  const width = block.props.width ?? 100;
  return (
    <figure className="flex flex-col gap-1">
      <div style={{ width: `${width}%` }} className="max-w-full">
        {file.data?.url ? (
          // Private, short-lived links from storage: next/image can't optimise them.
          // eslint-disable-next-line @next/next/no-img-element
          <img src={file.data.url} alt={(block.props.caption ?? []).map((s) => s.text).join("") || block.props.name || "Image"} loading="lazy" className="w-full rounded-md border border-border" />
        ) : file.isError ? (
          <div className="flex h-32 items-center justify-center rounded-md border border-dashed border-border type-label text-fg-muted">This image isn't available.</div>
        ) : (
          <Skeleton className="h-48 w-full rounded-md" />
        )}
      </div>
      <figcaption>
        <Caption block={block} />
      </figcaption>
    </figure>
  );
}

function FileBlock({ block }: { block: DocsBlock }) {
  const [want, setWant] = useState(false);
  const file = useDocsFileUrl(want ? block.props.file_id : null);
  useEffect(() => {
    if (want && file.data?.url) {
      window.open(file.data.url, "_blank", "noopener,noreferrer");
      setWant(false);
    }
  }, [want, file.data?.url]);
  return (
    <div className="flex flex-col gap-1">
      <button
        type="button"
        onClick={() => setWant(true)}
        className="flex min-h-12 items-center gap-3 rounded-md border border-border bg-surface px-3 py-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
      >
        <FileText aria-hidden className="size-5 shrink-0 text-fg-muted" />
        <span className="min-w-0 flex-1 truncate type-body text-fg">{block.props.name ?? "File"}</span>
        <span className="shrink-0 type-caption text-fg-muted">{formatBytes(block.props.size ?? 0)}</span>
        <Download aria-hidden className="size-4 shrink-0 text-fg-muted" />
        <span className="sr-only">Download</span>
      </button>
      {file.isError ? <p className="type-caption text-danger-strong">This file isn't available.</p> : null}
      <Caption block={block} />
    </div>
  );
}

function host(url: string | undefined): string {
  try {
    return url ? new URL(url).host : "";
  } catch {
    return url ?? "";
  }
}

function BookmarkBlock({ block }: { block: DocsBlock }) {
  return (
    <div className="flex flex-col gap-1">
      <a
        href={block.props.url}
        target="_blank"
        rel="noopener noreferrer nofollow"
        className="flex min-h-12 items-center gap-3 rounded-md border border-border bg-surface px-3 py-2 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
      >
        <Globe aria-hidden className="size-5 shrink-0 text-fg-muted" />
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="truncate type-body text-fg">{block.props.title || host(block.props.url)}</span>
          <span className="truncate type-caption text-fg-muted">{host(block.props.url)}</span>
        </span>
      </a>
      <Caption block={block} />
    </div>
  );
}

function PageLinkBlock({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  const ref = ctx.refs.pages[block.props.page_id ?? ""];
  const noAccess = ref ? ref.title === null : false;
  return (
    <button
      type="button"
      disabled={noAccess}
      onClick={() => block.props.page_id && ctx.openPage(block.props.page_id)}
      className="flex min-h-control w-full items-center gap-2 rounded-sm px-1 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-default disabled:hover:bg-transparent"
    >
      <span aria-hidden className="flex size-6 items-center justify-center type-body">
        {noAccess ? <Lock className="size-4 text-fg-subtle" /> : ref?.icon ? ref.icon : <FileText className="size-4 text-fg-muted" />}
      </span>
      <span className={cn("min-w-0 flex-1 truncate type-body underline decoration-border-strong underline-offset-4", noAccess ? "text-fg-subtle no-underline" : "text-fg")}>
        {noAccess ? "No access" : docsTitle(ref?.title)}
      </span>
      {ref?.deleted ? (
        <span className="flex items-center gap-1 type-caption text-fg-muted">
          <Trash2 aria-hidden className="size-3.5" />
          In trash
        </span>
      ) : null}
    </button>
  );
}

function TableBlock({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  const rows = block.props.rows ?? [[[]]];
  const cols = Math.max(1, ...rows.map((r) => r.length));
  const cellRefs = useRef(new Map<string, { focus: (at?: "start" | "end") => void }>());
  const setRows = (next: DocsSpan[][][], key?: string) => ctx.apply([{ op: "update", id: block.id, props: { rows: next } }], undefined, key ? "merge" : "push", key);
  const setCell = (r: number, c: number, spans: DocsSpan[]) => {
    const next = rows.map((row) => [...row]);
    while (next[r]!.length < cols) next[r]!.push([]);
    next[r]![c] = spans;
    setRows(next, `cell:${block.id}:${r}:${c}`);
  };
  const addRow = () => setRows([...rows, Array.from({ length: cols }, () => [] as DocsSpan[])]);
  const addCol = () => setRows(rows.map((r) => [...r, []]));
  const focusCell = (r: number, c: number) => requestAnimationFrame(() => cellRefs.current.get(`${r}:${c}`)?.focus("end"));
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-start gap-1">
        <div className="min-w-0 flex-1 overflow-x-auto">
          <table className="w-full border-collapse type-label">
            <tbody>
              {rows.map((row, r) => (
                <tr key={r}>
                  {Array.from({ length: cols }, (_, c) => {
                    const header = (block.props.header_row && r === 0) || (block.props.header_col && c === 0);
                    return (
                      <td key={c} className={cn("min-w-24 border border-border px-2 align-top", header && "bg-sunken font-semibold")}>
                        <RichText
                          ref={(h) => {
                            if (h) cellRefs.current.set(`${r}:${c}`, { focus: (at) => h.focus(at ?? "end") });
                            else cellRefs.current.delete(`${r}:${c}`);
                          }}
                          spans={row[c] ?? []}
                          refs={ctx.refs}
                          editable={ctx.editable}
                          ariaLabel={`Row ${r + 1}, column ${c + 1}`}
                          className="min-h-8 py-1"
                          onChange={(spans) => setCell(r, c, spans)}
                          onFocus={() => ctx.onFocusBlock(block)}
                          onOpenPage={ctx.openPage}
                          onKeyDown={(e) => {
                            if (e.key === "Tab") {
                              e.preventDefault();
                              const i = r * cols + c + (e.shiftKey ? -1 : 1);
                              if (i >= 0 && i < rows.length * cols) focusCell(Math.floor(i / cols), i % cols);
                            } else if (e.key === "Enter" && !e.shiftKey) {
                              e.preventDefault();
                              if (r + 1 >= rows.length) addRow();
                              focusCell(r + 1, c);
                            } else if (e.key === "Escape" || ((e.key === "z" || e.key === "y") && (e.metaKey || e.ctrlKey))) {
                              ctx.onTextKeyDown(block, e);
                            }
                          }}
                        />
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {ctx.editable && cols < 20 ? (
          <button type="button" aria-label="Add a column" title="Add a column" onClick={addCol} className="flex h-8 w-6 shrink-0 items-center justify-center rounded-sm text-fg-subtle hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring">
            <Plus aria-hidden className="size-4" />
          </button>
        ) : null}
      </div>
      {ctx.editable && rows.length < 100 ? (
        <button type="button" onClick={addRow} className="flex h-8 items-center justify-center gap-1 rounded-sm type-caption text-fg-subtle hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring">
          <Plus aria-hidden className="size-4" />
          Add a row
        </button>
      ) : null}
    </div>
  );
}
