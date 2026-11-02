"use client";

import {
  errorMessage,
  filesFormatSize,
  filesIsTextPreview,
  filesKindLabel,
  filesParseCsv,
  useFilesConnectedPreviewUrl,
  useFilesConnectedText,
  useFilesPreviewUrl,
  useFilesPublicPreviewUrl,
  useFilesPublicText,
  useFilesText,
  type FilesItem,
  type FilesPublicItem,
} from "@scaffoldapp/shared";
import { Banner, Button, EmptyState, ErrorState, SegmentedControl, Skeleton, cn } from "@scaffoldapp/ui-web";
import { Download, ExternalLink } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";
import { filesIconTone, filesItemIcon } from "./icons";
import { FilesMarkdown } from "./Markdown";

/** Where the preview reads from: the signed-in API (optionally an old version), a public link, or a connected platform (streamed through the API with your connection). */
export type FilesPreviewSource =
  | { kind: "app"; version?: string | null }
  | { kind: "public"; token: string }
  | { kind: "connected"; connectionId: string; ref: string; orgId?: string | null };

type PreviewItem = Pick<FilesItem | FilesPublicItem, "id" | "name" | "preview" | "size" | "mime_type" | "type" | "type_info">;

const MAX_LINES = 5000;

/**
 * Shows a file: images, PDF, video and audio from a short-lived inline link; text, code,
 * Markdown, CSV and SVG from the text endpoint (SVG drawn as an image, so its scripts never
 * run); anything else as a download card.
 */
export function FilesPreview({ item, source, onDownload, link }: { item: PreviewItem; source: FilesPreviewSource; onDownload: () => void; link?: string }) {
  const media = item.preview === "image" || item.preview === "pdf" || item.preview === "video" || item.preview === "audio";
  const text = filesIsTextPreview(item.preview);
  const app = source.kind === "app";
  const conn = source.kind === "connected" ? source : null;
  const version = source.kind === "app" ? (source.version ?? null) : null;
  const token = source.kind === "public" ? source.token : null;
  const appUrl = useFilesPreviewUrl(app ? item.id : null, version, media);
  const pubUrl = useFilesPublicPreviewUrl(token, token ? item.id : null, media);
  const connUrl = useFilesConnectedPreviewUrl(conn?.connectionId, conn?.ref, conn?.orgId, media && !!conn);
  const appText = useFilesText(app && text ? item.id : null, version);
  const pubText = useFilesPublicText(token, token && text ? item.id : null, text);
  const connText = useFilesConnectedText(conn?.connectionId, conn?.ref, conn?.orgId, text && !!conn);
  const url = app ? appUrl : conn ? connUrl : pubUrl;
  const content = app ? appText : conn ? connText : pubText;

  if (item.type !== "file") {
    return (
      <Fallback item={item}>
        {link ? (
          <Button asChild leftIcon={<ExternalLink />}>
            <Link href={link}>Open {item.type_info?.title.toLowerCase() ?? "it"}</Link>
          </Button>
        ) : null}
      </Fallback>
    );
  }

  if (media) {
    if (url.isPending) return <Skeleton className="h-96 w-full rounded-lg" />;
    if (url.isError) return <ErrorState title="Couldn't load the preview" info={errorMessage(url.error, "")} onRetry={() => void url.refetch()} />;
    const src = url.data.url;
    switch (item.preview) {
      case "image":
        return (
          <div className="flex justify-center rounded-lg bg-sunken p-2">
            {/* eslint-disable-next-line @next/next/no-img-element -- presigned, short-lived URL */}
            <img src={src} alt={item.name} className="max-h-[75dvh] max-w-full rounded-md object-contain" />
          </div>
        );
      case "pdf":
        return <iframe src={src} title={item.name} className="h-[75dvh] w-full rounded-lg border border-border bg-surface" />;
      case "video":
        return (
          <video src={src} controls preload="metadata" className="max-h-[75dvh] w-full rounded-lg bg-inverse">
            <track kind="captions" />
          </video>
        );
      case "audio":
        return (
          <div className="rounded-lg border border-border bg-surface p-card">
            <audio src={src} controls preload="metadata" className="w-full" />
          </div>
        );
    }
  }

  if (text) {
    if (content.isPending) return <Skeleton className="h-96 w-full rounded-lg" />;
    if (content.isError) {
      return <ErrorState title="Couldn't load the file" info={errorMessage(content.error, "")} onRetry={() => void content.refetch()} />;
    }
    return <TextPreview item={item} text={content.data.text} truncated={content.data.truncated} onDownload={onDownload} />;
  }

  return (
    <Fallback item={item}>
      <Button leftIcon={<Download />} onClick={onDownload}>
        Download ({filesFormatSize(item.size)})
      </Button>
    </Fallback>
  );
}

