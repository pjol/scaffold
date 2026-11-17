"use client";

import {
  DOCS_CODE_LANGUAGES,
  docsEmbedHeight,
  docsEmbedName,
  docsMarkdownToBlocks,
  docsTitle,
  useDocsCreateRow,
  useDocsPage,
  type DocsBlock,
  type DocsBlockButtonBlock,
  type DocsDraftBlock,
  type DocsSpan,
} from "@scaffoldapp/shared";
import { Button, Input, Select, cn, toast } from "@scaffoldapp/ui-web";
import { AlignCenter, AlignLeft, AlignRight, ArrowUpRight, Check, ChevronRight, Copy, FileText, Image as ImageIcon, Lock, Music, Paperclip, Plus, Sigma, Trash2, Video, WrapText, FileType2 } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { ConnectedPageBlock } from "../connected/ConnectedPageBlock";
import { InlineDatabase } from "../database/DatabaseView";
import { Floating } from "../Floating";
import { ButtonEditor } from "./ButtonEditor";
import { ColumnList } from "./columns";
import { useEditor } from "./context";
import { AudioBody, BookmarkBody, CodeTokens, EmbedBody, EquationBody, FileBody, ImageBody, jumpToBlock, MediaFrame, MermaidDiagram, PdfBody, TocBody, useHighlighted, VideoBody } from "./render";
import { RichText } from "./RichText";
import { SyncedBlock } from "./synced";

/** Non-text blocks in the editor. */
export function BlockBody({ block }: { block: DocsBlock }) {
  switch (block.type) {
    case "code":
      return <CodeBlock block={block} />;
    case "divider":
      return (
        <div className="flex h-6 items-center" aria-label="Divider" role="separator">
          <hr className="w-full border-current opacity-30" />
        </div>
      );
    case "image":
      return <MediaBlock block={block} />;
    case "video":
      return <MediaBlock block={block} />;
    case "audio":
    case "pdf":
    case "file":
      return <FileLikeBlock block={block} />;
    case "bookmark":
      return <BookmarkBlock block={block} />;
    case "embed":
      return <EmbedBlock block={block} />;
    case "page":
    case "link_to_page":
      return <PageLinkBlock block={block} />;
    case "table":
      return <TableBlock block={block} />;
    case "database":
      return block.props.database_id ? <DatabaseBlock block={block} /> : null;
    case "connected_page":
      return <ConnectedPageInEditor block={block} />;
    case "equation":
      return <EquationBlock block={block} />;
    case "table_of_contents":
      return <TocBlock block={block} />;
    case "breadcrumb":
      return <BreadcrumbBlock />;
    case "button":
      return <ButtonBlock block={block} />;
    case "column_list":
      return <ColumnList block={block} />;
    case "synced_block":
      return <SyncedBlock block={block} />;
    default:
      return null;
  }
}

