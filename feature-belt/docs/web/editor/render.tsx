"use client";

/**
 * Block bodies shared by the editor and the read-only views (published pages, history): media through the core
 * FilePreview kit, embeds in sandboxed frames, bookmark cards, KaTeX equations, highlighted code and Mermaid
 * diagrams, tables of contents. Nothing here edits; the editor wraps these with its controls.
 */
import {
  docsEmbedHeight,
  docsEmbedName,
  docsEmbedSrcAllowed,
  docsPlain,
  useDocsFileUrl,
  useDocsLinkPreview,
  useEndpoints,
  docsEndpoints,
  type DocsBlock,
} from "@scaffoldapp/shared";
import type { FilePreviewToken, FilePreviewTokenRole } from "@scaffoldapp/shared/preview/highlight";
import { FilePreview, FilePreviewDialog, Skeleton, cn } from "@scaffoldapp/ui-web";
import { Globe, ImageOff, TriangleAlert } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { renderTeX } from "./katex";
import { renderMermaid } from "./mermaid";

// ---- files -------------------------------------------------------------------------

/** A block's file link: the platform's (connected pages) or a short-lived link to the upload. */
export function useBlockFile(block: DocsBlock, token: string | null) {
  const file = useDocsFileUrl(block.props.url ? null : block.props.file_id, token);
  return { url: block.props.url || file.data?.url || null, isError: !block.props.url && file.isError, isLoading: !block.props.url && file.isLoading };
}

/** Opens an attachment link for a file (signed-in pages); public pages download from the shown link. */
export function useDownload(fileId: string | undefined, token: string | null, fallback: string | null) {
  const d = useEndpoints(docsEndpoints);
  return () => {
    if (!fileId || token) {
      if (fallback) window.open(fallback, "_blank", "noopener,noreferrer");
      return;
    }
    void d.fileDownload(fileId).then(
      (f) => f.url && (window.location.href = f.url),
      () => fallback && window.open(fallback, "_blank", "noopener,noreferrer"),
    );
  };
}

function Unavailable({ what }: { what: string }) {
  return (
    <div className="flex h-24 items-center justify-center gap-2 rounded-md border border-dashed border-border type-label text-fg-muted">
      <ImageOff aria-hidden className="size-4" />
      This {what} isn&apos;t available.
    </div>
  );
}

/** Width (percent) and alignment for images, videos and embeds. */
export function MediaFrame({ width, align, children, className }: { width?: number | undefined; align?: string | undefined; children: ReactNode; className?: string }) {
  return (
    <div className={cn("flex w-full", align === "left" ? "justify-start" : align === "right" ? "justify-end" : "justify-center", className)}>
      <div className="relative max-w-full" style={{ width: `${width ?? 100}%` }}>
        {children}
      </div>
    </div>
  );
}

