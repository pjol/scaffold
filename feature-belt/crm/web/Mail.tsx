"use client";

import {
  CRM_MEETING_OUTCOMES,
  errorMessage,
  useCrmChangeDraft,
  useCrmDrafts,
  useCrmLogMail,
  useCrmLogMeeting,
  useCrmMailAccounts,
  useCrmMeetings,
  useCrmRecentMail,
  type CrmAssociations,
  type CrmEmailDraft,
  type CrmRecord,
} from "@scaffoldapp/shared";
import {
  Badge,
  Button,
  Card,
  ConfirmDialog,
  ConnectPrompt,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  InfoTip,
  Menu,
  MenuItem,
  Select,
  Skeleton,
  SkeletonGroup,
  TextArea,
  cn,
  toast,
} from "@scaffoldapp/ui-web";
import { ArrowDownLeft, ArrowUpRight, Bot, CalendarCheck, CalendarX, Inbox, Mail, MoreHorizontal, PenLine, Trash2 } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useMemo, useState } from "react";
import { formatDateTime, formatRelative } from "@/lib/format";
import { useCrm } from "./CrmFrame";
import { MailComposer, type ComposerContact } from "./MailComposer";
import { MailAccountIcon, MailErrorBanner, mailAccountLabel } from "./MailParts";

/** The people an email from this record can go to: the contact itself, or contacts linked to a company, deal or ticket. */
function recipientsOf(record: CrmRecord, a: CrmAssociations): ComposerContact[] {
  if (record.type === "contact") return record.emails.length > 0 ? [{ id: record.id, name: record.name, emails: record.emails }] : [];
  return a.contacts.filter((c) => !!c.email).map((c) => ({ id: c.id, name: c.name, emails: [c.email as string] }));
}

/**
 * Header actions on a record: Email (the composer when you've connected a mailbox that can
 * send, else a mail link to the contact's address) and, on contacts, Log meeting from your
 * calendars when the product connects one.
 */
export function CrmRecordMailActions({ record, associations, email }: { record: CrmRecord; associations: CrmAssociations; email: string | null }) {
  const { orgId } = useCrm();
  const accounts = useCrmMailAccounts(orgId);
  const [composing, setComposing] = useState(false);
  const [logging, setLogging] = useState(false);
  const recipients = useMemo(() => recipientsOf(record, associations), [record, associations]);
  const canSend = (accounts.data?.accounts ?? []).some((a) => a.can_send);
  const canComment = record.my_level !== "none" && record.my_level !== "view";
  const dealId = record.type === "deal" ? record.id : null;
  const ticketId = record.type === "ticket" ? record.id : null;
  return (
    <>
      {canSend && canComment && recipients.length > 0 ? (
        <Button variant="secondary" size="sm" leftIcon={<Mail />} onClick={() => setComposing(true)}>
          Email
        </Button>
      ) : email ? (
        <span className="inline-flex items-center gap-1">
          <Button asChild variant="secondary" size="sm" leftIcon={<Mail />}>
            <a href={`mailto:${email}`}>Email</a>
          </Button>
          {accounts.data && !canSend ? (
            <InfoTip label="About sending from here" content="Connect Gmail or Outlook in Settings → Connections to send from the CRM; what you send lands on this timeline." />
          ) : null}
        </span>
      ) : null}
      {record.type === "contact" && accounts.data?.meetings && canComment ? (
        <Button variant="secondary" size="sm" leftIcon={<CalendarCheck />} onClick={() => setLogging(true)}>
          Log meeting
        </Button>
      ) : null}
      {composing ? <MailComposer orgId={orgId} contacts={recipients} dealId={dealId} ticketId={ticketId} onClose={() => setComposing(false)} /> : null}
      {logging && record.type === "contact" ? <LogMeetingDialog contactId={record.id} name={record.name} onClose={() => setLogging(false)} /> : null}
    </>
  );
}

