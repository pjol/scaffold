"use client";

import { assistantMessageText, assistantStopNote, type AssistantMessage } from "@scaffoldapp/shared";
import { Badge, Banner, Button, IconButton, InfoTip, Spinner, toast } from "@scaffoldapp/ui-web";
import { Copy, Flag, RotateCcw } from "lucide-react";
import { useState } from "react";
import { ReportDialog } from "@/features/moderation";
import { Markdown } from "./Markdown";
import { ToolActivity } from "./ToolActivity";

/**
 * One message. The user's own text sits in a bubble on the right; replies run full width
 * with Markdown, tool activity, and actions (copy, report). A failed reply shows what went
 * wrong and, when worth it, a retry.
 */
export function MessageItem({ message, onRetry }: { message: AssistantMessage; onRetry?: (() => void) | undefined }) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end pl-10 sm:pl-16">
        <p className="max-w-prose rounded-xl bg-primary-soft px-4 py-2.5 type-body whitespace-pre-wrap text-fg [overflow-wrap:anywhere]">{assistantMessageText(message)}</p>
      </div>
    );
  }
  return <Reply message={message} onRetry={onRetry} />;
}

function Reply({ message, onRetry }: { message: AssistantMessage; onRetry?: (() => void) | undefined }) {
  const [reporting, setReporting] = useState(false);
  const text = assistantMessageText(message);
  const streaming = message.status === "streaming";
  const note = assistantStopNote(message);

  if (message.removed) {
    return <p className="type-label text-fg-muted italic">This reply was removed after a report.</p>;
  }
  return (
    <article aria-busy={streaming || undefined} className="flex min-w-0 flex-col gap-3">
      <ToolActivity activity={message.activity} conversationId={message.conversation_id} messageId={message.id} />
      {text ? <Markdown text={text} /> : streaming && message.activity.length === 0 ? <Spinner size={18} label="Writing a reply" /> : null}
      {message.error ? (
        <Banner
          tone="danger"
          action={
            message.error.retryable && onRetry ? (
              <Button size="sm" variant="secondary" leftIcon={<RotateCcw />} onClick={onRetry}>
                Retry
              </Button>
            ) : undefined
          }
        >
          {message.error.message}
        </Banner>
      ) : null}
      {!streaming && (text || note) ? (
        <div className="flex min-h-control items-center gap-1">
          {note ? <span className="mr-2 type-caption text-fg-muted">{note}</span> : null}
          {text ? (
            <IconButton
              label="Copy reply"
              tooltip
              size="sm"
              variant="ghost"
              icon={<Copy />}
              onClick={() => {
                void navigator.clipboard.writeText(text).then(
                  () => toast.success("Copied"),
                  () => toast.error("Couldn't copy. Select the text instead."),
                );
              }}
            />
          ) : null}
          {text ? <IconButton label="Report reply" tooltip size="sm" variant="ghost" icon={<Flag />} onClick={() => setReporting(true)} /> : null}
          {message.provider === "mock" ? (
            <span className="ml-auto flex items-center gap-1">
              <Badge tone="warning" size="sm">
                Mock
              </Badge>
              <InfoTip content="Scripted reply from the development mock. No AI provider was called." />
            </span>
          ) : null}
        </div>
      ) : null}
      <ReportDialog open={reporting} onOpenChange={setReporting} targetKind="assistant.message" targetId={message.id} noun="reply" />
    </article>
  );
}