export function ImageBody({ block, token, onLoadRatio }: { block: DocsBlock; token: string | null; onLoadRatio?: (r: number) => void }) {
  const { url, isError } = useBlockFile(block, token);
  const [zoom, setZoom] = useState(false);
  const alt = block.props.alt || docsPlain(block.props.caption) || block.props.name || "Image";
  if (isError) return <Unavailable what="image" />;
  if (!url) return <Skeleton className="h-48 w-full rounded-md" />;
  return (
    <>
      <button type="button" className="block w-full cursor-zoom-in rounded-md focus-visible:outline-2 focus-visible:outline-ring" onClick={() => setZoom(true)} aria-label={`Open ${alt}`}>
        {/* Short-lived private links: next/image can't optimise them. */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={url}
          referrerPolicy="no-referrer"
          alt={alt}
          loading="lazy"
          draggable={false}
          onLoad={(e) => onLoadRatio?.(e.currentTarget.naturalWidth / Math.max(1, e.currentTarget.naturalHeight))}
          className={cn("mx-auto rounded-md border border-border", block.props.width && block.props.width < 100 ? "w-full" : "h-auto max-w-full")}
        />
      </button>
      <FilePreviewDialog open={zoom} onOpenChange={setZoom} url={url} name={block.props.name ?? alt} mime={block.props.content_type ?? null} size={block.props.size ?? null} />
    </>
  );
}

export function VideoBody({ block, token }: { block: DocsBlock; token: string | null }) {
  const { url, isError } = useBlockFile(block, token);
  const [failed, setFailed] = useState(false);
  if (isError) return <Unavailable what="video" />;
  if (!url) return <Skeleton className="aspect-video w-full rounded-md" />;
  // The browser's own player at the block's width; a file it can't play becomes a card with Download.
  if (failed) return <FilePreview compact url={url} name={block.props.name ?? "Video"} mime="application/octet-stream" size={block.props.size ?? null} />;
  return (
    <video src={url} controls preload="metadata" playsInline onError={() => setFailed(true)} className="w-full rounded-md bg-inverse" aria-label={block.props.name ?? "Video"}>
      {docsPlain(block.props.caption)}
    </video>
  );
}

export function AudioBody({ block, token }: { block: DocsBlock; token: string | null }) {
  const { url, isError } = useBlockFile(block, token);
  if (isError) return <Unavailable what="audio file" />;
  if (!url) return <Skeleton className="h-14 w-full rounded-md" />;
  return <FilePreview compact url={url} name={block.props.name ?? "Audio"} mime={block.props.content_type ?? "audio/mpeg"} size={block.props.size ?? null} />;
}

export function PdfBody({ block, token }: { block: DocsBlock; token: string | null }) {
  const { url, isError } = useBlockFile(block, token);
  const download = useDownload(block.props.file_id, token, url);
  if (isError) return <Unavailable what="PDF" />;
  if (!url) return <Skeleton className="h-96 w-full rounded-md" />;
  return (
    <div className="overflow-hidden rounded-md border border-border" style={{ height: block.props.height ?? 640 }}>
      <FilePreview url={url} name={block.props.name ?? "Document.pdf"} mime="application/pdf" size={block.props.size ?? null} onDownload={download} className="h-full" />
    </div>
  );
}

export function FileBody({ block, token }: { block: DocsBlock; token: string | null }) {
  const { url, isError } = useBlockFile(block, token);
  const download = useDownload(block.props.file_id, token, url);
  if (isError) return <Unavailable what="file" />;
  if (!url) return <Skeleton className="h-14 w-full rounded-md" />;
  return <FilePreview compact url={url} name={block.props.name ?? "File"} mime={block.props.content_type ?? null} size={block.props.size ?? null} onDownload={download} />;
}

// ---- embeds and bookmarks ------------------------------------------------------------

/** An allowlisted provider's player or viewer in a sandboxed frame (only the server's src, only on known hosts). */
export function EmbedBody({ block, height }: { block: DocsBlock; height?: number }) {
  const src = block.props.src;
  const name = docsEmbedName(block.props.provider);
  const [loaded, setLoaded] = useState(false);
  if (!docsEmbedSrcAllowed(src)) {
    return (
      <a href={block.props.url} target="_blank" rel="noopener noreferrer nofollow" className="flex min-h-12 items-center gap-3 rounded-md border border-border bg-surface px-3 py-2 type-body text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
        <Globe aria-hidden className="size-5 shrink-0 text-fg-muted" />
        <span className="min-w-0 flex-1 truncate">{block.props.url}</span>
      </a>
    );
  }
  return (
    <div className="relative w-full overflow-hidden rounded-md border border-border bg-sunken" style={{ height: height ?? block.props.height ?? docsEmbedHeight(block.props.provider) }}>
      {!loaded ? <Skeleton className="absolute inset-0 rounded-none" /> : null}
      <iframe
        src={src}
        title={`${name}: ${block.props.url ?? ""}`}
        className="relative size-full"
        loading="lazy"
        referrerPolicy="strict-origin"
        sandbox="allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox allow-forms allow-presentation"
        allow="fullscreen; picture-in-picture; encrypted-media; clipboard-write"
        allowFullScreen
        onLoad={() => setLoaded(true)}
      />
    </div>
  );
}

function host(url: string | undefined): string {
  try {
    return url ? new URL(url).host.replace(/^www\./, "") : "";
  } catch {
    return url ?? "";
  }
}

/** A link card: title, description, site, favicon and preview image (copies the server keeps; the site never sees readers). */
export function BookmarkBody({ block, token }: { block: DocsBlock; token: string | null }) {
  const preview = useDocsLinkPreview(block.props.preview_id, token);
  const p = preview.data;
  const title = block.props.title || p?.title || host(block.props.url);
  const desc = block.props.description || p?.description;
  const site = block.props.site_name || p?.site_name || host(block.props.url);
  const [iconOk, setIconOk] = useState(true);
  const [imageOk, setImageOk] = useState(true);
  return (
    <a
      href={block.props.url}
      target="_blank"
      rel="noopener noreferrer nofollow"
      className="flex min-h-16 w-full overflow-hidden rounded-md border border-border bg-surface hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
    >
      <span className="flex min-w-0 flex-1 flex-col gap-1 px-3 py-2">
        <span className="truncate type-body font-medium text-fg">{title}</span>
        {desc ? <span className="line-clamp-2 type-label text-fg-muted">{desc}</span> : null}
        <span className="flex min-w-0 items-center gap-2 type-caption text-fg-muted">
          {p?.icon_url && iconOk ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img src={p.icon_url} alt="" className="size-4 shrink-0 rounded-xs" referrerPolicy="no-referrer" onError={() => setIconOk(false)} />
          ) : (
            <Globe aria-hidden className="size-4 shrink-0" />
          )}
          <span className="truncate">{site}</span>
        </span>
      </span>
      {p?.image_url && imageOk ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={p.image_url} alt="" className="hidden w-40 shrink-0 object-cover sm:block" referrerPolicy="no-referrer" onError={() => setImageOk(false)} />
      ) : null}
    </a>
  );
}