/** Calendar events with the contact (last 30 days and the next day) to log one as a meeting. */
function LogMeetingDialog({ contactId, name, onClose }: { contactId: string; name: string; onClose: () => void }) {
  const meetings = useCrmMeetings(contactId);
  const log = useCrmLogMeeting(contactId);
  const [picked, setPicked] = useState<string | null>(null);
  const [notes, setNotes] = useState("");
  const [outcome, setOutcome] = useState("completed");
  const items = meetings.data?.items ?? [];
  const submit = () => {
    if (!picked) return;
    log.mutate(
      { event_id: picked, ...(notes.trim() ? { body: notes.trim() } : {}), outcome },
      {
        onSuccess: () => {
          toast.success("Meeting logged");
          onClose();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't log the meeting.")),
      },
    );
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`Log a meeting with ${name}`}
      info="Events on your calendars from the last 30 days and the next day that have this contact as a guest. The event's title, time and guests go on the timeline."
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button disabled={!picked} loading={log.isPending} onClick={submit}>
            Log meeting
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {meetings.isPending ? (
          <SkeletonGroup label="Loading your calendar" className="flex flex-col gap-2">
            <Skeleton className="h-14 w-full" />
            <Skeleton className="h-14 w-full" />
          </SkeletonGroup>
        ) : meetings.isError ? (
          <MailErrorBanner error={meetings.error} capability={null} />
        ) : items.length === 0 ? (
          <EmptyState compact icon={<CalendarX />} title="No events with this contact" info="Only events where one of the contact's email addresses is a guest show here. Log other meetings in the composer under the timeline." />
        ) : (
          <ul className="flex flex-col gap-1" role="radiogroup" aria-label="Events">
            {items.map((m) => (
              <li key={m.id}>
                <button
                  type="button"
                  role="radio"
                  aria-checked={picked === m.id}
                  onClick={() => setPicked(m.id)}
                  className={cn(
                    "flex w-full flex-col gap-0.5 rounded-md border px-3 py-2 text-left transition-colors focus-visible:outline-2 focus-visible:outline-ring",
                    picked === m.id ? "border-primary bg-primary-soft" : "border-border hover:bg-sunken",
                  )}
                >
                  <span className="type-label font-semibold text-fg">{m.title || "Untitled event"}</span>
                  <span className="type-caption text-fg-muted">
                    {formatDateTime(m.start)}
                    {m.source ? ` · ${m.source}` : ""}
                  </span>
                  {m.attendees.length > 0 ? <span className="truncate type-caption text-fg-subtle">{m.attendees.join(", ")}</span> : null}
                </button>
              </li>
            ))}
          </ul>
        )}
        {items.length > 0 ? (
          <>
            <Field label="Outcome">
              <Select value={outcome} onValueChange={setOutcome} options={CRM_MEETING_OUTCOMES} />
            </Field>
            <Field label="Notes">
              <TextArea value={notes} maxLength={20000} rows={3} onChange={(e) => setNotes(e.target.value)} placeholder="What was agreed" />
            </Field>
          </>
        ) : null}
      </div>
    </Dialog>
  );
}

/**
 * On contacts: your drafts to this contact and the latest mail with them in one of your
 * connected mailboxes (read live; only what you log is kept).
 */
export function CrmRecordMailPanel({ record }: { record: CrmRecord; associations: CrmAssociations }) {
  const { orgId } = useCrm();
  const pathname = usePathname() ?? "/crm";
  const accounts = useCrmMailAccounts(orgId);
  const drafts = useCrmDrafts(record.type === "contact" ? orgId : null);
  const [chosen, setChosen] = useState<string>("");
  const [openDraft, setOpenDraft] = useState<CrmEmailDraft | null>(null);
  if (record.type !== "contact" || accounts.isPending) return null;
  const readers = (accounts.data?.accounts ?? []).filter((a) => a.can_read);
  const reader = readers.find((a) => a.connection_id === chosen) ?? readers[0] ?? null;
  const mine = (drafts.data?.items ?? []).filter((d) => d.contact.id === record.id);
  const readOffers = (accounts.data?.offers ?? []).filter((o) => o.capability.includes("mail_read"));
  const canComment = record.my_level !== "none" && record.my_level !== "view";
  if (readers.length === 0 && mine.length === 0 && readOffers.length === 0) return null;
  return (
    <Card padding="compact" className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <h2 className="flex-1 type-title-sm text-fg">Email</h2>
        <InfoTip
          label="About email here"
          content="Recent mail with this contact is read live from your own mailbox and isn't stored. Log keeps that one email on the timeline for everyone who can open this contact."
        />
      </div>
      {mine.length > 0 ? (
        <section className="flex flex-col gap-1" aria-label="Your drafts">
          <h3 className="type-caption font-semibold text-fg-muted">Drafts</h3>
          <ul className="flex flex-col gap-1">
            {mine.map((d) => (
              <DraftRow key={d.id} draft={d} orgId={orgId} onOpen={() => setOpenDraft(d)} />
            ))}
          </ul>
        </section>
      ) : null}
      {reader ? (
        <section className="flex flex-col gap-2" aria-label="Recent mail">
          <div className="flex items-center gap-2">
            <h3 className="flex-1 type-caption font-semibold text-fg-muted">Recent mail</h3>
            {readers.length > 1 ? (
              <Select aria-label="Mailbox" className="w-44" value={reader.connection_id} onValueChange={setChosen} options={readers.map((a) => ({ value: a.connection_id, label: mailAccountLabel(a) }))} />
            ) : (
              <span className="inline-flex min-w-0 items-center gap-1 truncate type-caption text-fg-muted">
                <MailAccountIcon provider={reader.provider} />
                {reader.account}
              </span>
            )}
          </div>
          <RecentMail contactId={record.id} connectionId={reader.connection_id} canLog={canComment} />
        </section>
      ) : (
        <ConnectPrompt offers={readOffers} returnTo={pathname} linkAs={Link} title="See mail with this contact" />
      )}
      {openDraft ? (
        <MailComposer
          orgId={orgId}
          contacts={[{ id: record.id, name: record.name, emails: record.emails }]}
          draft={openDraft}
          dealId={openDraft.deal?.id ?? null}
          ticketId={openDraft.ticket?.id ?? null}
          onClose={() => setOpenDraft(null)}
        />
      ) : null}
    </Card>
  );
}

