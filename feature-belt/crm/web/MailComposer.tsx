"use client";

import {
  errorMessage,
  useCrmBookingLinks,
  useCrmChangeDraft,
  useCrmMailAccounts,
  useCrmRenderMail,
  useCrmSendMail,
  useCrmTemplates,
  type CrmEmailDraft,
  type CrmEmailTemplate,
  type CrmRendered,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  ConfirmDialog,
  ConnectPrompt,
  Dialog,
  Field,
  Input,
  Menu,
  MenuItem,
  MenuLabel,
  MenuSeparator,
  Select,
  Skeleton,
  Spinner,
  TextArea,
  toast,
} from "@scaffoldapp/ui-web";
import { Bot, CalendarClock, Eye, FileText, PenLine, Settings2 } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import {
  AddressInput,
  EMAIL_RE,
  MailAccountIcon,
  MailErrorBanner,
  MergeFieldMenu,
  hasMergeFields,
  insertAtCaret,
  mailAccountLabel,
  mergeKeys,
  mergeLabels,
  useCaretInsert,
} from "./MailParts";

/** A contact the email can go to: the record's own contact, or one linked to the deal or ticket. */
export interface ComposerContact {
  id: string;
  name: string;
  /** The first is primary. */
  emails: string[];
}

export interface MailComposerProps {
  orgId: string;
  /** People to write to (at least one, each with an address). */
  contacts: ComposerContact[];
  /** The deal or ticket the email is about (it shows on its timeline too). */
  dealId?: string | null;
  ticketId?: string | null;
  /** Continue a saved draft. */
  draft?: CrmEmailDraft | null;
  onClose: () => void;
}

const MAX_RECIPIENTS = 10;
const FROM_KEY = "crm.mail.from";

function readFrom(): string | null {
  try {
    return window.localStorage.getItem(FROM_KEY);
  } catch {
    return null;
  }
}

function rememberFrom(id: string) {
  try {
    window.localStorage.setItem(FROM_KEY, id);
  } catch {
    // Private windows refuse storage; the first account is the default then.
  }
}

/**
 * Write an email to a contact and send it from your own connected Gmail or Outlook account:
 * templates, merge fields (filled from the records when you send), booking links, Save draft.
 * ⌘/Ctrl+Enter sends. The sent email lands on the timeline; nothing is tracked.
 */
