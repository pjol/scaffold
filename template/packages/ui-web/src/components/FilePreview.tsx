"use client";

import * as DialogPrimitive from "@radix-ui/react-dialog";
import {
  FILE_PREVIEW_TEXT_MAX,
  fileFormatSize,
  fileIsZip,
  fileOfficeFormat,
  filePreviewFormatJson,
  filePreviewHasView,
  filePreviewKind,
  filePreviewKindLabel,
  filePreviewLanguage,
  type FilePreviewKind,
} from "@scaffoldapp/shared/preview";
import { Download, ExternalLink, X } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { lazy, Suspense, useMemo, useState, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useMediaQuery } from "../lib/hooks";
import { useMotionTokens } from "../motion/tokens";
import { FileIcon } from "../preview/icons";
import { ImageView } from "../preview/ImageView";
import { fetchText, useLoaded } from "../preview/load";
import { CodeView, CsvView, MarkdownView } from "../preview/TextViews";
import { Banner } from "./Banner";
import { Button } from "./Button";
import { ErrorState } from "./EmptyState";
import { IconButton } from "./IconButton";
import { SegmentedControl } from "./SegmentedControl";
import { Skeleton } from "./Skeleton";

export { FileIcon, filePreviewIcon, filePreviewTone, type FileIconProps } from "../preview/icons";

const OfficeView = lazy(() => import("../preview/OfficeView"));
const ArchiveView = lazy(() => import("../preview/ArchiveView"));
const FontView = lazy(() => import("../preview/FontView"));

export interface FilePreviewProps {
  /** A link the browser may fetch (presigned, public or a same-origin stream). Text kinds are fetched from it, so it must allow CORS GET from the app. */
  url: string;
  name: string;
  /** The stored content type; missing or generic types fall back to the extension. */
  mime?: string | null;
  /** Bytes, shown on cards and used to skip previews too large to read. */
  size?: number | null;
  /** An attachment link for Download. */
  downloadUrl?: string | null;
  /** Download handled by the caller (instead of `downloadUrl`). */
  onDownload?: () => void;
  /** Attachment size: a bounded image or player, or a file card that opens the full preview in a dialog. */
  compact?: boolean;
  /** A small image for compact images and video posters (a server thumbnail). */
  thumbnailUrl?: string | null;
  /** Classes for the view's box (heights). */
  className?: string;
}

/**
 * Shows a file from a URL: images (zoom and pan), video, audio, PDF, text, code (highlighted),
 * Markdown (drawn from a parsed tree, never HTML), CSV/TSV (virtualised table), JSON
 * (formatted), SVG (as an image only, so its scripts never run), Word/Excel/PowerPoint and
 * OpenDocument files, zip listings and fonts; anything else as an icon, size and Download.
 * Highlighter, office, zip and font readers load only when needed; nothing comes from a CDN.
 */
export function FilePreview(props: FilePreviewProps) {
  return props.compact ? <CompactPreview {...props} /> : <FullPreview {...props} />;
}

function DownloadButton({ name, downloadUrl, onDownload, buttonSize = "md", variant = "primary" }: Pick<FilePreviewProps, "name" | "downloadUrl" | "onDownload"> & { buttonSize?: "sm" | "md"; variant?: "primary" | "secondary" | "ghost" }) {
  if (downloadUrl) {
    return (
      <Button asChild size={buttonSize} variant={variant} leftIcon={<Download />}>
        <a href={downloadUrl} download={name} rel="noopener">
          Download
        </a>
      </Button>
    );
  }
  if (onDownload) {
    return (
      <Button size={buttonSize} variant={variant} leftIcon={<Download />} onClick={onDownload}>
        Download
      </Button>
    );
  }
  return null;
}

/** Icon, what the file is, its size and Download, for files without a preview (or one that failed). */
function NoPreview({ kind, reason, ...p }: FilePreviewProps & { kind: FilePreviewKind; reason?: string }) {
  const label = filePreviewKindLabel(kind, p.name, p.mime);
  const title =
    reason === "too_large"
      ? "This file is too large to preview"
      : reason === "unplayable"
        ? "This browser can't play this file"
        : reason === "unreadable"
        ? "Couldn't read this file"
        : label === "File"
          ? "No preview for this file"
          : `No preview for ${label === "PDF" || label === "JSON" ? label : label.toLowerCase()} files`;
  return (
    <div className={cn("flex flex-col items-center gap-4 rounded-lg border border-border bg-surface px-card py-12 text-center")}>
      <FileIcon name={p.name} mime={p.mime} kind={kind} className="size-12" />
      <div className="flex flex-col gap-1">
        <p className="type-title-sm text-fg">{title}</p>
        <p className="type-caption text-fg-muted">
          {label}
          {p.size != null ? ` · ${fileFormatSize(p.size)}` : ""}
        </p>
      </div>
      <DownloadButton {...p} />
    </div>
  );
}