function Fallback({ item, children }: { item: PreviewItem; children?: React.ReactNode }) {
  const Icon = filesItemIcon(item);
  return (
    <div className="flex flex-col items-center gap-4 rounded-lg border border-border bg-surface px-card py-12 text-center">
      <Icon className={cn("size-12", filesIconTone(item))} aria-hidden />
      <p className="type-title-sm text-fg">{item.type !== "file" ? item.name : filesKindLabel(item) === "File" ? "No preview for this file" : `No preview for ${filesKindLabel(item).toLowerCase()} files`}</p>
      {children}
    </div>
  );
}

function TextPreview({ item, text, truncated, onDownload }: { item: PreviewItem; text: string; truncated: boolean; onDownload: () => void }) {
  const rich = item.preview === "markdown" || item.preview === "csv" || item.preview === "svg";
  const [mode, setMode] = useState<"rendered" | "source">("rendered");
  const showSource = !rich || mode === "source";
  return (
    <div className="flex flex-col gap-3">
      {truncated ? (
        <Banner
          tone="info"
          action={
            <Button variant="ghost" size="sm" onClick={onDownload}>
              Download
            </Button>
          }
        >
          Showing the first 1 MB.
        </Banner>
      ) : null}
      {rich ? (
        <SegmentedControl
          aria-label="Show as"
          fullWidth={false}
          className="self-start"
          value={mode}
          onValueChange={setMode}
          options={[
            { value: "rendered", label: item.preview === "csv" ? "Table" : item.preview === "svg" ? "Image" : "Preview" },
            { value: "source", label: "Source" },
          ]}
        />
      ) : null}
      {showSource ? (
        <CodeView text={text} onDownload={onDownload} />
      ) : item.preview === "markdown" ? (
        <div className="rounded-lg border border-border bg-surface p-card">
          <FilesMarkdown text={text} />
        </div>
      ) : item.preview === "csv" ? (
        <CsvTable text={text} name={item.name} />
      ) : (
        <div className="flex justify-center rounded-lg bg-sunken p-4">
          {/* An <img> never runs an SVG's scripts. */}
          {/* eslint-disable-next-line @next/next/no-img-element -- local data URL */}
          <img src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(text)}`} alt={item.name} className="max-h-[75dvh] max-w-full object-contain" />
        </div>
      )}
    </div>
  );
}

function CodeView({ text, onDownload }: { text: string; onDownload: () => void }) {
  const lines = useMemo(() => text.replace(/\r\n/g, "\n").replace(/\n$/, "").split("\n"), [text]);
  const shown = lines.length > MAX_LINES ? lines.slice(0, MAX_LINES) : lines;
  const gutter = String(shown.length).length;
  if (text === "") return <EmptyState compact title="This file is empty" />;
  return (
    <div className="flex flex-col gap-2">
      <div className="max-h-[75dvh] overflow-auto rounded-lg border border-border bg-surface">
        <pre className="min-w-max py-3 font-mono type-label text-fg">
          {shown.map((l, i) => (
            <div key={i} className="flex">
              <span aria-hidden className="shrink-0 pr-4 pl-4 text-right text-fg-subtle tabular-nums select-none" style={{ width: `${gutter + 3}ch` }}>
                {i + 1}
              </span>
              <code className="pr-4 whitespace-pre">{l || " "}</code>
            </div>
          ))}
        </pre>
      </div>
      {lines.length > MAX_LINES ? (
        <p className="type-caption text-fg-muted">
          Showing {MAX_LINES.toLocaleString()} of {lines.length.toLocaleString()} lines.{" "}
          <button type="button" className="underline underline-offset-2" onClick={onDownload}>
            Download the file
          </button>{" "}
          for the rest.
        </p>
      ) : null}
    </div>
  );
}

function CsvTable({ text, name }: { text: string; name: string }) {
  const { rows, truncated } = useMemo(() => filesParseCsv(text, 501, /\.tsv$/i.test(name) ? "\t" : ","), [text, name]);
  const [header, ...body] = rows;
  if (!header) return <EmptyState compact title="This file is empty" />;
  return (
    <div className="flex flex-col gap-2">
      <div className="max-h-[75dvh] overflow-auto rounded-lg border border-border bg-surface">
        <table className="w-full border-collapse type-label">
          <thead className="sticky top-0 bg-sunken">
            <tr>
              {header.map((c, i) => (
                <th key={i} scope="col" className="px-3 py-2 text-left font-semibold whitespace-nowrap text-fg">
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {body.map((r, ri) => (
              <tr key={ri} className="border-t border-border">
                {header.map((_, ci) => (
                  <td key={ci} className="px-3 py-2 whitespace-nowrap text-fg">
                    {r[ci] ?? ""}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {truncated ? <p className="type-caption text-fg-muted">Showing the first 500 rows.</p> : null}
    </div>
  );
}
