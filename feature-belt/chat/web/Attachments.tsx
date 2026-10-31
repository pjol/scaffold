"use client";

import { formatFileSize, useChatAttachmentUrl, type ChatAttachment } from "@scaffoldapp/shared";
import { Spinner, cn } from "@scaffoldapp/ui-web";
import { Download, FileText } from "lucide-react";
import { useState } from "react";

/** The attachment's link, re-signed once if the first one fails (links last 1 hour). */
function useAttachmentSrc(a: ChatAttachment): { src: string; onError: () => void } {
  const [failed, setFailed] = useState(false);
  const fresh = useChatAttachmentUrl(a, failed && !a.id.startsWith("local-"));
  return { src: fresh.data?.url ?? a.url, onError: () => setFailed(true) };
}

function ImageTile({ a, single, pending, onOpen }: { a: ChatAttachment; single: boolean; pending: boolean; onOpen: () => void }) {
  const { src, onError } = useAttachmentSrc(a);
  const ratio = a.width && a.height ? a.width / a.height : undefined;
  return (
    <button
      type="button"
      onClick={onOpen}
      disabled={pending}
      aria-label={a.file_name ? `Open ${a.file_name}` : "Open photo"}
      className={cn("relative block overflow-hidden rounded-md bg-sunken focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring", single ? "max-h-80 w-full max-w-sm" : "aspect-square w-full")}
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

function MediaTile({ a }: { a: ChatAttachment }) {
  const { src, onError } = useAttachmentSrc(a);
  if (a.kind === "video") {
    return <video src={src} onError={onError} controls preload="metadata" className="max-h-80 w-full max-w-sm rounded-md bg-sunken" aria-label={a.file_name || "Video"} />;
  }
  return <audio src={src} onError={onError} controls preload="metadata" className="w-full max-w-sm" aria-label={a.file_name || "Audio"} />;
}

function FileTile({ a, own, pending }: { a: ChatAttachment; own: boolean; pending: boolean }) {
  const { src } = useAttachmentSrc(a);
  const size = formatFileSize(a.size_bytes);
  const body = (
    <>
      <FileText aria-hidden className="size-5 shrink-0" />
      <span className="flex min-w-0 flex-1 items-baseline gap-2">
        <span className="truncate type-label">{a.file_name || "File"}</span>
        {size && <span className={cn("shrink-0 type-caption", own ? "opacity-80" : "text-fg-muted")}>{size}</span>}
      </span>
      {pending ? <Spinner size={16} label="Uploading" /> : <Download aria-hidden className="size-4 shrink-0" />}
    </>
  );
  const cls = cn("flex min-h-tap-sm w-full max-w-sm items-center gap-3 rounded-md px-3 py-2", own ? "bg-primary-fg/15" : "bg-surface");
  if (pending || !src) return <span className={cls}>{body}</span>;
  return (
    <a href={src} target="_blank" rel="noopener noreferrer" download={a.file_name || undefined} className={cn(cls, "hover:underline focus-visible:outline-2 focus-visible:outline-ring")}>
      {body}
    </a>
  );
}

/** A message's files: photos in a grid, video and audio players, documents as download rows. */
export function Attachments({ attachments, own, pending, onOpenImage }: { attachments: ChatAttachment[]; own: boolean; pending: boolean; onOpenImage: (attachmentId: string) => void }) {
  const images = attachments.filter((a) => a.kind === "image");
  const media = attachments.filter((a) => a.kind === "video" || a.kind === "audio");
  const files = attachments.filter((a) => a.kind === "file");
  return (
    <div className="flex flex-col gap-2">
      {images.length > 0 && (
        <div className={cn("grid gap-1", images.length > 1 && "grid-cols-2")}>
          {images.map((a) => (
            <ImageTile key={a.id} a={a} single={images.length === 1} pending={pending} onOpen={() => onOpenImage(a.id)} />
          ))}
        </div>
      )}
      {media.map((a) => (
        <MediaTile key={a.id} a={a} />
      ))}
      {files.map((a) => (
        <FileTile key={a.id} a={a} own={own} pending={pending} />
      ))}
    </div>
  );
}