function Lazy({ children }: { children: ReactNode }) {
  return <Suspense fallback={<Skeleton className="h-96 w-full rounded-lg" />}>{children}</Suspense>;
}

function FullPreview(p: FilePreviewProps) {
  const kind = filePreviewKind(p.name, p.mime);
  const [failed, setFailed] = useState(false);
  const box = p.className ?? "h-[70dvh] min-h-72";
  const fallback = (reason?: string) => <NoPreview {...p} kind={kind} reason={reason} />;
  if (failed) return fallback(kind === "video" || kind === "audio" ? "unplayable" : "unreadable");
  switch (kind) {
    case "image":
      return <ImageView src={p.url} alt={p.name} className={box} onError={() => setFailed(true)} />;
    case "video":
      return (
        <video src={p.url} poster={p.thumbnailUrl ?? undefined} controls preload="metadata" onError={() => setFailed(true)} className={cn("w-full rounded-lg bg-inverse", p.className ?? "max-h-[70dvh]")}>
          <track kind="captions" />
        </video>
      );
    case "audio":
      return (
        <div className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-card">
          <div className="flex min-w-0 items-center gap-3">
            <FileIcon name={p.name} mime={p.mime} kind={kind} className="size-8" />
            <p className="min-w-0 truncate type-body font-semibold text-fg">{p.name}</p>
          </div>
          <audio src={p.url} controls preload="metadata" onError={() => setFailed(true)} className="w-full" />
        </div>
      );
    case "pdf":
      return <PdfView {...p} box={box} />;
    case "office": {
      const format = fileOfficeFormat(p.name, p.mime);
      if (!format) return fallback();
      return (
        <Lazy>
          <OfficeView url={p.url} name={p.name} format={format} size={p.size ?? undefined} className={p.className ?? "max-h-[70dvh]"} fallback={fallback} />
        </Lazy>
      );
    }
    case "archive":
      if (!fileIsZip(p.name, p.mime)) return fallback();
      return (
        <Lazy>
          <ArchiveView url={p.url} name={p.name} size={p.size ?? undefined} className={p.className ?? "max-h-[60dvh]"} fallback={fallback} />
        </Lazy>
      );
    case "font":
      return (
        <Lazy>
          <FontView url={p.url} className={p.className ?? "max-h-[70dvh]"} fallback={fallback} />
        </Lazy>
      );
    case "other":
      return fallback();
  }
  return <TextPreview {...p} kind={kind} />;
}

/** PDF in the browser's own viewer on wide screens; phones (whose browsers don't draw PDFs in a frame) get Open. */
function PdfView(p: FilePreviewProps & { box: string }) {
  const wide = useMediaQuery("(min-width: 640px)", true);
  if (!wide) {
    return (
      <div className="flex flex-col items-center gap-4 rounded-lg border border-border bg-surface px-card py-12 text-center">
        <FileIcon name={p.name} mime={p.mime} kind="pdf" className="size-12" />
        <p className="type-caption text-fg-muted">PDF{p.size != null ? ` · ${fileFormatSize(p.size)}` : ""}</p>
        <div className="flex flex-wrap justify-center gap-2">
          <Button asChild leftIcon={<ExternalLink />}>
            <a href={p.url} target="_blank" rel="noopener noreferrer">
              Open
            </a>
          </Button>
          <DownloadButton {...p} variant="secondary" />
        </div>
      </div>
    );
  }
  return <iframe src={p.url} title={p.name} className={cn("w-full rounded-lg border border-border bg-surface", p.box)} />;
}

type Mode = "rendered" | "source";

const RENDERED_LABEL: Partial<Record<FilePreviewKind, string>> = { markdown: "Preview", csv: "Table", json: "Formatted", svg: "Image" };

