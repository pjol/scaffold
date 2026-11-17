"use client";

import { errorMessage, filesEndpoints, filesFormatSize, hasErrorCode, useApi, useFilesPublicRequest } from "@scaffoldapp/shared";
import { Banner, Button, EmptyState, ErrorState, Field, FileIcon, Input, Skeleton, cn } from "@scaffoldapp/ui-web";
import { CircleAlert, CircleCheck, Inbox, Link2Off, Upload } from "lucide-react";
import { useRef, useState, type DragEvent, type FormEvent } from "react";
import { formatDate } from "@/lib/format";
import { xhrPut } from "./uploads";

interface Sent {
  key: string;
  name: string;
  size: number;
  sent: number;
  state: "sending" | "done" | "failed";
  error?: string;
}

/**
 * /f/request/[token]: someone without an account sends files into a folder through a file
 * request. They see the title, the instructions and the limits, never the folder.
 */
export function FilesRequestScreen({ token }: { token: string }) {
  const req = useFilesPublicRequest(token);
  const api = useApi();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [ready, setReady] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [sent, setSent] = useState<Sent[]>([]);
  const [dropping, setDropping] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const patch = (key: string, p: Partial<Sent>) => setSent((list) => list.map((s) => (s.key === key ? { ...s, ...p } : s)));

  const send = async (files: File[]) => {
    const f = filesEndpoints(api.http);
    for (const file of files) {
      const key = `${file.name}-${file.size}-${Math.random().toString(36).slice(2)}`;
      setSent((list) => [...list, { key, name: file.name, size: file.size, sent: 0, state: "sending" }]);
      try {
        const t = await f.publicRequestUpload(token, { name: file.name, content_type: file.type || "application/octet-stream", size: file.size, uploader_name: name.trim(), uploader_email: email.trim() });
        await xhrPut(t.upload_url, t.headers, file, { onProgress: (n) => patch(key, { sent: n }) });
        const done = await f.publicRequestComplete(token, t.upload_id);
        patch(key, { state: "done", sent: file.size, name: done.name });
      } catch (e) {
        patch(key, { state: "failed", error: errorMessage(e, "That file didn't send. Try again.") });
      }
    }
    void req.refetch();
  };

  const start = (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return setFormError("Enter your name.");
    if (req.data?.require_email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) return setFormError("Enter a valid email address.");
    setFormError(null);
    setReady(true);
  };

  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setDropping(false);
    if (ready && e.dataTransfer.files.length) void send(Array.from(e.dataTransfer.files));
  };

  return (
    <div className="mx-auto flex w-full max-w-form flex-col gap-6 px-gutter py-section">
      {req.isPending ? (
        <div className="flex flex-col gap-4">
          <Skeleton className="h-8 w-64" />
          <Skeleton className="h-48 w-full rounded-lg" />
        </div>
      ) : req.isError ? (
        req.error.status === 404 || hasErrorCode(req.error, "request_not_found") ? (
          <EmptyState icon={<Link2Off />} title={errorMessage(req.error, "This file request doesn't take files anymore.")} />
        ) : (
          <ErrorState title="Couldn't open this request" info={errorMessage(req.error, "")} onRetry={() => void req.refetch()} />
        )
      ) : (
        <>
          <header className="flex flex-col gap-2">
            <div className="flex items-center gap-3">
              <Inbox className="size-6 shrink-0 text-primary-strong" aria-hidden />
              <h1 className="min-w-0 type-title text-fg">{req.data.title}</h1>
            </div>
            <p className="type-label text-fg-muted">
              {[
                `${req.data.requested_by || "Someone"}${req.data.org_name ? ` at ${req.data.org_name}` : ""} asked for files`,
                req.data.expires_at ? `until ${formatDate(req.data.expires_at)}` : null,
                `up to ${filesFormatSize(req.data.max_file_bytes)} each`,
                req.data.files_left !== null ? `${req.data.files_left} more ${req.data.files_left === 1 ? "file" : "files"}` : null,
              ]
                .filter(Boolean)
                .join(" · ")}
            </p>
          </header>
          {req.data.instructions ? <p className="type-body whitespace-pre-wrap text-fg">{req.data.instructions}</p> : null}
          {!ready ? (
            <form onSubmit={start} className="flex flex-col gap-4 rounded-xl border border-border bg-surface p-card">
              {formError ? <Banner tone="danger">{formError}</Banner> : null}
              <Field label="Your name" required>
                <Input value={name} maxLength={100} autoComplete="name" onChange={(e) => setName(e.target.value)} />
              </Field>
              <Field label="Your email" required={req.data.require_email}>
                <Input type="email" value={email} maxLength={254} autoComplete="email" onChange={(e) => setEmail(e.target.value)} />
              </Field>
              <Button type="submit">Continue</Button>
            </form>
          ) : (
            <div
              onDragOver={(e) => {
                e.preventDefault();
                setDropping(true);
              }}
              onDragLeave={() => setDropping(false)}
              onDrop={onDrop}
              className={cn("flex flex-col items-center gap-3 rounded-xl border-2 border-dashed px-card py-10 text-center", dropping ? "border-primary bg-primary-soft" : "border-border-strong bg-surface")}
            >
              <Upload className="size-8 text-fg-muted" aria-hidden />
              <p className="type-body text-fg">Drop files here or choose them</p>
              <Button onClick={() => input.current?.click()} disabled={req.data.files_left === 0}>
                Choose files
              </Button>
              <input ref={input} type="file" multiple hidden onChange={(e) => e.target.files && void send(Array.from(e.target.files)).finally(() => (e.target.value = ""))} />
            </div>
          )}
          {sent.length > 0 ? (
            <ul className="flex flex-col gap-2" aria-label="Files sent" aria-live="polite">
              {sent.map((s) => (
                <li key={s.key} className="flex items-center gap-3 rounded-lg border border-border bg-surface p-3">
                  <FileIcon name={s.name} className="size-6" />
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <span className="truncate type-label font-semibold text-fg">{s.name}</span>
                    {s.state === "sending" ? (
                      <div className="h-1.5 overflow-hidden rounded-pill bg-sunken" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round((s.sent / Math.max(1, s.size)) * 100)}>
                        <div className="h-full rounded-pill bg-primary" style={{ width: `${Math.max(2, (s.sent / Math.max(1, s.size)) * 100)}%` }} />
                      </div>
                    ) : (
                      <span className={cn("type-caption", s.state === "failed" ? "text-danger-strong" : "text-fg-muted")}>{s.state === "failed" ? s.error : `Sent · ${filesFormatSize(s.size)}`}</span>
                    )}
                  </div>
                  {s.state === "done" ? <CircleCheck className="size-5 shrink-0 text-success-strong" aria-label="Sent" /> : s.state === "failed" ? <CircleAlert className="size-5 shrink-0 text-danger-strong" aria-label="Failed" /> : null}
                </li>
              ))}
            </ul>
          ) : null}
        </>
      )}
    </div>
  );
}
