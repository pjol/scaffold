"use client";

import {
  SUPPORT_CATEGORY_LABEL,
  errorMessage,
  useAdminReply,
  useAdminTicket,
  useAdminUpdateTicket,
  useMe,
  useNow,
  useSupportAssignees,
  type AdminSupportTicketDetail,
  type SupportStatus,
} from "@scaffoldapp/shared";
import { Button, Card, ErrorState, Field, SegmentedControl, Select, Skeleton, SkeletonGroup, TextArea, toast } from "@scaffoldapp/ui-web";
import { CircleCheck, Lock, MessageSquareReply, NotebookPen, Send } from "lucide-react";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { Section } from "@/components/shell/PageHeader";
import { formatDateTime } from "@/lib/format";
import { BackLink, ThreadMessage, TicketHeader } from "./components";
import { supportPaths } from "./registry";

const UNASSIGNED = "__none";

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-4">
      <dt className="type-label text-fg-muted sm:w-32 sm:shrink-0">{label}</dt>
      <dd className="flex min-w-0 items-center gap-2 type-body text-fg">{children}</dd>
    </div>
  );
}

function Controls({ ticket }: { ticket: AdminSupportTicketDetail }) {
  const update = useAdminUpdateTicket(ticket.id);
  const staff = useSupportAssignees();
  const me = useMe();
  const assigneeOptions = [
    { value: UNASSIGNED, label: "Unassigned" },
    ...(staff.data?.items ?? []).map((s) => ({ value: s.id, label: s.id === me.data?.id ? `${s.display_name || s.email} (you)` : s.display_name || s.email })),
  ];
  // Keep a deactivated assignee visible until someone changes it.
  if (ticket.assignee && !assigneeOptions.some((o) => o.value === ticket.assignee?.id)) {
    assigneeOptions.push({ value: ticket.assignee.id, label: ticket.assignee.display_name || ticket.assignee.email });
  }
  const save = (body: { status?: SupportStatus; assignee_id?: string }, done: string) =>
    update.mutate(body, { onSuccess: () => toast.success(done), onError: (e) => toast.error(errorMessage(e, "Couldn't save. Try again.")) });

  return (
    <dl className="flex flex-col gap-4">
      <Fact label="Status">
        <SegmentedControl<SupportStatus>
          aria-label="Status"
          className="w-full sm:w-80"
          value={ticket.status}
          onValueChange={(v) => v !== ticket.status && save({ status: v }, "Status updated")}
          options={[
            { value: "open", label: "Open" },
            { value: "answered", label: "Answered" },
            { value: "closed", label: "Closed" },
          ]}
        />
      </Fact>
      <Fact label="Assignee">
        <Select
          aria-label="Assignee"
          className="w-full sm:w-64"
          options={assigneeOptions}
          value={ticket.assignee?.id ?? UNASSIGNED}
          onValueChange={(v) => save({ assignee_id: v === UNASSIGNED ? "" : v }, v === UNASSIGNED ? "Unassigned" : "Assigned")}
        />
      </Fact>
    </dl>
  );
}

type Mode = "reply" | "note";

function Composer({ ticket }: { ticket: AdminSupportTicketDetail }) {
  const reply = useAdminReply(ticket.id);
  const [mode, setMode] = useState<Mode>("reply");
  const [body, setBody] = useState("");
  const send = (close: boolean) => {
    if (!body.trim()) return;
    reply.mutate(
      { body: body.trim(), internal: mode === "note", ...(close ? { close: true } : {}) },
      {
        onSuccess: () => {
          setBody("");
          toast.success(mode === "note" ? "Note added" : close ? "Reply sent and request closed" : "Reply sent");
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't send. Try again.")),
      },
    );
  };
  return (
    <Card>
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          send(false);
        }}
      >
        <SegmentedControl<Mode>
          aria-label="Message type"
          value={mode}
          onValueChange={setMode}
          className="self-start"
          options={[
            { value: "reply", label: "Reply", icon: <MessageSquareReply aria-hidden /> },
            { value: "note", label: "Internal note", icon: <NotebookPen aria-hidden /> },
          ]}
        />
        <Field
          label={mode === "note" ? "Note" : "Reply"}
          info={mode === "note" ? "Only staff see notes." : `${ticket.user.email} gets this by email and as a notification.`}
        >
          <TextArea value={body} onChange={(e) => setBody(e.target.value)} maxLength={10000} minRows={4} maxRows={14} />
        </Field>
        <div className="flex flex-wrap gap-2">
          <Button type="submit" size="auto" leftIcon={mode === "note" ? <Lock /> : <Send />} loading={reply.isPending} disabled={!body.trim()}>
            {mode === "note" ? "Add note" : "Send reply"}
          </Button>
          {mode === "reply" && ticket.status !== "closed" && (
            <Button type="button" variant="secondary" size="auto" leftIcon={<CircleCheck />} disabled={!body.trim() || reply.isPending} onClick={() => send(true)}>
              Send and close
            </Button>
          )}
        </div>
      </form>
    </Card>
  );
}

/** Admin → Support → one request: requester, status, assignee, the full thread with notes, and the reply/note composer. */
export function AdminTicketScreen({ id }: { id: string }) {
  const ticket = useAdminTicket(id);
  const now = useNow(60_000);
  const t = ticket.data;
  return (
    <div className="flex flex-col gap-2">
      <BackLink href={supportPaths.admin} label="Support" />
      {ticket.isPending ? (
        <SkeletonGroup label="Loading request" className="flex flex-col gap-4">
          <Skeleton className="h-8 w-2/3" />
          <Skeleton className="h-40" />
          <Skeleton className="h-32" />
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
          <TicketHeader title={t.subject} info={`Request #${t.number}`} status={t.status} />
          <div className="flex flex-col gap-section">
            <Card>
              <dl className="flex flex-col gap-4">
                <Fact label="From">
                  {t.user.status === "deleted" ? (
                    <span className="text-fg-muted">Deleted user</span>
                  ) : (
                    <Link href={`/admin/users/${t.user.id}`} className="truncate font-semibold underline-offset-2 hover:underline">
                      {t.user.display_name ? `${t.user.display_name} · ${t.user.email}` : t.user.email}
                    </Link>
                  )}
                </Fact>
                <Fact label="Category">{SUPPORT_CATEGORY_LABEL[t.category]}</Fact>
                <Fact label="App">{[t.client_platform, t.client_version].filter(Boolean).join(" ") || "Unknown"}</Fact>
                <Fact label="Opened">{formatDateTime(t.created_at)}</Fact>
              </dl>
            </Card>
            <Section title="Handling">
              <Controls ticket={t} />
            </Section>
            <Section title="Conversation">
              <div className="flex flex-col gap-4">
                <ol className="flex flex-col gap-4" aria-label="Conversation">
                  {t.messages.map((m) => (
                    <li key={m.id}>
                      <ThreadMessage message={m} now={now} staffView />
                    </li>
                  ))}
                </ol>
                <Composer ticket={t} />
              </div>
            </Section>
          </div>
        </div>
      )}
    </div>
  );
}