function TextPreview(p: FilePreviewProps & { kind: FilePreviewKind }) {
  const { kind } = p;
  const tooBig = kind === "svg" && (p.size ?? 0) > FILE_PREVIEW_TEXT_MAX;
  const state = useLoaded(tooBig ? null : p.url, (signal) => fetchText(p.url, FILE_PREVIEW_TEXT_MAX, signal));
  const [mode, setMode] = useState<Mode>("rendered");
  const rich = kind === "markdown" || kind === "csv" || kind === "json" || kind === "svg";
  const json = useMemo(() => (kind === "json" && state.status === "ready" && !state.data.truncated ? filePreviewFormatJson(state.data.text) : null), [kind, state]);
  const box = p.className ?? "max-h-[70dvh]";

  if (tooBig) return <NoPreview {...p} reason="too_large" />;
  if (state.status === "loading") return <Skeleton className="h-96 w-full rounded-lg" />;
  if (state.status === "error") return <ErrorState compact title="Couldn't load the file" info={state.error} onRetry={state.retry} />;
  const { text, truncated } = state.data;
  const source = !rich || mode === "source" || (kind === "json" && (!json || !json.ok)) || (kind === "svg" && truncated);
  const language = kind === "text" ? "plaintext" : kind === "json" ? "json" : kind === "svg" ? "xml" : filePreviewLanguage(p.name, p.mime);

  let body: ReactNode;
  if (source) body = <CodeView text={text} language={language} wrap={kind === "text"} className={box} />;
  else if (kind === "markdown") body = <MarkdownView text={text} className={box} />;
  else if (kind === "csv") body = <CsvView text={text} name={p.name} className={p.className ?? "max-h-[60dvh]"} />;
  else if (kind === "json" && json?.ok) body = <CodeView text={json.text} language="json" className={box} />;
  else body = <ImageView src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(text)}`} alt={p.name} className={p.className ?? "h-[70dvh] min-h-72"} />;

  return (
    <div className="flex flex-col gap-3">
      {truncated ? (
        <Banner tone="info" action={<DownloadButton {...p} buttonSize="sm" variant="ghost" />}>
          Showing the first {fileFormatSize(FILE_PREVIEW_TEXT_MAX)}.
        </Banner>
      ) : null}
      {kind === "json" && json && !json.ok ? <Banner tone="warning">This isn&apos;t valid JSON: {json.error}</Banner> : null}
      {rich && !(kind === "json" && (!json || !json.ok)) && !(kind === "svg" && truncated) ? (
        <SegmentedControl
          aria-label="Show as"
          fullWidth={false}
          className="self-start"
          value={mode}
          onValueChange={setMode}
          options={[
            { value: "rendered", label: RENDERED_LABEL[kind] ?? "Preview" },
            { value: "source", label: "Source" },
          ]}
        />
      ) : null}
      {body}
    </div>
  );
}

function CompactPreview(p: FilePreviewProps) {
  const kind = filePreviewKind(p.name, p.mime);
  const [open, setOpen] = useState(false);
  const [failed, setFailed] = useState(false);
  const dialog = <FilePreviewDialog {...p} open={open} onOpenChange={setOpen} />;
  if (kind === "image" && !failed) {
    return (
      <>
        <button type="button" onClick={() => setOpen(true)} className={cn("block max-w-full overflow-hidden rounded-lg bg-sunken outline-none focus-visible:ring-2 focus-visible:ring-ring", p.className)} aria-label={`Open ${p.name}`}>
          {/* eslint-disable-next-line @next/next/no-img-element -- short-lived links from storage */}
          <img src={p.thumbnailUrl ?? p.url} alt="" loading="lazy" onError={() => setFailed(true)} className="max-h-60 max-w-full object-contain" />
        </button>
        {dialog}
      </>
    );
  }
  if (kind === "video" && !failed) {
    return (
      <video src={p.url} poster={p.thumbnailUrl ?? undefined} controls preload="metadata" onError={() => setFailed(true)} className={cn("max-h-60 max-w-full rounded-lg bg-inverse", p.className)}>
        <track kind="captions" />
      </video>
    );
  }
  if (kind === "audio" && !failed) {
    return (
      <div className={cn("flex w-full max-w-sm flex-col gap-2 rounded-lg border border-border bg-surface p-3", p.className)}>
        <div className="flex min-w-0 items-center gap-2">
          <FileIcon name={p.name} mime={p.mime} kind={kind} />
          <span className="min-w-0 flex-1 truncate type-label font-semibold text-fg">{p.name}</span>
          {p.size != null ? <span className="type-caption text-fg-muted">{fileFormatSize(p.size)}</span> : null}
        </div>
        <audio src={p.url} controls preload="metadata" onError={() => setFailed(true)} className="h-10 w-full" />
      </div>
    );
  }
  const viewable = filePreviewHasView(kind, p.name, p.mime);
  const caption = `${filePreviewKindLabel(kind, p.name, p.mime)}${p.size != null ? ` · ${fileFormatSize(p.size)}` : ""}`;
  const download = p.downloadUrl ? (
    <a
      href={p.downloadUrl}
      download={p.name}
      rel="noopener"
      aria-label={`Download ${p.name}`}
      title="Download"
      className="inline-flex size-control-sm shrink-0 items-center justify-center rounded-pill text-fg outline-none hover:bg-sunken focus-visible:ring-2 focus-visible:ring-ring [&_svg]:size-4"
    >
      <Download aria-hidden />
    </a>
  ) : p.onDownload ? (
    <IconButton size="sm" variant="ghost" label={`Download ${p.name}`} icon={<Download />} onClick={p.onDownload} />
  ) : null;
  return (
    <div className={cn("flex w-full max-w-sm min-w-0 items-center gap-1 rounded-lg border border-border bg-surface p-1 pr-2", p.className)}>
      <button
        type="button"
        disabled={!viewable}
        onClick={() => setOpen(true)}
        className="flex min-w-0 flex-1 items-center gap-3 rounded-md p-2 text-left outline-none enabled:hover:bg-sunken focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-default"
        aria-label={viewable ? `Open ${p.name}` : undefined}
      >
        <FileIcon name={p.name} mime={p.mime} kind={kind} className="size-8" />
        <span className="flex min-w-0 flex-col">
          <span className="truncate type-label font-semibold text-fg">{p.name}</span>
          <span className="truncate type-caption text-fg-muted">{caption}</span>
        </span>
      </button>
      {download}
      {viewable ? dialog : null}
    </div>
  );
}

export interface FilePreviewDialogProps extends Omit<FilePreviewProps, "compact"> {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** The full preview in a near full-screen dialog, with the name, size, Download and Close. */
export function FilePreviewDialog({ open, onOpenChange, ...p }: FilePreviewDialogProps) {
  const t = useMotionTokens();
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <AnimatePresence>
        {open ? (
          <DialogPrimitive.Portal forceMount>
            <DialogPrimitive.Overlay asChild forceMount>
              <motion.div className="fixed inset-0 z-(--app-z-overlay) bg-overlay" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: t.duration.base, ease: t.ease.out }} />
            </DialogPrimitive.Overlay>
            <DialogPrimitive.Content asChild forceMount aria-describedby={undefined}>
              <motion.div
                className="fixed inset-2 z-(--app-z-modal) flex flex-col overflow-hidden rounded-xl border border-border bg-surface-raised shadow-xl outline-none sm:inset-8"
                initial={{ opacity: 0, scale: 0.98 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.98, transition: { duration: t.duration.fast } }}
                transition={{ ...t.spring.gentle, opacity: { duration: t.duration.fast } }}
              >
                <div className="flex items-center gap-3 border-b border-border px-4 py-3">
                  <FileIcon name={p.name} mime={p.mime} />
                  <DialogPrimitive.Title className="min-w-0 flex-1 truncate type-title-sm text-fg">{p.name}</DialogPrimitive.Title>
                  {p.size != null ? <span className="hidden type-caption text-fg-muted sm:inline">{fileFormatSize(p.size)}</span> : null}
                  <DownloadButton {...p} buttonSize="sm" variant="secondary" />
                  <DialogPrimitive.Close asChild>
                    <IconButton label="Close" icon={<X />} size="sm" variant="ghost" />
                  </DialogPrimitive.Close>
                </div>
                <div className="min-h-0 flex-1 overflow-auto p-4">
                  <FullPreview {...p} />
                </div>
              </motion.div>
            </DialogPrimitive.Content>
          </DialogPrimitive.Portal>
        ) : null}
      </AnimatePresence>
    </DialogPrimitive.Root>
  );
}

export { filePreviewKind, type FilePreviewKind };