// ---- equations ---------------------------------------------------------------------

/** A block equation rendered with KaTeX (lazy); a parse error shows the source and what's wrong. */
export function EquationBody({ expression, placeholder }: { expression: string; placeholder?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const [error, setError] = useState<string | null>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || !expression) return undefined;
    el.textContent = expression;
    renderTeX(el, expression, true);
    const obs = new MutationObserver(() => setError(el.dataset["error"] ?? null));
    obs.observe(el, { attributes: true, attributeFilter: ["data-error"] });
    return () => obs.disconnect();
  }, [expression]);
  if (!expression) return <p className="py-2 type-body text-fg-subtle">{placeholder ?? "Empty equation"}</p>;
  return (
    <div className="flex flex-col gap-1">
      <div ref={ref} role="img" aria-label={`Equation: ${expression}`} className="overflow-x-auto py-2 text-center type-body-lg text-fg" />
      {error ? (
        <p className="flex items-center gap-1 type-caption text-danger-strong">
          <TriangleAlert aria-hidden className="size-3.5" />
          {error}
        </p>
      ) : null}
    </div>
  );
}

// ---- code ------------------------------------------------------------------------------

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

const LANG_ALIAS: Record<string, string> = { html: "xml", shell: "bash", sh: "bash", js: "javascript", ts: "typescript", py: "python", objectivec: "c", toml: "ini" };

/** Lines of highlighted tokens (highlight.js through the shared preview kit, loaded on first use). */
export function useHighlighted(code: string, language: string): FilePreviewToken[][] | null {
  const [lines, setLines] = useState<FilePreviewToken[][] | null>(null);
  useEffect(() => {
    if (!language || language === "plain" || language === "mermaid" || code.length > 200_000) {
      setLines(null);
      return undefined;
    }
    let live = true;
    const t = setTimeout(() => {
      void import("@scaffoldapp/shared/preview/highlight").then((m) => live && setLines(m.filePreviewHighlight(code, LANG_ALIAS[language] ?? language)));
    }, 60);
    return () => {
      live = false;
      clearTimeout(t);
    };
  }, [code, language]);
  return lines;
}

/** Highlighted code (one line per line, so a textarea can sit exactly on top). */
export function CodeTokens({ code, lines }: { code: string; lines: FilePreviewToken[][] | null }) {
  if (!lines) return <>{code.endsWith("\n") ? `${code} ` : code || " "}</>;
  return (
    <>
      {lines.map((l, i) => (
        <span key={i}>
          {l.map((t, j) => (
            <span key={j} className={TOKEN_CLASS[t.role]}>
              {t.text}
            </span>
          ))}
          {i < lines.length - 1 || code.endsWith("\n") ? "\n" : ""}
        </span>
      ))}
    </>
  );
}