export function MailComposer({ orgId, contacts, dealId = null, ticketId = null, draft = null, onClose }: MailComposerProps) {
  const pathname = usePathname() ?? "/crm";
  const accounts = useCrmMailAccounts(orgId);
  const templates = useCrmTemplates(orgId);
  const booking = useCrmBookingLinks(orgId);
  const render = useCrmRenderMail();
  const changeDraft = useCrmChangeDraft(orgId);

  const initialContact = (draft ? contacts.find((c) => c.id === draft.contact.id) : undefined) ?? contacts[0];
  const [contactId, setContactId] = useState(initialContact?.id ?? "");
  const contact = contacts.find((c) => c.id === contactId) ?? initialContact;
  const send = useCrmSendMail(contact?.id ?? "");

  const [fromId, setFromId] = useState<string>("");
  const [to, setTo] = useState<string[]>(draft ? draft.to : contact?.emails[0] ? [contact.emails[0]] : []);
  const [cc, setCc] = useState<string[]>(draft?.cc ?? []);
  const [showCc, setShowCc] = useState((draft?.cc.length ?? 0) > 0);
  const [subject, setSubject] = useState(draft?.subject ?? "");
  const [body, setBody] = useState(draft?.body ?? "");
  const [draftId, setDraftId] = useState<string | null>(draft?.id ?? null);
  const [error, setError] = useState<unknown>(null);
  const [missing, setMissing] = useState<string[]>([]);
  const [preview, setPreview] = useState<CrmRendered | null>(null);
  const [pendingTemplate, setPendingTemplate] = useState<CrmEmailTemplate | null>(null);
  const [confirmClose, setConfirmClose] = useState(false);
  const initial = useRef({ subject: draft?.subject ?? "", body: draft?.body ?? "" });

  const subjectRef = useRef<HTMLInputElement>(null);
  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const toRef = useRef<HTMLInputElement>(null);
  const lastField = useRef<"subject" | "body">("body");
  const caret = useCaretInsert();

  const senders = useMemo(() => (accounts.data?.accounts ?? []).filter((a) => a.can_send), [accounts.data]);
  const sendOffers = (accounts.data?.offers ?? []).filter((o) => o.capability.includes("mail_send"));
  const from = senders.find((a) => a.connection_id === fromId) ?? null;

  // Default sender: the one used last, else the first that can send.
  useEffect(() => {
    if (fromId || senders.length === 0) return;
    const last = readFrom();
    setFromId(senders.find((a) => a.connection_id === last)?.connection_id ?? senders[0]?.connection_id ?? "");
  }, [senders, fromId]);

  const fields = templates.data?.merge_fields;
  const text = `${subject}\n${body}`;
  const stillMissing = missing.filter((k) => mergeKeys(text).includes(k));
  const dirty = subject !== initial.current.subject || body !== initial.current.body;
  const busy = send.isPending || render.isPending || changeDraft.isPending;
  const capability = from ? `crm.${from.provider}.mail_send` : null;

  const pickContact = (id: string) => {
    const next = contacts.find((c) => c.id === id);
    if (!next) return;
    const old = new Set((contact?.emails ?? []).map((e) => e.toLowerCase()));
    const kept = to.filter((e) => !old.has(e.toLowerCase()));
    setTo(next.emails[0] ? [next.emails[0], ...kept.filter((e) => e.toLowerCase() !== next.emails[0]?.toLowerCase())] : kept);
    setContactId(id);
    setPreview(null);
  };

  const insert = (snippet: string, into: "subject" | "body" = lastField.current) => {
    const el = into === "subject" ? subjectRef.current : bodyRef.current;
    const current = into === "subject" ? subject : body;
    const r = insertAtCaret(el, current, snippet);
    if (into === "subject") setSubject(r.value);
    else setBody(r.value);
    setPreview(null);
    caret.focusAfter(el, r.caret);
  };

  const renderBody = (s: string, b: string) =>
    render.mutateAsync({ contact_id: contact?.id ?? "", ...(dealId ? { deal_id: dealId } : {}), ...(ticketId ? { ticket_id: ticketId } : {}), subject: s, body: b });

  const applyTemplate = (t: CrmEmailTemplate) => {
    setPendingTemplate(null);
    setError(null);
    setPreview(null);
    renderBody(t.subject, t.body).then(
      (r) => {
        if (r.missing.length === 0) {
          setSubject(r.subject);
          setBody(r.body);
          setMissing([]);
        } else {
          // Keep the merge fields where they are so it's clear what to fill in.
          setSubject(t.subject);
          setBody(t.body);
          setMissing(r.missing);
        }
      },
      (e: unknown) => {
        setSubject(t.subject);
        setBody(t.body);
        setError(e);
      },
    );
  };

  const chooseTemplate = (t: CrmEmailTemplate) => {
    if (body.trim() && body !== t.body) setPendingTemplate(t);
    else applyTemplate(t);
  };

  const togglePreview = () => {
    if (preview) return setPreview(null);
    setError(null);
    renderBody(subject, body).then(setPreview, setError);
  };

  const problem = (): string | null => {
    const all = [...to, ...cc];
    if (to.length === 0) return "Add who it's to.";
    if (all.some((e) => !EMAIL_RE.test(e))) return "Fix the addresses in red.";
    if (all.length > MAX_RECIPIENTS) return `Send to at most ${MAX_RECIPIENTS} people at once.`;
    const mine = new Set((contact?.emails ?? []).map((e) => e.toLowerCase()));
    if (!to.some((e) => mine.has(e.toLowerCase()))) return `Send it to one of ${contact?.name ?? "the contact"}'s email addresses (others can be added too).`;
    if (!subject.trim()) return "Add a subject.";
    if (!body.trim()) return "Write the message first.";
    return null;
  };

  const doSend = async () => {
    if (busy) return;
    setError(null);
    if (!from) return setError("Choose an account to send from.");
    const p = problem();
    if (p) return setError(p);
    let s = subject;
    let b = body;
    if (hasMergeFields(text)) {
      let r: CrmRendered;
      try {
        r = await renderBody(subject, body);
      } catch (e) {
        return setError(e);
      }
      if (r.missing.length > 0) {
        setMissing(r.missing);
        return setError(`No value for ${mergeLabels(r.missing, fields)}. Replace those merge fields or add a fallback, like {{contact.first_name | there}}.`);
      }
      s = r.subject;
      b = r.body;
    }
    rememberFrom(from.connection_id);
    send.mutate(
      {
        connection_id: from.connection_id,
        to,
        ...(cc.length ? { cc } : {}),
        subject: s,
        body: b,
        ...(dealId ? { deal_id: dealId } : {}),
        ...(ticketId ? { ticket_id: ticketId } : {}),
        ...(draftId ? { draft_id: draftId } : {}),
      },
      {
        onSuccess: () => {
          toast.success("Email sent");
          onClose();
        },
        onError: setError,
      },
    );
  };

  const saveDraft = () => {
    if (busy || !contact) return;
    setError(null);
    if (!subject.trim() && !body.trim()) return setError("Write a subject or a message first.");
    const req = { to, cc, subject, body, deal_id: dealId, ticket_id: ticketId };
    changeDraft.mutate(draftId ? { op: "update", id: draftId, body: req } : { op: "create", contactId: contact.id, body: req }, {
      onSuccess: (d) => {
        if (d) setDraftId(d.id);
        toast.success("Draft saved");
        onClose();
      },
      onError: setError,
    });
  };

  const requestClose = () => {
    if (dirty && !busy) setConfirmClose(true);
    else onClose();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      void doSend();
    }
  };

  const mine = (templates.data?.items ?? []).filter((t) => !t.shared);
  const shared = (templates.data?.items ?? []).filter((t) => t.shared);
  const links = booking.data?.available ? booking.data.items : [];

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && requestClose()}
      size="lg"
      title={draftId ? "Draft" : "New email"}
      info="Sent from your own mailbox and kept on the timeline. Opens and clicks aren't tracked."
      footer={
        <>
          <Button variant="secondary" onClick={saveDraft} loading={changeDraft.isPending} disabled={busy && !changeDraft.isPending}>
            Save draft
          </Button>
          <Button onClick={() => void doSend()} loading={send.isPending || (render.isPending && !preview)} disabled={!from || (busy && !send.isPending)}>
            Send
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4" onKeyDown={onKeyDown}>
        {draft?.agent ? (
          <Badge tone="info" icon={<Bot />} className="self-start">
            Drafted by {draft.agent}
          </Badge>
        ) : null}

        {accounts.isPending ? (
          <Field label="From">
            <Skeleton className="h-control w-full" />
          </Field>
        ) : accounts.isError ? (
          <MailErrorBanner error={accounts.error} capability={null} />
        ) : senders.length === 0 ? (
          <div className="flex flex-col gap-2">
            <Banner tone="warning">No connected mailbox can send. Save a draft, or connect Gmail or Outlook to send from here.</Banner>
            <ConnectPrompt offers={sendOffers} returnTo={pathname} linkAs={Link} title="Send from" />
          </div>
        ) : (
          <Field label="From">
            <Select
              value={fromId}
              onValueChange={(v) => {
                setFromId(v);
                setError(null);
              }}
              options={senders.map((a) => ({ value: a.connection_id, label: mailAccountLabel(a), icon: <MailAccountIcon provider={a.provider} /> }))}
            />
          </Field>
        )}

        {contacts.length > 1 ? (
          <Field label="Contact" info="The email goes on this contact's timeline, and their details fill the merge fields.">
            <Select value={contact?.id} onValueChange={pickContact} options={contacts.map((c) => ({ value: c.id, label: `${c.name} · ${c.emails[0] ?? ""}` }))} />
          </Field>
        ) : null}

        <Field
          label="To"
          labelEnd={
            !showCc ? (
              <Button variant="ghost" size="sm" onClick={() => setShowCc(true)}>
                Cc
              </Button>
            ) : undefined
          }
        >
          <AddressInput value={to} onChange={(v) => (setTo(v), setError(null))} inputRef={toRef} placeholder="name@example.com" />
        </Field>
        {showCc ? (
          <Field label="Cc">
            <AddressInput value={cc} onChange={(v) => (setCc(v), setError(null))} placeholder="name@example.com" />
          </Field>
        ) : null}

        <div className="flex flex-wrap items-center gap-1">
          <Menu
            align="start"
            className="max-h-80 overflow-y-auto"
            trigger={
              <Button variant="ghost" size="sm" leftIcon={<FileText />} disabled={templates.isPending || render.isPending}>
                Template
              </Button>
            }
          >
            {templates.isError ? <MenuLabel>Couldn&apos;t load templates</MenuLabel> : null}
            {mine.length ? <MenuLabel>Yours</MenuLabel> : null}
            {mine.map((t) => (
              <MenuItem key={t.id} onSelect={() => chooseTemplate(t)}>
                {t.name}
              </MenuItem>
            ))}
            {shared.length ? <MenuLabel>Shared</MenuLabel> : null}
            {shared.map((t) => (
              <MenuItem key={t.id} onSelect={() => chooseTemplate(t)}>
                {t.name}
              </MenuItem>
            ))}
            {templates.data && mine.length + shared.length === 0 ? <MenuLabel>No templates yet</MenuLabel> : null}
            <MenuSeparator />
            <MenuItem asChild icon={<Settings2 />}>
              <Link href="/crm/templates">Manage templates</Link>
            </MenuItem>
          </Menu>
          <MergeFieldMenu fields={fields} onInsert={(k) => insert(`{{${k}}}`)} onTriggerFocus={caret.onTriggerFocus} disabled={!!preview} />
          {links.length === 1 && links[0] ? (
            <Button variant="ghost" size="sm" leftIcon={<CalendarClock />} disabled={!!preview} onClick={() => insert(links[0]?.url ?? "", "body")}>
              Booking link
            </Button>
          ) : links.length > 1 ? (
            <Menu
              align="start"
              trigger={
                <Button variant="ghost" size="sm" leftIcon={<CalendarClock />} disabled={!!preview} onFocus={caret.onTriggerFocus}>
                  Booking link
                </Button>
              }
            >
              {links.map((l) => (
                <MenuItem key={l.url} onSelect={() => insert(l.url, "body")}>
                  {l.title}
                </MenuItem>
              ))}
            </Menu>
          ) : null}
          <Button variant="ghost" size="sm" className="ml-auto" leftIcon={preview ? <PenLine /> : <Eye />} onClick={togglePreview} loading={render.isPending && !preview} aria-pressed={!!preview}>
            {preview ? "Edit" : "Preview"}
          </Button>
        </div>

        {stillMissing.length > 0 ? (
          <Banner tone="warning">No value for {mergeLabels(stillMissing, fields)}. Edit those parts before sending.</Banner>
        ) : null}

        {preview ? (
          <section aria-label="Preview" className="flex flex-col gap-3 rounded-lg bg-sunken p-4">
            {preview.missing.length > 0 ? <Banner tone="warning">No value for {mergeLabels(preview.missing, fields)}: those parts come out empty.</Banner> : null}
            <p className="type-label font-semibold break-words text-fg">{preview.subject || "(no subject)"}</p>
            <p className="type-body break-words whitespace-pre-wrap text-fg">{preview.body}</p>
          </section>
        ) : (
          <>
            <Field label="Subject">
              <Input
                ref={subjectRef}
                value={subject}
                maxLength={200}
                onFocus={() => (lastField.current = "subject")}
                onChange={(e) => {
                  setSubject(e.target.value);
                  setError(null);
                }}
              />
            </Field>
            <Field label="Message" info="Plain text. Merge fields like {{contact.first_name}} fill in from the records when you send; {{contact.first_name | there}} uses “there” when it's empty.">
              <TextArea
                ref={bodyRef}
                value={body}
                minRows={8}
                maxRows={18}
                maxLength={20000}
                onFocus={() => (lastField.current = "body")}
                onChange={(e) => {
                  setBody(e.target.value);
                  setError(null);
                }}
              />
            </Field>
          </>
        )}

        {error ? <MailErrorBanner error={error} capability={capability} /> : null}
        {render.isPending && !preview ? (
          <span className="sr-only" role="status">
            <Spinner size={16} label="Filling in merge fields" />
          </span>
        ) : null}
      </div>

      <ConfirmDialog
        open={pendingTemplate !== null}
        onOpenChange={(o) => !o && setPendingTemplate(null)}
        title={`Use “${pendingTemplate?.name ?? ""}”?`}
        confirmLabel="Replace message"
        onConfirm={() => {
          if (pendingTemplate) applyTemplate(pendingTemplate);
        }}
      >
        The subject and message you wrote are replaced.
      </ConfirmDialog>
      <ConfirmDialog
        open={confirmClose}
        onOpenChange={setConfirmClose}
        tone="danger"
        title="Discard this email?"
        confirmLabel="Discard"
        cancelLabel="Keep writing"
        onConfirm={() => {
          setConfirmClose(false);
          onClose();
        }}
      >
        {draftId ? "Changes since you saved the draft are lost." : "What you wrote is lost. Save a draft to keep it."}
      </ConfirmDialog>
    </Dialog>
  );
}

/** The message for an error from the composer's hooks (used in toasts). */
export function mailErrorText(e: unknown, fallback: string): string {
  return errorMessage(e, fallback);
}
