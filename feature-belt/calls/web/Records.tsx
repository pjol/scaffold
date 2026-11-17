"use client";

import {
  CALLS_RECORDING_STATUS_LABELS,
  callsEndpoints,
  errorMessage,
  formatCallsBytes,
  formatCallsDuration,
  formatCallsTime,
  formatCallsWhen,
  useCallsRecording,
  useCallsSavedChat,
  useCallsTimeZone,
  useCallsTranscript,
  useDeleteCallsRecording,
  useEndpoints,
  type CallsRecording,
} from "@scaffoldapp/shared";
import { Badge, Button, ConfirmDialog, EmptyState, ErrorState, FilePreview, Skeleton, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { Clapperboard, Download, MessageSquare, ScrollText, Sparkles, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { callsTranscriptAction } from "./seams";

const STATUS_TONE: Record<CallsRecording["status"], "neutral" | "danger" | "warning" | "success"> = {
  starting: "danger",
  recording: "danger",
  processing: "warning",
  ready: "success",
  failed: "neutral",
};

/** One row per recording: when, how long, its state; ready ones open their page. */
export function RecordingsList({ recordings }: { recordings: CallsRecording[] }) {
  const zone = useCallsTimeZone();
  if (recordings.length === 0) return <EmptyState compact icon={<Clapperboard />} title="No recordings" />;
  return (
    <ul className="flex flex-col divide-y divide-border rounded-lg border border-border bg-surface">
      {recordings.map((r) => (
        <li key={r.id} className="flex min-h-row items-center gap-3 px-4 py-3">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-md bg-primary-soft text-primary-strong">
            <Clapperboard className="size-5" aria-hidden />
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="flex min-w-0 flex-wrap items-center gap-2">
              <span className="truncate type-body font-semibold text-fg">{formatCallsWhen(r.started_at, zone)}</span>
              {r.status !== "ready" && (
                <Badge tone={STATUS_TONE[r.status]} {...(r.status === "recording" ? { dot: true } : {})}>
                  {CALLS_RECORDING_STATUS_LABELS[r.status]}
                </Badge>
              )}
            </span>
            <span className="truncate type-label text-fg-muted">
              {r.status === "failed"
                ? (r.error ?? "The recording failed.")
                : [r.duration_seconds ? formatCallsDuration(r.duration_seconds) : null, formatCallsBytes(r.size_bytes) || null, r.started_by ? `by ${r.started_by.display_name}` : null].filter(Boolean).join(" · ")}
            </span>
          </div>
          {r.status === "ready" && (
            <Button asChild size="sm" variant="secondary">
              <Link href={`/calls/recordings/${r.id}`}>Watch</Link>
            </Button>
          )}
        </li>
      ))}
    </ul>
  );
}

/** A recording's player with download and delete (the recording page). */
export function RecordingPlayer({ id }: { id: string }) {
  const rec = useCallsRecording(id);
  const del = useDeleteCallsRecording();
  const router = useRouter();
  const zone = useCallsTimeZone();
  const [deleting, setDeleting] = useState(false);
  if (rec.isPending) {
    return (
      <SkeletonGroup label="Loading recording" className="flex flex-col gap-4">
        <Skeleton className="aspect-video w-full rounded-lg" />
        <Skeleton className="h-6 w-64" />
      </SkeletonGroup>
    );
  }
  if (rec.isError) {
    return rec.error.status === 404 ? (
      <EmptyState icon={<Clapperboard />} title="Recording not found" action={<Button asChild variant="secondary"><Link href="/calls">Back to calls</Link></Button>} />
    ) : (
      <ErrorState title="Couldn't load the recording" info={rec.error.message} onRetry={() => void rec.refetch()} retrying={rec.isFetching} />
    );
  }
  const r = rec.data;
  const name = `${r.meeting_title || "Recording"} ${r.started_at.slice(0, 10)}.mp4`;
  return (
    <div className="flex flex-col gap-4">
      {r.status === "ready" && r.url ? (
        <FilePreview url={r.url} name={name} mime="video/mp4" {...(r.size_bytes ? { size: r.size_bytes } : {})} {...(r.download_url ? { downloadUrl: r.download_url } : {})} className="max-h-[70dvh]" />
      ) : (
        <EmptyState icon={<Clapperboard />} title={r.status === "failed" ? "This recording failed" : "This recording isn't ready yet"} {...(r.error ? { info: r.error } : {})} />
      )}
      <div className="flex flex-wrap items-center gap-3">
        <span className="min-w-0 basis-full type-label text-fg-muted sm:flex-1 sm:basis-auto">
          {formatCallsWhen(r.started_at, zone)}
          {r.duration_seconds ? ` · ${formatCallsDuration(r.duration_seconds)}` : ""}
          {r.size_bytes ? ` · ${formatCallsBytes(r.size_bytes)}` : ""}
          {r.expires_at ? ` · deleted on ${new Date(r.expires_at).toLocaleDateString()}` : ""}
        </span>
        {r.download_url && (
          <Button asChild variant="secondary" leftIcon={<Download />}>
            <a href={r.download_url}>Download</a>
          </Button>
        )}
        {r.can_delete && (
          <Button variant="ghost" leftIcon={<Trash2 />} onClick={() => setDeleting(true)}>
            Delete
          </Button>
        )}
      </div>
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title="Delete this recording?"
        info="The video is deleted for everyone. This can't be undone."
        confirmLabel="Delete recording"
        tone="danger"
        onConfirm={() => del.mutateAsync(r.id).then(() => (toast.success("Recording deleted"), router.push(r.session_id ? `/calls/${r.session_id}` : "/calls")))}
        errorText={(e) => errorMessage(e, "Couldn't delete the recording.")}
      />
    </div>
  );
}

/** A call's saved transcript, with the app's action on it (e.g. summarize into a page). */
export function TranscriptView({ sessionId, meetingId, title }: { sessionId: string; meetingId: string; title: string }) {
  const t = useCallsTranscript(sessionId);
  const zone = useCallsTimeZone();
  const router = useRouter();
  const action = callsTranscriptAction();
  const [running, setRunning] = useState(false);
  if (t.isPending) {
    return (
      <SkeletonGroup label="Loading transcript" className="flex flex-col gap-2">
        <SkeletonRow />
        <SkeletonRow />
      </SkeletonGroup>
    );
  }
  if (t.isError) return <ErrorState compact title="Couldn't load the transcript" info={t.error.message} onRetry={() => void t.refetch()} retrying={t.isFetching} />;
  if (t.data.lines.length === 0) return <EmptyState compact icon={<ScrollText />} title="No transcript" />;
  return (
    <div className="flex flex-col gap-3">
      {action && (
        <Button
          variant="secondary"
          leftIcon={<Sparkles />}
          className="self-start"
          loading={running}
          onClick={() => {
            setRunning(true);
            Promise.resolve(action.run({ sessionId, meetingId, title, navigate: (href) => router.push(href) }))
              .catch((e: unknown) => toast.error(errorMessage(e, "That didn't work. Try again.")))
              .finally(() => setRunning(false));
          }}
        >
          {action.label}
        </Button>
      )}
      <ol className="flex max-h-[32rem] flex-col gap-3 overflow-y-auto rounded-lg border border-border bg-surface p-4">
        {t.data.lines.map((l, i) => {
          const prev = t.data.lines[i - 1];
          const grouped = prev && prev.name === l.name && Date.parse(l.spoken_at) - Date.parse(prev.spoken_at) < 60_000;
          return (
            <li key={i} className="flex flex-col gap-0.5">
              {!grouped && (
                <span className="flex items-baseline gap-2">
                  <span className="type-label font-semibold text-fg">{l.name || "Guest"}</span>
                  <span className="type-caption text-fg-subtle">{formatCallsTime(l.spoken_at, zone)}</span>
                </span>
              )}
              <p className="type-body text-fg">{l.text}</p>
            </li>
          );
        })}
      </ol>
    </div>
  );
}

/** Chat messages the meeting kept. */
export function SavedChatView({ sessionId }: { sessionId: string }) {
  const chat = useCallsSavedChat(sessionId);
  const zone = useCallsTimeZone();
  if (chat.isPending) return <SkeletonRow />;
  if (chat.isError) return <ErrorState compact title="Couldn't load the chat" info={chat.error.message} onRetry={() => void chat.refetch()} retrying={chat.isFetching} />;
  if (chat.data.items.length === 0) return <EmptyState compact icon={<MessageSquare />} title="No saved messages" />;
  return (
    <ol className="flex max-h-[32rem] flex-col gap-3 overflow-y-auto rounded-lg border border-border bg-surface p-4">
      {chat.data.items.map((m) => (
        <li key={m.id} className="flex flex-col gap-0.5">
          <span className="flex items-baseline gap-2">
            <span className="type-label font-semibold text-fg">{m.name || "Guest"}</span>
            <span className="type-caption text-fg-subtle">{formatCallsTime(m.sent_at, zone)}</span>
          </span>
          <p className="whitespace-pre-wrap break-words type-body text-fg">{m.text}</p>
        </li>
      ))}
    </ol>
  );
}

/** Hosts: attendance as a CSV file (fetched with the session, saved by the browser). */
export function AttendanceButton({ sessionId, title }: { sessionId: string; title: string }) {
  const api = useEndpoints(callsEndpoints);
  const [busy, setBusy] = useState(false);
  return (
    <Button
      variant="secondary"
      size="sm"
      leftIcon={<Download />}
      loading={busy}
      onClick={() => {
        setBusy(true);
        api
          .attendanceCsv(sessionId)
          .then((csv) => {
            const url = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
            const a = document.createElement("a");
            a.href = url;
            a.download = `${(title || "call").replace(/[\\/:*?"<>|]/g, "-")} attendance.csv`;
            a.click();
            setTimeout(() => URL.revokeObjectURL(url), 1000);
          })
          .catch((e: unknown) => toast.error(errorMessage(e, "Couldn't download attendance.")))
          .finally(() => setBusy(false));
      }}
    >
      Attendance (CSV)
    </Button>
  );
}
