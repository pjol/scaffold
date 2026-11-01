"use client";

import {
  SUPPORT_ATTACHMENT_PURPOSE,
  SUPPORT_ATTACHMENT_TYPES,
  SUPPORT_MAX_ATTACHMENT_BYTES,
  SUPPORT_MAX_ATTACHMENTS,
  SUPPORT_STATUS_LABEL,
  errorMessage,
  useUpload,
  type SupportAttachment,
  type SupportAttachmentInput,
  type SupportMessage,
  type SupportStatus,
} from "@scaffoldapp/shared";
import { Badge, Button, IconButton, InfoTip, Spinner, cn, type BadgeTone } from "@scaffoldapp/ui-web";
import { ChevronLeft, CircleAlert, FileText, Paperclip, X } from "lucide-react";
import Link from "next/link";
import { useRef, useState } from "react";
import { formatDateTime, formatRelative } from "@/lib/format";

const STATUS_TONE: Record<SupportStatus, BadgeTone> = { open: "primary", answered: "success", closed: "neutral" };

export function TicketStatusBadge({ status }: { status: SupportStatus }) {
  return (
    <Badge tone={STATUS_TONE[status]} dot>
      {SUPPORT_STATUS_LABEL[status]}
    </Badge>
  );
}

/** A ticket's title: wraps instead of truncating (subjects are long), with ⓘ and the status. */
export function TicketHeader({ title, info, status }: { title: string; info: string; status: SupportStatus }) {
  return (
    <div className="flex min-h-control-lg items-start gap-3">
      <h1 className="min-w-0 flex-1 type-title break-words text-fg">
        {title}
        <span className="ml-2 inline-flex align-middle">
          <InfoTip content={info} size="md" />
        </span>
      </h1>
      <span className="flex min-h-control items-center">
        <TicketStatusBadge status={status} />
      </span>
    </div>
  );
}

/** A back link above a screen title (keyboard and touch sized). */
export function BackLink({ href, label }: { href: string; label: string }) {
  return (
    <Link href={href} className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
      <ChevronLeft className="size-4" aria-hidden />
      {label}
    </Link>
  );
}