function DatabaseBlock({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  // blockId and pageId let a linked view keep its own views (docs-databases); extra props are ignored until then.
  const props = { databaseId: block.props.database_id!, blockId: block.id, pageId: ctx.pageId };
  return <InlineDatabase {...props} />;
}

function Caption({ block, placeholder = "Add a caption" }: { block: DocsBlock; placeholder?: string }) {
  const ctx = useEditor();
  // A connected page keeps some blocks read-only.
  const editable = ctx.editable && !ctx.isReadOnly(block.id);
  const caption = block.props.caption ?? [];
  if (!editable && caption.length === 0) return null;
  return (
    <RichText
      ref={(h) => ctx.register(block.id, h)}
      spans={caption}
      refs={ctx.refs}
      editable={editable}
      placeholder={placeholder}
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

function ToolChip({ label, onClick, active, children }: { label: string; onClick: () => void; active?: boolean; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      aria-pressed={active}
      onMouseDown={(e) => e.preventDefault()}
      onClick={onClick}
      className={cn(
        "flex size-8 items-center justify-center rounded-sm text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:size-11 [&_svg]:size-4",
        active && "bg-primary-soft text-primary-strong",
      )}
    >
      {children}
    </button>
  );
}

/** Hover tools over media: alignment and more. */
function MediaTools({ block, children }: { block: DocsBlock; children?: ReactNode }) {
  const ctx = useEditor();
  const align = block.props.align ?? "center";
  const set = (a: "left" | "center" | "right") => ctx.apply([{ op: "update", id: block.id, props: { align: a } }]);
  return (
    <div className="absolute top-2 right-2 z-10 flex items-center gap-0.5 rounded-md border border-border bg-surface-raised p-0.5 opacity-0 shadow-sm transition-opacity group-hover/media:opacity-100 group-focus-within/media:opacity-100 motion-reduce:transition-none pointer-coarse:opacity-100">
      <ToolChip label="Align left" active={align === "left"} onClick={() => set("left")}>
        <AlignLeft aria-hidden />
      </ToolChip>
      <ToolChip label="Align center" active={align === "center"} onClick={() => set("center")}>
        <AlignCenter aria-hidden />
      </ToolChip>
      <ToolChip label="Align right" active={align === "right"} onClick={() => set("right")}>
        <AlignRight aria-hidden />
      </ToolChip>
      {children}
    </div>
  );
}

/** Drag the side handles to resize (percent of the column), in steps of 1%; arrow keys on a handle step 5%. */
function ResizeHandles({ block, frame }: { block: DocsBlock; frame: React.RefObject<HTMLDivElement | null> }) {
  const ctx = useEditor();
  const start = (e: React.PointerEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const box = frame.current?.parentElement?.parentElement?.getBoundingClientRect();
    if (!box) return;
    const align = block.props.align ?? "center";
    const move = (ev: PointerEvent) => {
      let w: number;
      if (align === "center") w = (Math.abs(ev.clientX - (box.left + box.width / 2)) * 2) / box.width;
      else if (align === "left") w = (ev.clientX - box.left) / box.width;
      else w = (box.right - ev.clientX) / box.width;
      const pct = Math.round(Math.min(100, Math.max(10, w * 100)));
      ctx.apply([{ op: "update", id: block.id, props: { width: pct } }], undefined, "merge", `width:${block.id}`);
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  const key = (e: React.KeyboardEvent) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    const w = Math.min(100, Math.max(10, (block.props.width ?? 100) + (e.key === "ArrowRight" ? 5 : -5)));
    ctx.apply([{ op: "update", id: block.id, props: { width: w } }], undefined, "merge", `width:${block.id}`);
  };
  const cls =
    "absolute top-1/2 z-10 hidden h-12 w-3 -translate-y-1/2 cursor-ew-resize touch-none items-center justify-center rounded-pill focus-visible:outline-2 focus-visible:outline-ring sm:flex [&>span]:h-10 [&>span]:w-1 [&>span]:rounded-pill [&>span]:bg-fg-muted [&>span]:opacity-0 group-hover/media:[&>span]:opacity-70 focus-visible:[&>span]:opacity-70";
  return (
    <>
      <div role="slider" tabIndex={0} aria-label="Width" aria-valuemin={10} aria-valuemax={100} aria-valuenow={block.props.width ?? 100} aria-valuetext={`${block.props.width ?? 100}%`} className={cn(cls, "-left-1.5")} onPointerDown={start} onKeyDown={key}>
        <span />
      </div>
      <div role="slider" tabIndex={-1} aria-hidden aria-valuenow={block.props.width ?? 100} className={cn(cls, "-right-1.5")} onPointerDown={start}>
        <span />
      </div>
    </>
  );
}

/** Images and videos: width, alignment, caption; images also alt text. */
function MediaBlock({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  const editable = ctx.editable && !ctx.isReadOnly(block.id);
  const frame = useRef<HTMLDivElement>(null);
  const [altOpen, setAltOpen] = useState<HTMLElement | null>(null);
  return (
    <figure className="flex flex-col gap-1">
      <MediaFrame width={block.props.width} align={block.props.align}>
        <div ref={frame} className="group/media relative">
          {block.type === "image" ? <ImageBody block={block} token={null} /> : <VideoBody block={block} token={null} />}
          {editable && !block.props.url ? (
            <>
              <ResizeHandles block={block} frame={frame} />
              <MediaTools block={block}>
                {block.type === "image" ? (
                  <ToolChip label="Alt text" active={!!block.props.alt} onClick={() => setAltOpen(frame.current)}>
                    <FileType2 aria-hidden />
                  </ToolChip>
                ) : null}
              </MediaTools>
            </>
          ) : null}
        </div>
      </MediaFrame>
      <figcaption className={cn(block.props.align === "center" || !block.props.align ? "text-center" : block.props.align === "right" ? "text-right" : "")}>
        <Caption block={block} />
      </figcaption>
      {altOpen ? (
        <AltForm
          anchor={altOpen}
          initial={block.props.alt ?? ""}
          onClose={() => setAltOpen(null)}
          onSave={(alt) => {
            setAltOpen(null);
            ctx.apply([{ op: "update", id: block.id, props: { alt } }]);
          }}
        />
      ) : null}
    </figure>
  );
}

function AltForm({ anchor, initial, onSave, onClose }: { anchor: HTMLElement; initial: string; onSave: (v: string) => void; onClose: () => void }) {
  const [v, setV] = useState(initial);
  return (
    <Floating open anchor={anchor} onClose={onClose} autoFocus label="Alt text" className="w-80 p-3">
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          onSave(v.trim());
        }}
      >
        <Input value={v} onChange={(e) => setV(e.target.value)} placeholder="Describe the image for people who can't see it" aria-label="Alt text" maxLength={1000} data-autofocus />
        <div className="flex justify-end">
          <Button type="submit" size="sm">
            Save
          </Button>
        </div>
      </form>
    </Floating>
  );
}

/** Audio, PDF and other files: the core preview (player, viewer, or a card that opens a preview), plus a caption. */
function FileLikeBlock({ block }: { block: DocsBlock }) {
  return (
    <div className="flex flex-col gap-1">
      {block.type === "audio" ? <AudioBody block={block} token={null} /> : block.type === "pdf" ? <PdfBody block={block} token={null} /> : <FileBody block={block} token={null} />}
      <Caption block={block} />
    </div>
  );
}

function BookmarkBlock({ block }: { block: DocsBlock }) {
  return (
    <div className="flex flex-col gap-1">
      <BookmarkBody block={block} token={null} />
      <Caption block={block} />
    </div>
  );
}

function EmbedBlock({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  const editable = ctx.editable && !ctx.isReadOnly(block.id);
  const base = block.props.height ?? docsEmbedHeight(block.props.provider);
  const [height, setHeight] = useState<number | null>(null);
  const startResize = (e: React.PointerEvent) => {
    e.preventDefault();
    const y0 = e.clientY;
    let h = base;
    const move = (ev: PointerEvent) => {
      h = Math.round(Math.min(1600, Math.max(120, base + ev.clientY - y0)));
      setHeight(h);
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      setHeight(null);
      ctx.apply([{ op: "update", id: block.id, props: { height: h } }]);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  return (
    <figure className="flex flex-col gap-1">
      <MediaFrame width={block.props.width} align={block.props.align}>
        <div className="group/media relative">
          <EmbedBody block={block} height={height ?? base} />
          {editable ? (
            <>
              <MediaTools block={block}>
                <ToolChip label={`Open in ${docsEmbedName(block.props.provider)}`} onClick={() => window.open(block.props.url, "_blank", "noopener,noreferrer")}>
                  <ArrowUpRight aria-hidden />
                </ToolChip>
              </MediaTools>
              <div
                role="slider"
                tabIndex={0}
                aria-label="Height"
                aria-valuemin={120}
                aria-valuemax={1600}
                aria-valuenow={height ?? base}
                className="absolute -bottom-1.5 left-1/2 z-10 flex h-3 w-12 -translate-x-1/2 cursor-ns-resize touch-none items-center justify-center rounded-pill focus-visible:outline-2 focus-visible:outline-ring"
                onPointerDown={startResize}
                onKeyDown={(e) => {
                  if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return;
                  e.preventDefault();
                  ctx.apply([{ op: "update", id: block.id, props: { height: Math.min(1600, Math.max(120, base + (e.key === "ArrowDown" ? 40 : -40))) } }]);
                }}
              >
                <span className="h-1 w-10 rounded-pill bg-fg-muted opacity-0 group-hover/media:opacity-70" />
              </div>
            </>
          ) : null}
        </div>
      </MediaFrame>
      <Caption block={block} />
    </figure>
  );
}

// ---- code ------------------------------------------------------------------------------

function CodeBlock({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  // A connected page keeps some blocks read-only.
  const editable = ctx.editable && !ctx.isReadOnly(block.id);
  const ref = useRef<HTMLTextAreaElement>(null);
  const code = block.props.code ?? "";
  const language = block.props.language ?? "plain";
  const wrap = !!block.props.wrap;
  const lines = useHighlighted(code, language);
  const [copied, setCopied] = useState(false);
  const [showCode, setShowCode] = useState(language !== "mermaid" || !code.trim());
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
  const box = cn("col-start-1 row-start-1 m-0 min-w-0 px-3 pt-2 pb-3 font-mono type-label", wrap ? "whitespace-pre-wrap break-words" : "whitespace-pre");
  return (
    <div className="flex flex-col gap-1">
      <div className="group/code relative rounded-md bg-sunken">
        <div className="flex items-center justify-between gap-2 px-3 pt-2">
          {editable ? (
            <Select
              aria-label="Language"
              className="h-8 w-40"
              value={language}
              options={DOCS_CODE_LANGUAGES.map((l) => ({ value: l, label: l === "plain" ? "Plain text" : l === "mermaid" ? "Mermaid" : l }))}
              onValueChange={(v) => ctx.apply([{ op: "update", id: block.id, props: { language: v } }])}
            />
          ) : (
            <span className="type-caption text-fg-muted">{language === "plain" ? "Plain text" : language}</span>
          )}
          <div className="flex items-center gap-0.5 opacity-100 transition-opacity sm:opacity-0 sm:group-hover/code:opacity-100 sm:group-focus-within/code:opacity-100 motion-reduce:transition-none">
            {language === "mermaid" && code.trim() ? (
              <Button size="sm" variant="ghost" onClick={() => setShowCode((s) => !s)}>
                {showCode ? "Hide code" : "Edit code"}
              </Button>
            ) : null}
            {editable ? (
              <ToolChip label={wrap ? "Don't wrap lines" : "Wrap lines"} active={wrap} onClick={() => ctx.apply([{ op: "update", id: block.id, props: { wrap: !wrap } }])}>
                <WrapText aria-hidden />
              </ToolChip>
            ) : null}
            <ToolChip
              label={copied ? "Copied" : "Copy code"}
              onClick={() => {
                void navigator.clipboard?.writeText(code).then(
                  () => {
                    setCopied(true);
                    window.setTimeout(() => setCopied(false), 1500);
                  },
                  () => toast.error("Couldn't copy the code."),
                );
              }}
            >
              {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
            </ToolChip>
          </div>
        </div>
        {language === "mermaid" ? <MermaidDiagram code={code} /> : null}
        {showCode || language !== "mermaid" ? (
          <div className={cn("grid", !wrap && "overflow-x-auto")}>
            <pre aria-hidden className={cn(box, "pointer-events-none text-fg")}>
              <CodeTokens code={code} lines={lines} />
              {"\n"}
            </pre>
            <textarea
              ref={ref}
              value={code}
              readOnly={!editable}
              spellCheck={false}
              aria-label="Code"
              wrap={wrap ? "soft" : "off"}
              className={cn(box, "resize-none overflow-hidden bg-transparent text-transparent caret-fg outline-none selection:bg-primary-soft selection:text-transparent")}
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
        ) : null}
      </div>
      <Caption block={block} placeholder="Add a caption" />
    </div>
  );
}

// ---- equations, contents, breadcrumb ----------------------------------------------------

/** A block equation: rendered, and edited in a TeX box under it while focused. */
function EquationBlock({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  const editable = ctx.editable && !ctx.isReadOnly(block.id);
  const [editing, setEditing] = useState(!block.props.expression && editable);
  const ref = useRef<HTMLTextAreaElement>(null);
  const expr = block.props.expression ?? "";
  useEffect(() => {
    ctx.register(block.id, {
      get el() {
        return null;
      },
      focus(at = "end") {
        setEditing(true);
        requestAnimationFrame(() => {
          const el = ref.current;
          if (!el) return;
          el.focus();
          const p = at === "start" ? 0 : at === "end" ? el.value.length : at;
          el.setSelectionRange(p, p);
        });
      },
      caret() {
        const el = ref.current;
        return el ? { start: el.selectionStart, end: el.selectionEnd } : null;
      },
    });
    return () => ctx.register(block.id, null);
  }, [ctx, block.id]);
  return (
    <div className="flex flex-col gap-1 rounded-md hover:bg-sunken/60">
      <button
        type="button"
        disabled={!editable}
        className="w-full rounded-md px-2 text-left focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-default"
        onClick={() => {
          setEditing(true);
          requestAnimationFrame(() => ref.current?.focus());
        }}
        aria-label={editable ? "Edit equation" : undefined}
      >
        <EquationBody expression={expr} placeholder={editable ? "Add a TeX equation" : ""} />
      </button>
      {editing && editable ? (
        <div className="flex items-start gap-2 rounded-md border border-border bg-surface p-2">
          <Sigma aria-hidden className="mt-1.5 size-4 shrink-0 text-fg-muted" />
          <textarea
            ref={ref}
            value={expr}
            rows={Math.min(8, Math.max(1, expr.split("\n").length))}
            spellCheck={false}
            aria-label="TeX"
            placeholder="E = mc^2"
            className="min-w-0 flex-1 resize-none bg-transparent font-mono type-label text-fg outline-none placeholder:text-fg-subtle"
            onChange={(e) => ctx.apply([{ op: "update", id: block.id, props: { expression: e.target.value } }], undefined, "merge", `eq:${block.id}`)}
            onFocus={() => ctx.onFocusBlock(block)}
            onBlur={() => setEditing(false)}
            onKeyDown={(e) => {
              if ((e.key === "Enter" && !e.shiftKey) || e.key === "Escape") {
                e.preventDefault();
                setEditing(false);
                if (e.key === "Enter") ctx.onEnter(block, false);
                else ctx.onTextKeyDown(block, e as unknown as React.KeyboardEvent<HTMLDivElement>);
              }
            }}
          />
          <Button size="sm" onMouseDown={(e) => e.preventDefault()} onClick={() => setEditing(false)}>
            Done
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function TocBlock({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  return (
    <div className="py-1">
      <TocBody blocks={ctx.snap.blocks} onJump={jumpToBlock} colorClassName={block.props.color ? "text-current" : undefined} />
    </div>
  );
}

function BreadcrumbBlock() {
  const ctx = useEditor();
  const detail = useDocsPage(ctx.pageId);
  const path = detail.data?.path ?? [];
  return (
    <nav aria-label="Breadcrumb" className="flex min-h-8 flex-wrap items-center gap-1 type-label text-fg-muted">
      {path.map((c, i) => (
        <span key={c.id} className="flex min-w-0 items-center gap-1">
          {i > 0 ? <ChevronRight aria-hidden className="size-3.5 shrink-0 text-fg-subtle" /> : null}
          <button type="button" className="flex min-w-0 items-center gap-1 rounded-sm px-1 py-0.5 hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring" onClick={() => ctx.openPage(c.id)}>
            {c.icon ? <span aria-hidden>{c.icon}</span> : null}
            <span className="truncate">{docsTitle(c.title)}</span>
          </button>
        </span>
      ))}
      {detail.data ? (
        <span className="flex min-w-0 items-center gap-1 text-fg">
          {path.length ? <ChevronRight aria-hidden className="size-3.5 shrink-0 text-fg-subtle" /> : null}
          {detail.data.page.icon ? <span aria-hidden>{detail.data.page.icon}</span> : null}
          <span aria-current="page" className="truncate px-1">
            {docsTitle(detail.data.page.title)}
          </span>
        </span>
      ) : null}
    </nav>
  );
}

// ---- pages, connected pages, buttons ---------------------------------------------------

function ConnectedPageInEditor({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  return <ConnectedPageBlock block={block} orgId={ctx.orgId} />;
}

function PageLinkBlock({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  const ref = ctx.refs.pages[block.props.page_id ?? ""];
  const noAccess = ref ? ref.title === null : false;
  const link = block.type === "link_to_page";
  return (
    <button
      type="button"
      disabled={noAccess}
      data-page-id={noAccess ? undefined : block.props.page_id}
      onClick={() => block.props.page_id && ctx.openPage(block.props.page_id)}
      className="flex min-h-control w-full items-center gap-2 rounded-sm px-1 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-default disabled:hover:bg-transparent"
    >
      <span aria-hidden className="relative flex size-6 items-center justify-center type-body">
        {noAccess ? <Lock className="size-4 text-fg-subtle" /> : ref?.icon ? ref.icon : <FileText className="size-4 text-fg-muted" />}
        {link && !noAccess ? <ArrowUpRight className="absolute -right-1 -bottom-1 size-3 rounded-xs bg-surface text-fg-muted" /> : null}
      </span>
      <span className={cn("min-w-0 flex-1 truncate type-body underline decoration-border-strong underline-offset-4", noAccess ? "text-fg-subtle no-underline" : "")}>
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

function toDraft(b: DocsBlockButtonBlock): DocsDraftBlock {
  return { type: b.type, props: b.props, children: b.children.map(toDraft) };
}

function ButtonBlock({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  const editable = ctx.editable && !ctx.isReadOnly(block.id);
  const actions = block.props.actions ?? [];
  const addRow = actions.find((a) => a.type === "add_row");
  const createRow = useDocsCreateRow(addRow?.type === "add_row" ? addRow.database_id : "");
  const [editAnchor, setEditAnchor] = useState<HTMLElement | null>(null);
  const run = async () => {
    if (!actions.length) {
      if (editable) setEditAnchor(document.querySelector<HTMLElement>(`[data-block-id="${block.id}"]`));
      return;
    }
    for (const a of actions) {
      if (a.type === "insert" && editable) ctx.insertAfter(block, a.blocks.map(toDraft));
      else if (a.type === "link") window.open(a.url, "_blank", "noopener,noreferrer");
      else if (a.type === "add_row") {
        try {
          await createRow.mutateAsync({ ...(a.title ? { title: a.title } : {}), values: a.values ?? {} });
          toast.success("Row added.");
        } catch (e) {
          toast.error((e as { message?: string }).message ?? "Couldn't add the row.");
        }
      }
    }
  };
  return (
    <div className="flex items-center gap-2 py-1">
      <Button size="sm" variant="secondary" onClick={() => void run()} disabled={createRow.isPending}>
        {block.props.label || "Button"}
      </Button>
      {editable ? (
        <Button size="sm" variant="ghost" onClick={(e) => setEditAnchor(e.currentTarget)}>
          Edit
        </Button>
      ) : null}
      {editAnchor ? (
        <ButtonEditor
          anchor={editAnchor}
          orgId={ctx.orgId}
          initial={block}
          onClose={() => setEditAnchor(null)}
          onSave={(label, next) => {
            setEditAnchor(null);
            ctx.apply([{ op: "update", id: block.id, props: { label, actions: next } }]);
          }}
        />
      ) : null}
    </div>
  );
}

// ---- tables ----------------------------------------------------------------------------

function TableBlock({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  // A connected page keeps some blocks read-only.
  const editable = ctx.editable && !ctx.isReadOnly(block.id);
  const rows = block.props.rows ?? [[[]]];
  const cols = Math.max(1, ...rows.map((r) => r.length));
  const cellRefs = useRef(new Map<string, { focus: (at?: "start" | "end") => void }>());
  const [cellMenu, setCellMenu] = useState<{ r: number; c: number; anchor: HTMLElement } | null>(null);
  const setRows = (next: DocsSpan[][][], key?: string) => ctx.apply([{ op: "update", id: block.id, props: { rows: next } }], undefined, key ? "merge" : "push", key);
  const grid = () => rows.map((row) => Array.from({ length: cols }, (_, c) => row[c] ?? []));
  const setCell = (r: number, c: number, spans: DocsSpan[]) => {
    const next = grid();
    next[r]![c] = spans;
    setRows(next, `cell:${block.id}:${r}:${c}`);
  };
  const empty = () => Array.from({ length: cols }, () => [] as DocsSpan[]);
  const addRow = (at = rows.length) => rows.length < 100 && setRows([...grid().slice(0, at), empty(), ...grid().slice(at)]);
  const addCol = (at = cols) => cols < 20 && setRows(grid().map((r) => [...r.slice(0, at), [], ...r.slice(at)]));
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
                      <td key={c} className={cn("group/cell relative min-w-24 border border-border px-2 align-top", header && "bg-sunken font-semibold")}>
                        <RichText
                          ref={(h) => {
                            if (h) cellRefs.current.set(`${r}:${c}`, { focus: (at) => h.focus(at ?? "end") });
                            else cellRefs.current.delete(`${r}:${c}`);
                          }}
                          spans={row[c] ?? []}
                          refs={ctx.refs}
                          editable={editable}
                          ariaLabel={`Row ${r + 1}, column ${c + 1}`}
                          className="min-h-8 py-1 pr-5"
                          onChange={(spans) => setCell(r, c, spans)}
                          onFocus={() => ctx.onFocusBlock(block)}
                          onOpenPage={ctx.openPage}
                          onKeyDown={(e) => {
                            if (e.key === "Tab") {
                              e.preventDefault();
                              const i = r * cols + c + (e.shiftKey ? -1 : 1);
                              if (i >= 0 && i < rows.length * cols) focusCell(Math.floor(i / cols), i % cols);
                              else if (i >= rows.length * cols && editable) {
                                addRow();
                                focusCell(rows.length, 0);
                              }
                            } else if (e.key === "Enter" && !e.shiftKey) {
                              e.preventDefault();
                              if (r + 1 >= rows.length) addRow();
                              focusCell(r + 1, c);
                            } else if (e.key === "Escape" || ((e.key === "z" || e.key === "y") && (e.metaKey || e.ctrlKey))) {
                              ctx.onTextKeyDown(block, e);
                            } else if ((e.metaKey || e.ctrlKey) && ["b", "i", "u"].includes(e.key.toLowerCase())) {
                              ctx.onTextKeyDown(block, e);
                            }
                          }}
                        />
                        {editable ? (
                          <button
                            type="button"
                            aria-label={`Row ${r + 1}, column ${c + 1} options`}
                            className="absolute top-1 right-0.5 hidden size-6 items-center justify-center rounded-xs text-fg-subtle hover:bg-surface hover:text-fg focus-visible:flex focus-visible:outline-2 focus-visible:outline-ring group-hover/cell:flex group-focus-within/cell:flex [&_svg]:size-3.5"
                            onMouseDown={(e) => e.preventDefault()}
                            onClick={(e) => setCellMenu({ r, c, anchor: e.currentTarget })}
                          >
                            <ChevronRight aria-hidden className="rotate-90" />
                          </button>
                        ) : null}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {editable && cols < 20 ? (
          <button type="button" aria-label="Add a column" title="Add a column" onClick={() => addCol()} className="flex h-8 w-6 shrink-0 items-center justify-center rounded-sm text-fg-subtle hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring">
            <Plus aria-hidden className="size-4" />
          </button>
        ) : null}
      </div>
      {editable && rows.length < 100 ? (
        <button type="button" onClick={() => addRow()} className="flex h-8 items-center justify-center gap-1 rounded-sm type-caption text-fg-subtle hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring">
          <Plus aria-hidden className="size-4" />
          Add a row
        </button>
      ) : null}
      {cellMenu ? (
        <Floating open anchor={cellMenu.anchor} onClose={() => setCellMenu(null)} autoFocus label="Cell options" role="menu">
          <CellMenu
            onDone={() => setCellMenu(null)}
            items={[
              { label: "Insert row above", run: () => addRow(cellMenu.r) },
              { label: "Insert row below", run: () => addRow(cellMenu.r + 1) },
              { label: "Insert column left", run: () => addCol(cellMenu.c) },
              { label: "Insert column right", run: () => addCol(cellMenu.c + 1) },
              { label: "Clear cell", run: () => setCell(cellMenu.r, cellMenu.c, []) },
              ...(rows.length > 1 ? [{ label: "Delete row", destructive: true, run: () => setRows(grid().filter((_, i) => i !== cellMenu.r)) }] : []),
              ...(cols > 1 ? [{ label: "Delete column", destructive: true, run: () => setRows(grid().map((row) => row.filter((_, i) => i !== cellMenu.c))) }] : []),
            ]}
          />
        </Floating>
      ) : null}
    </div>
  );
}

function CellMenu({ items, onDone }: { items: { label: string; run: () => void; destructive?: boolean }[]; onDone: () => void }) {
  return (
    <div className="flex min-w-48 flex-col">
      {items.map((it) => (
        <button
          key={it.label}
          type="button"
          role="menuitem"
          className={cn("flex min-h-control items-center rounded-sm px-3 text-left type-body hover:bg-sunken focus-visible:bg-sunken focus-visible:outline-none pointer-coarse:min-h-tap", it.destructive ? "text-danger-strong" : "text-fg")}
          onClick={() => {
            onDone();
            it.run();
          }}
        >
          {it.label}
        </button>
      ))}
    </div>
  );
}

/** Icons for media placeholders in menus. */
export const MEDIA_ICON = { image: <ImageIcon />, video: <Video />, audio: <Music />, pdf: <FileText />, file: <Paperclip /> } as const;

/** Markdown typed into a button's "insert" box → its template blocks. */
export function buttonBlocksFromMarkdown(md: string): DocsBlockButtonBlock[] {
  const conv = (d: DocsDraftBlock): DocsBlockButtonBlock => ({ type: d.type, props: d.props, children: d.children.map(conv) });
  return docsMarkdownToBlocks(md).map(conv);
}