function DraftRow({ draft: d, orgId, onOpen }: { draft: CrmEmailDraft; orgId: string; onOpen: () => void }) {
  const change = useCrmChangeDraft(orgId);
  const [discarding, setDiscarding] = useState(false);
  return (
    <li className="-mx-2 flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-sunken">
      <button type="button" onClick={onOpen} className="flex min-w-0 flex-1 flex-col text-left focus-visible:outline-2 focus-visible:outline-ring">
        <span className="truncate type-label font-semibold text-fg">{d.subject || "(no subject)"}</span>
        <span className="flex items-center gap-1 truncate type-caption text-fg-muted">
          {d.agent ? (
            <>
              <Bot className="size-3.5 shrink-0" aria-hidden />
              Drafted by {d.agent} ·
            </>
          ) : null}{" "}
          {formatRelative(d.updated_at)}
        </span>
      </button>
      <Menu trigger={<IconButton label="Draft actions" size="sm" icon={<MoreHorizontal />} />}>
        <MenuItem icon={<PenLine />} onSelect={onOpen}>
          Review and send
        </MenuItem>
        <MenuItem icon={<Trash2 />} destructive onSelect={() => setDiscarding(true)}>
          Discard
        </MenuItem>
      </Menu>
      <ConfirmDialog
        open={discarding}
        onOpenChange={setDiscarding}
        tone="danger"
        title="Discard this draft?"
        confirmLabel="Discard"
        onConfirm={() => change.mutateAsync({ op: "delete", id: d.id }).then(() => toast.success("Draft discarded"), (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't discard it."))))}
      >
        It can&apos;t be undone.
      </ConfirmDialog>
    </li>
  );
}

function RecentMail({ contactId, connectionId, canLog }: { contactId: string; connectionId: string; canLog: boolean }) {
  const recent = useCrmRecentMail(contactId, connectionId);
  const log = useCrmLogMail(contactId);
  const [logging, setLogging] = useState<string | null>(null);
  if (recent.isPending) {
    return (
      <SkeletonGroup label="Reading your mailbox" className="flex flex-col gap-2">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
      </SkeletonGroup>
    );
  }
  if (recent.isError) {
    return recent.error.code?.startsWith("connection_") ? (
      <MailErrorBanner error={recent.error} capability={null} />
    ) : (
      <ErrorState compact title="Couldn't read your mailbox" info={recent.error.message} onRetry={() => void recent.refetch()} retrying={recent.isRefetching} />
    );
  }
  if (recent.data.items.length === 0) return <p className="type-label text-fg-subtle">No mail with this contact in this mailbox</p>;
  return (
    <ul className="flex flex-col gap-1">
      {recent.data.items.map((m) => (
        <li key={m.message_id} className="-mx-2 flex items-start gap-2 rounded-md px-2 py-1.5 hover:bg-sunken">
          <span className="mt-0.5 text-fg-muted" title={m.inbound ? "Received" : "Sent"}>
            {m.inbound ? <ArrowDownLeft className="size-4" aria-label="Received" /> : <ArrowUpRight className="size-4" aria-label="Sent" />}
          </span>
          <div className="flex min-w-0 flex-1 flex-col">
            <span className="truncate type-label font-semibold text-fg">{m.subject || "(no subject)"}</span>
            <span className="truncate type-caption text-fg-muted">{m.snippet}</span>
            <time className="type-caption text-fg-subtle" dateTime={m.at}>
              {formatDateTime(m.at)}
            </time>
          </div>
          {m.logged ? (
            <Badge tone="success">Logged</Badge>
          ) : canLog ? (
            <Button
              variant="ghost"
              size="sm"
              leftIcon={<Inbox />}
              loading={log.isPending && logging === m.message_id}
              onClick={() => {
                setLogging(m.message_id);
                log.mutate(
                  { connection_id: connectionId, message_id: m.message_id },
                  { onSuccess: () => toast.success("Logged to the timeline"), onError: (e) => toast.error(errorMessage(e, "Couldn't log it.")), onSettled: () => setLogging(null) },
                );
              }}
            >
              Log
            </Button>
          ) : null}
        </li>
      ))}
    </ul>
  );
}
