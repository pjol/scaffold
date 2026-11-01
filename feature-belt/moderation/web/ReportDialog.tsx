"use client";

import { errorMessage, REPORT_REASONS, useBlockUser, useCreateReport, useIsBlocked, useMe, type ReportReason } from "@scaffoldapp/shared";
import { Button, Checkbox, cn, Dialog, Field, IconButton, TextArea, toast, type ButtonProps } from "@scaffoldapp/ui-web";
import { Check, Flag } from "lucide-react";
import { useEffect, useState, type KeyboardEvent } from "react";

const NOTE_MAX = 1000;

/** The person behind reported content; adds a "Block <name>" option to the dialog. */
export interface ReportAuthor {
  id: string;
  name: string;
}

export interface ReportDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** "user" for a person, or a registered content kind: "chat.message", "feed.post", "feed.comment", "assistant.message". */
  targetKind: string;
  targetId: string;
  /** What is being reported, for the title: "message" → "Report this message". Defaults to "person" for users, else "content". */
  noun?: string;
  /** Offer to block the author (or the reported person) in the same step. */
  author?: ReportAuthor | null;
  /** Called after the report is sent (and the block, if chosen). */
  onReported?: (result: { blocked: boolean }) => void;
}

function ReasonPicker({ value, onChange }: { value: ReportReason | null; onChange: (r: ReportReason) => void }) {
  const index = value ? REPORT_REASONS.findIndex((r) => r.value === value) : -1;
  const move = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = e.key === "ArrowDown" || e.key === "ArrowRight" ? 1 : e.key === "ArrowUp" || e.key === "ArrowLeft" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    const next = REPORT_REASONS[(Math.max(index, 0) + step + REPORT_REASONS.length) % REPORT_REASONS.length];
    if (!next) return;
    onChange(next.value);
    const el = e.currentTarget.querySelector<HTMLButtonElement>(`[data-reason="${next.value}"]`);
    el?.focus();
  };
  return (
    <div role="radiogroup" aria-label="Reason" className="flex flex-col gap-1" onKeyDown={move}>
      {REPORT_REASONS.map((r, i) => {
        const selected = r.value === value;
        return (
          <button
            key={r.value}
            type="button"
            role="radio"
            aria-checked={selected}
            data-reason={r.value}
            tabIndex={selected || (index < 0 && i === 0) ? 0 : -1}
            onClick={() => onChange(r.value)}
            className={cn(
              "flex min-h-control items-center gap-3 rounded-md border px-3 text-left type-body transition-colors pointer-coarse:min-h-tap",
              "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
              selected ? "border-primary bg-primary-soft text-fg" : "border-border text-fg hover:bg-sunken",
            )}
          >
            <span
              aria-hidden
              className={cn("flex size-5 shrink-0 items-center justify-center rounded-pill border", selected ? "border-primary bg-primary text-primary-fg" : "border-border-strong")}
            >
              {selected && <Check className="size-3" />}
            </span>
            {r.label}
          </button>
        );
      })}
    </div>
  );
}

/**
 * Report a person or a piece of content. Controlled: open it from your own menu item or
 * button. Sends `POST /v1/moderation/reports`; with `author`, can block them too.
 *
 * ```tsx
 * <ReportDialog open={open} onOpenChange={setOpen} targetKind="feed.post" targetId={post.id} noun="post"
 *   author={{ id: post.author.id, name: post.author.display_name }} />
 * ```
 */
export function ReportDialog({ open, onOpenChange, targetKind, targetId, noun, author, onReported }: ReportDialogProps) {
  const me = useMe();
  const report = useCreateReport();
  const block = useBlockUser();
  const alreadyBlocked = useIsBlocked(author?.id);
  const [reason, setReason] = useState<ReportReason | null>(null);
  const [note, setNote] = useState("");
  const [alsoBlock, setAlsoBlock] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setReason(null);
    setNote("");
    setAlsoBlock(false);
    setError(null);
  }, [open]);

  const isUser = targetKind === "user";
  const label = noun ?? (isUser ? "person" : "content");
  const canBlock = !!author && author.id !== me.data?.id && !alreadyBlocked;
  const busy = report.isPending || block.isPending;

  const submit = async () => {
    if (!reason) {
      setError("Choose a reason.");
      return;
    }
    setError(null);
    try {
      await report.mutateAsync({ target_kind: targetKind, target_id: targetId, reason, note: note.trim() });
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't send the report. Try again."));
      return;
    }
    let blocked = false;
    if (alsoBlock && author) {
      try {
        await block.mutateAsync(author.id);
        blocked = true;
      } catch (e) {
        toast.error(errorMessage(e, `Report sent, but couldn't block ${author.name}. Try again from their profile.`));
      }
    }
    toast.success(blocked && author ? `Report sent and ${author.name} blocked` : "Report sent. Thank you!");
    onOpenChange(false);
    onReported?.({ blocked });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !busy && onOpenChange(o)}
      title={`Report this ${label}`}
      info="Reports go to our moderators, who review them within 24 hours. The person you report isn't told who reported them."
      size="md"
      footer={
        <>
          <Button variant="ghost" size="auto" disabled={busy} onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="danger" size="auto" loading={busy} onClick={() => void submit()}>
            Send report
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-6"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <Field label="Reason" error={error ?? undefined} required>
          <ReasonPicker
            value={reason}
            onChange={(r) => {
              setReason(r);
              setError(null);
            }}
          />
        </Field>
        <Field label="Details" info="Optional. Only moderators see this.">
          <TextArea value={note} onChange={(e) => setNote(e.target.value.slice(0, NOTE_MAX))} maxLength={NOTE_MAX} minRows={2} maxRows={6} autoGrow />
        </Field>
        {canBlock && author && <Checkbox checked={alsoBlock} onCheckedChange={setAlsoBlock} label={`Also block ${author.name}`} info="You won't see each other's content or be able to message each other." />}
      </form>
    </Dialog>
  );
}

export interface ReportButtonProps extends Omit<ReportDialogProps, "open" | "onOpenChange"> {
  /** `icon` (default) is a ghost flag button; `button` shows the "Report" label. */
  variant?: "icon" | "button";
  size?: ButtonProps["size"];
}

/** A flag button that opens {@link ReportDialog}. */
export function ReportButton({ variant = "icon", size = "sm", ...dialog }: ReportButtonProps) {
  const [open, setOpen] = useState(false);
  const label = `Report this ${dialog.noun ?? (dialog.targetKind === "user" ? "person" : "content")}`;
  return (
    <>
      {variant === "icon" ? (
        <IconButton label={label} icon={<Flag />} variant="ghost" size={size === "lg" || size === "auto" ? "md" : "sm"} onClick={() => setOpen(true)} />
      ) : (
        <Button variant="ghost" size={size} leftIcon={<Flag />} onClick={() => setOpen(true)}>
          Report
        </Button>
      )}
      <ReportDialog {...dialog} open={open} onOpenChange={setOpen} />
    </>
  );
}