/** A Mermaid diagram drawn as an image (lazy). */
export function MermaidDiagram({ code }: { code: string }) {
  const [out, setOut] = useState<{ url: string; width?: number | undefined } | { error: string } | null>(null);
  useEffect(() => {
    if (!code.trim()) {
      setOut(null);
      return undefined;
    }
    let live = true;
    const t = setTimeout(() => void renderMermaid(code).then((r) => live && setOut(r)), 300);
    return () => {
      live = false;
      clearTimeout(t);
    };
  }, [code]);
  if (!code.trim()) return <p className="px-3 py-4 type-label text-fg-subtle">Write a diagram below.</p>;
  if (!out) return <Skeleton className="h-40 w-full" />;
  if ("error" in out)
    return (
      <p className="flex items-center gap-1 px-3 py-3 type-caption text-danger-strong">
        <TriangleAlert aria-hidden className="size-3.5" />
        {out.error}
      </p>
    );
  // eslint-disable-next-line @next/next/no-img-element
  return <img src={out.url} alt="Diagram" style={out.width ? { width: out.width } : undefined} className="mx-auto h-auto max-h-[70dvh] max-w-full py-3" />;
}

/** Read-only code: highlighted, optional wrap, diagram for Mermaid. */
export function CodeView({ block }: { block: DocsBlock }) {
  const code = block.props.code ?? "";
  const language = block.props.language ?? "plain";
  const lines = useHighlighted(code, language);
  return (
    <div className="overflow-hidden rounded-md bg-sunken">
      {language === "mermaid" ? <MermaidDiagram code={code} /> : null}
      {language !== "mermaid" || !code.trim() ? (
        <pre className={cn("overflow-x-auto px-3 py-3 font-mono type-label text-fg", block.props.wrap ? "whitespace-pre-wrap break-words" : "whitespace-pre")}>
          <code>
            <CodeTokens code={code} lines={lines} />
          </code>
        </pre>
      ) : null}
    </div>
  );
}

// ---- table of contents ------------------------------------------------------------------

const LEVEL: Record<string, number> = { heading_1: 0, heading_2: 1, heading_3: 2, heading_4: 3 };
const INDENT = ["pl-0", "pl-4", "pl-8", "pl-12"];

/** The page's headings, linked; `onJump` scrolls to one. */
export function TocBody({ blocks, onJump, colorClassName }: { blocks: readonly DocsBlock[]; onJump: (id: string) => void; colorClassName?: string }) {
  const heads = useMemo(() => blocks.filter((b) => b.type in LEVEL && docsPlain(b.props.text).trim()), [blocks]);
  if (!heads.length) return <p className="py-1 type-label text-fg-subtle">Add headings to fill the table of contents.</p>;
  return (
    <nav aria-label="Table of contents" className="flex flex-col">
      {heads.map((h) => (
        <a
          key={h.id}
          href={`#block-${h.id}`}
          onClick={(e) => {
            e.preventDefault();
            onJump(h.id);
          }}
          className={cn("truncate rounded-sm py-1 type-body underline decoration-border-strong underline-offset-4 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring", INDENT[LEVEL[h.type] ?? 0], colorClassName || "text-fg-muted")}
        >
          {docsPlain(h.props.text)}
        </a>
      ))}
    </nav>
  );
}

/** Scrolls to a block and highlights it briefly (#block-<id>). */
export function jumpToBlock(id: string): void {
  const el = document.querySelector<HTMLElement>(`[data-block-id="${CSS.escape(id)}"]`);
  if (!el) return;
  const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  el.scrollIntoView({ block: "center", behavior: reduce ? "auto" : "smooth" });
  const flash = ["ring-2", "ring-primary", "rounded-sm"];
  el.classList.add(...flash);
  window.setTimeout(() => el.classList.remove(...flash), 1600);
  history.replaceState(null, "", `#block-${id}`);
}
