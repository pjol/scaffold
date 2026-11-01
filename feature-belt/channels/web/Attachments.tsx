"use client";

import { channelsFileSize, useChannelsAttachmentUrl, type ChannelsAttachment } from "@scaffoldapp/shared";
import { Spinner, cn } from "@scaffoldapp/ui-web";
import { Download, FileText } from "lucide-react";
import { useState } from "react";

/** The attachment's link, re-signed once if it fails (links last 1 hour). */
function useAttachmentSrc(a: ChannelsAttachment): { src: string; onError: () => void } {
  const [failed, setFailed] = useState(false);
  const fresh = useChannelsAttachmentUrl(a.id, failed && !a.id.startsWith("local-"));
  return { src: fresh.data?.url ?? a.url, onError: () => setFailed(true) };
}

function ImageTile({ a, single, pending, onOpen }: { a: ChannelsAttachment; single: boolean; pending: boolean; onOpen: () => void }) {
  const { src, onError } = useAttachmentSrc(a);
  const ratio = a.width && a.height ? a.width / a.height : undefined;
  return (
    <button
      type="button"
      onClick={onOpen}
      disabled={pending}
      aria-label={a.file_name ? `Open ${a.file_name}` : "Open photo"}
      className={cn("relative block overflow-hidden rounded-md border border-border bg-sunken focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring", single ? "max-h-80 w-full max-w-sm" : "aspect-square w-full")}
      style={single && ratio ? { aspectRatio: String(ratio) } : undefined}
    >
      {/* Signed, short-lived URLs: plain <img> (next/image would cache a link that expires). */}
      <img src={src} alt={a.file_name || "Photo"} onError={onError} loading="lazy" className={cn("size-full object-cover", pending && "opacity-60")} />
      {pending && (
        <span className="absolute inset-0 flex items-center justify-center">
          <Spinner size={24} label="Uploading" />
        </span>
      )}
    </button>
  );
}

function MediaTile({ a }: { a: ChannelsAttachment }) {
  const { src, onError } = useAttachmentSrc(a);
  if (a.kind === "video") return <video src={src} onError={onError} controls preload="metadata" className="max-h-80 w-full max-w-sm rounded-md bg-sunken" aria-label={a.file_name || "Video"} />;
  return <audio src={src} onError={onError} controls preload="metadata" className="w-full max-w-sm" aria-label={a.file_name || "Audio"} />;
}

function FileTile({ a, pending }: { a: ChannelsAttachment; pending: boolean }) {
  const { src } = useAttachmentSrc(a);
  const size = channelsFileSize(a.size_bytes);
  const body = (
    <>
      <FileText aria-hidden className="size-5 shrink-0 text-fg-muted" />
      <span className="flex min-w-0 flex-1 items-baseline gap-2">
        <span className="truncate type-label text-fg">{a.file_name || "File"}</span>
        {size && <span className="shrink-0 type-caption text-fg-muted">{size}</span>}
      </span>
      {pending ? <Spinner size={16} label="Uploading" /> : <Download aria-hidden className="size-4 shrink-0 text-fg-muted" />}
    </>
  );
  const cls = "flex min-h-tap-sm w-full max-w-sm items-center gap-3 rounded-md border border-border bg-surface px-3 py-2";
  if (pending || !src) return <span className={cls}>{body}</span>;
  return (
    <a href={src} target="_blank" rel="noopener noreferrer" download={a.file_name || undefined} className={cn(cls, "transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring")}>
      {body}
    </a>
  );
}

/** A message's files: photos in a grid, video and audio players, documents as download rows. */
export function Attachments({ attachments, pending, onOpenImage }: { attachments: ChannelsAttachment[]; pending: boolean; onOpenImage: (attachmentId: string) => void }) {
  const images = attachments.filter((a) => a.kind === "image");
  const media = attachments.filter((a) => a.kind === "video" || a.kind === "audio");
  const files = attachments.filter((a) => a.kind === "file");
  return (
    <div className="flex flex-col gap-2">
      {images.length > 0 && (
        <div className={cn("grid max-w-sm gap-1", images.length > 1 && "grid-cols-2")}>
          {images.map((a) => (
            <ImageTile key={a.id} a={a} single={images.length === 1} pending={pending} onOpen={() => onOpenImage(a.id)} />
          ))}
        </div>
      )}
      {media.map((a) => (
        <MediaTile key={a.id} a={a} />
      ))}
      {files.map((a) => (
        <FileTile key={a.id} a={a} pending={pending} />
      ))}
    </div>
  );
}
