"use client";

import { SUPPORT_BODY_MAX, SUPPORT_CATEGORY_LABEL, errorMessage, useCloseTicket, useNow, useReopenTicket, useReplyTicket, useTicket, type SupportTicketDetail } from "@scaffoldapp/shared";
import { Banner, Button, Card, ErrorState, Field, Skeleton, SkeletonGroup, TextArea, toast } from "@scaffoldapp/ui-web";
import { CircleCheck, RotateCcw, Send } from "lucide-react";
import { useState } from "react";
import { formatDate } from "@/lib/format";
import { AttachmentPicker, BackLink, ThreadMessage, TicketHeader, attachmentInputs, attachmentsBusy, type PendingAttachment } from "./components";
import { supportPaths } from "./registry";

function Composer({ ticket }: { ticket: SupportTicketDetail }) {
  const reply = useReplyTicket(ticket.id);
  const [body, setBody] = useState("");
  const [files, setFiles] = useState<PendingAttachment[]>([]);
  const busy = attachmentsBusy(files);
  return (
    <Card>
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (!body.trim() || busy) return;
          reply.mutate(
            { body: body.trim(), attachments: attachmentInputs(files) },
            {
              onSuccess: () => {
                setBody("");
                setFiles([]);
              },
              onError: (err) => toast.error(errorMessage(err, "Couldn't send your reply. Try again.")),
            },
          );
        }}
      >
        <Field label={ticket.status === "closed" ? "Reply to reopen" : "Reply"}>
          <TextArea value={body} onChange={(e) => setBody(e.target.value)} maxLength={SUPPORT_BODY_MAX} minRows={3} maxRows={12} />
        </Field>
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <AttachmentPicker value={files} onChange={setFiles} disabled={reply.isPending} />
          <Button type="submit" size="auto" leftIcon={<Send />} loading={reply.isPending} disabled={!body.trim() || busy} className="self-start">
            Send
          </Button>
        </div>
      </form>
    </Card>
  );
}

/** One support request: the thread (live), reply with files, close or reopen. */
export function TicketScreen({ id }: { id: string }) {
  const ticket = useTicket(id);
  const close = useCloseTicket();
  const reopen = useReopenTicket();
  const now = useNow(60_000);
  const t = ticket.data;

  return (
    <div className="flex flex-col gap-2">
      <BackLink href={supportPaths.list} label="Support requests" />
      {ticket.isPending ? (
        <SkeletonGroup label="Loading request" className="flex flex-col gap-4">
          <Skeleton className="h-8 w-2/3" />
          <Skeleton className="h-32" />
          <Skeleton className="h-24" />
        </SkeletonGroup>
      ) : ticket.isError || !t ? (
        <Card padding="compact">
          <ErrorState
            compact
            title={ticket.error?.status === 404 ? "This request doesn't exist" : "Couldn't load this request"}
            info={ticket.error?.message}
            onRetry={() => void ticket.refetch()}
            retrying={ticket.isFetching}
          />
        </Card>
      ) : (
        <div className="flex max-w-prose flex-col gap-4 lg:gap-6">
          <TicketHeader title={t.subject} info={`Request #${t.number} · ${SUPPORT_CATEGORY_LABEL[t.category]} · opened ${formatDate(t.created_at)}`} status={t.status} />
          <div className="flex flex-col gap-section">
            <ol className="flex flex-col gap-4" aria-label="Conversation">
              {t.messages.map((m) => (
                <li key={m.id}>
                  <ThreadMessage message={m} now={now} />
                </li>
              ))}
            </ol>
            <div className="flex flex-col gap-4">
              {t.status === "closed" ? (
                <Banner
                  tone="neutral"
                  action={
                    <Button
                      size="sm"
                      variant="secondary"
                      leftIcon={<RotateCcw />}
                      loading={reopen.isPending}
                      onClick={() => reopen.mutate(t.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't reopen. Try again.")) })}
                    >
                      Reopen
                    </Button>
                  }
                >
                  This request is closed.
                </Banner>
              ) : null}
              <Composer ticket={t} />
              {t.status !== "closed" && (
                <Button
                  variant="ghost"
                  size="auto"
                  leftIcon={<CircleCheck />}
                  loading={close.isPending}
                  className="self-start"
                  onClick={() =>
                    close.mutate(t.id, {
                      onSuccess: () => toast.success("Request closed"),
                      onError: (e) => toast.error(errorMessage(e, "Couldn't close. Try again.")),
                    })
                  }
                >
                  Close request
                </Button>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