export function formatBytes(bytes: number): string {
  if (bytes <= 0) return "";
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`;
  return `${Math.round((bytes / (1024 * 1024)) * 10) / 10} MB`;
}

function AttachmentLink({ a }: { a: SupportAttachment }) {
  const size = formatBytes(a.size_bytes);
  return (
    <a
      href={a.url}
      target="_blank"
      rel="noreferrer"
      className="flex min-h-control max-w-full items-center gap-2 rounded-md border border-border bg-surface px-3 type-label text-fg hover:border-border-strong pointer-coarse:min-h-tap"
    >
      <FileText className="size-4 shrink-0 text-fg-muted" aria-hidden />
      <span className="truncate">{a.file_name}</span>
      {size && <span className="shrink-0 text-fg-muted">{size}</span>}
    </a>
  );
}

/**
 * One thread entry. Staff replies sit on a tinted surface; internal notes (staff views
 * only) on the warning tint with a badge, so they're never mistaken for a reply.
 */
export function ThreadMessage({ message, now, staffView = false }: { message: SupportMessage; now: number; staffView?: boolean }) {
  const staff = message.author_role === "staff";
  return (
    <article
      className={cn(
        "flex flex-col gap-3 rounded-lg border p-4",
        message.internal ? "border-warning bg-warning-soft" : staff ? "border-border bg-sunken" : "border-border bg-surface",
      )}
    >
      <header className="flex flex-wrap items-center gap-2">
        <span className="min-w-0 truncate type-label font-semibold text-fg">{message.author_name}</span>
        {message.internal ? (
          <Badge tone="warning">Internal note</Badge>
        ) : staff ? (
          <Badge tone="primary">{staffView ? "Reply" : "Support"}</Badge>
        ) : null}
        <time dateTime={message.created_at} title={formatDateTime(message.created_at)} className="ml-auto type-caption text-fg-muted">
          {formatRelative(message.created_at, now)}
        </time>
      </header>
      <p className="type-body break-words whitespace-pre-wrap text-fg">{message.body}</p>
      {message.attachments.length > 0 && (
        <ul className="flex flex-wrap gap-2" aria-label="Attachments">
          {message.attachments.map((a) => (
            <li key={a.id} className="max-w-full">
              <AttachmentLink a={a} />
            </li>
          ))}
        </ul>
      )}
    </article>
  );
}

/** A file being attached: uploading, uploaded (has a key) or failed. */
export interface PendingAttachment {
  id: string;
  name: string;
  size: number;
  key: string | null;
  error: string | null;
}

export function attachmentInputs(items: readonly PendingAttachment[]): SupportAttachmentInput[] {
  return items.filter((a) => a.key).map((a) => ({ key: a.key as string, name: a.name, size: a.size }));
}

export function attachmentsBusy(items: readonly PendingAttachment[]): boolean {
  return items.some((a) => !a.key && !a.error);
}

let seq = 0;

/**
 * Attach up to 5 files (photos, videos, PDFs, text; 25 MB each). Each uploads as soon as
 * it's picked (purpose `support_attachment`); the parent sends the keys with the message.
 */
export function AttachmentPicker({ value, onChange, disabled }: { value: PendingAttachment[]; onChange: (next: PendingAttachment[]) => void; disabled?: boolean }) {
  const input = useRef<HTMLInputElement>(null);
  const upload = useUpload();
  const latest = useRef(value);
  latest.current = value;
  const [note, setNote] = useState<string | null>(null);
  const room = SUPPORT_MAX_ATTACHMENTS - value.length;

  const patch = (id: string, change: Partial<PendingAttachment>) => {
    onChange(latest.current.map((a) => (a.id === id ? { ...a, ...change } : a)));
  };

  const add = (files: FileList | null) => {
    if (!files) return;
    const picked = Array.from(files).slice(0, Math.max(0, room));
    setNote(files.length > picked.length ? `Attach up to ${SUPPORT_MAX_ATTACHMENTS} files.` : null);
    const added: PendingAttachment[] = [];
    for (const file of picked) {
      const item: PendingAttachment = { id: `a${++seq}`, name: file.name, size: file.size, key: null, error: null };
      if (!SUPPORT_ATTACHMENT_TYPES.includes(file.type)) item.error = "This file type can't be attached.";
      else if (file.size > SUPPORT_MAX_ATTACHMENT_BYTES) item.error = "Over 25 MB.";
      added.push(item);
      if (item.error) continue;
      upload.mutate(
        { file, purpose: SUPPORT_ATTACHMENT_PURPOSE, maxBytes: SUPPORT_MAX_ATTACHMENT_BYTES },
        {
          onSuccess: (r) => patch(item.id, { key: r.key }),
          onError: (e) => patch(item.id, { error: errorMessage(e, "Upload failed.") }),
        },
      );
    }
    onChange([...latest.current, ...added]);
    if (input.current) input.current.value = "";
  };

  return (
    <div className="flex flex-col gap-2">
      {value.length > 0 && (
        <ul className="flex flex-wrap gap-2" aria-label="Attachments">
          {value.map((a) => (
            <li
              key={a.id}
              className={cn(
                "flex min-h-control max-w-full items-center gap-2 rounded-md border pl-3 type-label",
                a.error ? "border-danger bg-danger-soft text-danger-strong" : "border-border bg-surface text-fg",
              )}
            >
              {a.error ? <CircleAlert className="size-4 shrink-0" aria-hidden /> : !a.key ? <Spinner size={14} label="Uploading" /> : <FileText className="size-4 shrink-0 text-fg-muted" aria-hidden />}
              <span className="truncate">{a.name}</span>
              <span className="shrink-0 text-fg-muted">{a.error ?? formatBytes(a.size)}</span>
              <IconButton label={`Remove ${a.name}`} icon={<X />} size="sm" variant="ghost" onClick={() => onChange(latest.current.filter((x) => x.id !== a.id))} />
            </li>
          ))}
        </ul>
      )}
      <input
        ref={input}
        type="file"
        multiple
        hidden
        accept={SUPPORT_ATTACHMENT_TYPES.join(",")}
        onChange={(e) => add(e.target.files)}
      />
      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" variant="secondary" size="sm" leftIcon={<Paperclip />} disabled={disabled || room <= 0} onClick={() => input.current?.click()}>
          Attach files
        </Button>
        {note && <span className="type-caption text-fg-muted">{note}</span>}
      </div>
    </div>
  );
}
