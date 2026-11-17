"use client";

import {
  errorMessage,
  useCrmChangeQuote,
  useCrmDeleteQuote,
  useCrmQuote,
  useTopic,
  topic,
  type CrmQuote,
  type CrmQuoteItem,
  type CrmSearchHit,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  Menu,
  MenuItem,
  MenuSeparator,
  Skeleton,
  SkeletonGroup,
  TextArea,
  toast,
} from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowLeft, ArrowUp, Copy, ExternalLink, FileX, MoreHorizontal, Package, PenLine, Plus, Printer, RefreshCw, Send, Trash2, Undo2, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { formatDate, formatDateTime } from "@/lib/format";
import { CrmFrame, useCrm } from "./CrmFrame";
import { LineEditorDialog, LineRow, ProductPickerDialog, emptyLine, lineFromProduct, lineRequest, type LineDraft } from "./DealCommerce";
import { QuoteDocument, QuotePrintStyles, QuoteStatusBadge, TotalsList, usePrintInLight } from "./QuoteDocument";
import { RecordPicker, recordHref } from "./parts";

/** /crm/quotes/[id]: edit a draft, publish it, follow it (views, the buyer's decision), print it. */
export function CrmQuoteScreen({ id }: { id: string }) {
  return (
    <CrmFrame>
      <QuoteView id={id} />
    </CrmFrame>
  );
}

function QuoteView({ id }: { id: string }) {
  const quote = useCrmQuote(id);
  useTopic(quote.data ? topic("crm", quote.data.deal.id) : null);
  if (quote.isPending) {
    return (
      <SkeletonGroup label="Loading the quote" className="flex flex-col gap-6">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-96 w-full" />
      </SkeletonGroup>
    );
  }
  if (quote.isError) {
    return quote.error.status === 404 ? (
      <Card padding="compact">
        <EmptyState icon={<FileX />} title="Quote not found" info="It was deleted, or you can't open its deal." />
      </Card>
    ) : (
      <ErrorState title="Couldn't load the quote" info={quote.error.message} onRetry={() => void quote.refetch()} retrying={quote.isRefetching} />
    );
  }
  return <QuoteBody q={quote.data} />;
}

function QuoteBody({ q }: { q: CrmQuote }) {
  const { orgId } = useCrm();
  const router = useRouter();
  const change = useCrmChangeQuote(q.id);
  const del = useCrmDeleteQuote();
  const [publishing, setPublishing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const docRef = useRef<HTMLDivElement>(null);
  usePrintInLight(docRef);
  const editable = q.my_level === "edit" || q.my_level === "manage";
  const draft = q.status === "draft";
  const fail = (fallback: string) => (e: unknown) => toast.error(errorMessage(e, fallback));

  return (
    <div className="flex flex-col gap-6">
      <QuotePrintStyles />
      <Card padding="compact" className="flex flex-col gap-4 print:hidden">
        <div className="flex items-start gap-3">
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <Link href={recordHref("deal", q.deal.id)} className="inline-flex w-fit items-center gap-1 type-label text-fg-muted hover:text-fg hover:underline">
              <ArrowLeft className="size-4" aria-hidden />
              {q.deal.name}
            </Link>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <h2 className="type-headline break-words text-fg">
                {q.number} · {q.title}
              </h2>
              <QuoteStatusBadge status={q.status} expired={q.expired} size="md" />
              {q.agent ? <Badge>Drafted by {q.agent}</Badge> : null}
            </div>
          </div>
          <ResourceMenu
            feature="crm"
            kind="crm.quote"
            id={q.id}
            title={`Quote ${q.number}`}
            orgId={orgId}
            actions={[
              { id: "crm-quote-print", label: "Print", icon: Printer, onSelect: () => window.print() },
              ...(editable && q.status === "published"
                ? [{ id: "crm-quote-unpublish", label: "Back to draft", icon: Undo2, onSelect: () => change.mutate({ op: "unpublish" }, { onSuccess: () => toast.success("Back to a draft: the link stopped working"), onError: fail("Couldn't take it back.") }) }]
                : []),
              ...(editable ? [{ id: "crm-quote-delete", label: "Delete quote", icon: Trash2, destructive: true, onSelect: () => setDeleting(true) }] : []),
            ]}
          />
        </div>
        {draft && editable ? (
          <div className="flex flex-wrap items-center gap-3">
            {q.can_publish ? (
              <Button leftIcon={<Send />} onClick={() => setPublishing(true)} disabled={q.items.length === 0}>
                Publish
              </Button>
            ) : (
              <Banner tone="info">Publishing needs the “Publish CRM quotes” and “Share with a public link” permissions. Ask an admin, or a colleague who has them.</Banner>
            )}
            <Button variant="secondary" leftIcon={<RefreshCw />} loading={change.isPending} onClick={() => change.mutate({ op: "refresh" }, { onSuccess: () => toast.success("Lines copied from the deal again"), onError: fail("Couldn't copy the lines.") })}>
              Copy lines from the deal
            </Button>
          </div>
        ) : null}
        {!draft ? <PublishedFacts q={q} /> : null}
      </Card>

      {draft && editable ? <DraftEditor q={q} /> : null}

      <Card padding="card" className="mx-auto w-full max-w-4xl print:border-0 print:shadow-none" ref={docRef} data-print-root>
        <QuoteDocument quote={q} />
      </Card>

      <ConfirmDialog
        open={publishing}
        onOpenChange={setPublishing}
        title={`Publish ${q.number}?`}
        confirmLabel="Publish"
        onConfirm={() =>
          change.mutateAsync({ op: "publish" }).then(
            () => toast.success("Published: copy the link to send it"),
            (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't publish it."))),
          )
        }
      >
        It freezes the quote and makes a link anyone who has it can open, accept or decline until {formatDate(q.expires_at)}. Take it back to a draft to stop the link.
      </ConfirmDialog>
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        tone="danger"
        title={`Delete ${q.number}?`}
        confirmLabel="Delete quote"
        onConfirm={() =>
          del.mutateAsync({ id: q.id, dealId: q.deal.id }).then(
            () => {
              toast.success("Quote deleted");
              router.push(recordHref("deal", q.deal.id));
            },
            (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete it."))),
          )
        }
      >
        {q.status === "draft" ? "The draft goes; the deal's line items stay." : "Its link stops working and the buyer can't open it any more."} It can&apos;t be undone.
      </ConfirmDialog>
    </div>
  );
}

/** The link, views and the buyer's decision. */
function PublishedFacts({ q }: { q: CrmQuote }) {
  const copy = () => {
    if (!q.url) return;
    void navigator.clipboard?.writeText(q.url).then(() => toast.success("Link copied"));
  };
  return (
    <div className="flex flex-col gap-3">
      {q.url ? (
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <Input aria-label="Quote link" readOnly value={q.url} onFocus={(e) => e.target.select()} wrapperClassName="flex-1" />
          <div className="flex gap-2">
            <Button variant="secondary" leftIcon={<Copy />} onClick={copy}>
              Copy link
            </Button>
            <Button asChild variant="secondary" leftIcon={<ExternalLink />}>
              <a href={q.url} target="_blank" rel="noreferrer noopener">
                Open
              </a>
            </Button>
          </div>
        </div>
      ) : null}
      {q.expired ? <Banner tone="warning">This quote expired on {formatDate(q.expires_at)}. Take it back to a draft to change the date and publish again.</Banner> : null}
      <dl className="grid gap-x-8 gap-y-2 type-label sm:grid-cols-2 lg:grid-cols-4">
        <Fact label="Published" value={q.published_at ? formatDateTime(q.published_at) : "—"} />
        <Fact label="Views" value={q.view_count === 0 ? "Not opened yet" : `${q.view_count} · last ${q.last_viewed_at ? formatDateTime(q.last_viewed_at) : ""}`} />
        <Fact label="Valid until" value={formatDate(q.expires_at)} />
        {q.decided_at ? <Fact label={q.status === "accepted" ? "Accepted" : "Declined"} value={formatDateTime(q.decided_at)} /> : null}
      </dl>
      {q.signer_name ? (
        <Banner tone={q.status === "accepted" ? "success" : "warning"}>
          {q.status === "accepted" ? "Accepted" : "Declined"} by {q.signer_name}
          {q.signer_email ? ` (${q.signer_email})` : ""}
          {q.signer_ip ? ` from ${q.signer_ip}` : ""}
          {q.decline_reason ? `: “${q.decline_reason}”` : ""}
        </Banner>
      ) : null}
    </div>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col">
      <dt className="type-caption text-fg-muted">{label}</dt>
      <dd className="tabular-nums text-fg">{value}</dd>
    </div>
  );
}

/** A day as the end of that local day, in ISO. */
function endOfDay(day: string): string {
  const [y, m, d] = day.split("-").map(Number);
  return new Date(y ?? 1970, (m ?? 1) - 1, d ?? 1, 23, 59, 0).toISOString();
}

function localDay(iso: string): string {
  const d = new Date(iso);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

function toDraft(it: CrmQuoteItem): LineDraft {
  return { ...it };
}

function DraftEditor({ q }: { q: CrmQuote }) {
  const { orgId } = useCrm();
  const change = useCrmChangeQuote(q.id);
  const [title, setTitle] = useState(q.title);
  const [expires, setExpires] = useState(localDay(q.expires_at));
  const [comments, setComments] = useState(q.comments);
  const [terms, setTerms] = useState(q.terms);
  const [picking, setPicking] = useState(false);
  const [editing, setEditing] = useState<{ index: number | null; line: LineDraft; title: string } | null>(null);
  const [pickingContact, setPickingContact] = useState(false);
  useEffect(() => {
    setTitle(q.title);
    setExpires(localDay(q.expires_at));
    setComments(q.comments);
    setTerms(q.terms);
  }, [q.title, q.expires_at, q.comments, q.terms]);
  const dirty = title !== q.title || expires !== localDay(q.expires_at) || comments !== q.comments || terms !== q.terms;
  const fail = (fallback: string) => (e: unknown) => toast.error(errorMessage(e, fallback));
  const lines = q.items.map(toDraft);
  const saveLines = (next: LineDraft[], done?: () => void) =>
    change.mutate({ op: "update", body: { items: next.map((l) => lineRequest(l, false)) } }, { onSuccess: () => done?.(), onError: fail("Couldn't save the lines.") });
  const saveFields = () =>
    change.mutate(
      { op: "update", body: { title: title.trim(), expires_at: endOfDay(expires), comments, terms } },
      { onSuccess: () => toast.success("Quote saved"), onError: fail("Couldn't save the quote.") },
    );
  const pickContact = (h: CrmSearchHit) => {
    setPickingContact(false);
    change.mutate({ op: "update", body: { contact_id: h.id } }, { onError: fail("Couldn't change the buyer.") });
  };
  return (
    <div className="grid items-start gap-6 lg:grid-cols-2 print:hidden">
      <Card padding="compact" className="flex flex-col gap-4">
        <h2 className="type-title-sm text-fg">Details</h2>
        <Field label="Title" required>
          <Input value={title} maxLength={200} onChange={(e) => setTitle(e.target.value)} />
        </Field>
        <Field label="Valid until" info="The buyer can accept until the end of this day. After it, the link says the quote expired.">
          <Input type="date" value={expires} onChange={(e) => setExpires(e.target.value)} />
        </Field>
        <Field label="Prepared for">
          {q.contact ? (
            <div className="flex min-h-control items-center gap-2 rounded-md border border-border pl-3 pr-1">
              <Link href={recordHref("contact", q.contact.id)} className="min-w-0 flex-1 truncate type-label text-fg hover:underline">
                {q.contact.name}
              </Link>
              <IconButton label="Remove the buyer" size="sm" icon={<X />} onClick={() => change.mutate({ op: "update", body: { contact_id: null } }, { onError: fail("Couldn't change the buyer.") })} />
            </div>
          ) : pickingContact ? (
            <RecordPicker orgId={orgId} types={["contact"]} label="Find a contact" autoFocus onPick={pickContact} />
          ) : (
            <Button variant="secondary" size="sm" onClick={() => setPickingContact(true)}>
              Choose a contact
            </Button>
          )}
        </Field>
        <Field label="Comments" info="A note to the buyer, shown above the lines.">
          <TextArea value={comments} maxLength={10000} onChange={(e) => setComments(e.target.value)} />
        </Field>
        <Field label="Terms">
          <TextArea value={terms} maxLength={10000} onChange={(e) => setTerms(e.target.value)} />
        </Field>
        <div className="flex justify-end">
          <Button disabled={!dirty || !title.trim()} loading={change.isPending && dirty} onClick={saveFields}>
            Save
          </Button>
        </div>
      </Card>
      <Card padding="compact" className="flex flex-col gap-3">
        <div className="flex items-center gap-2">
          <h2 className="flex-1 type-title-sm text-fg">Lines</h2>
          <Menu trigger={<Button variant="ghost" size="sm" leftIcon={<Plus />}>Add</Button>}>
            <MenuItem icon={<Package />} onSelect={() => setPicking(true)}>
              From products…
            </MenuItem>
            <MenuItem icon={<PenLine />} onSelect={() => setEditing({ index: null, line: emptyLine, title: "New line" })}>
              Custom line
            </MenuItem>
          </Menu>
        </div>
        {lines.length === 0 ? (
          <p className="type-label text-fg-subtle">No lines: add one, or copy the deal's line items again.</p>
        ) : (
          <ul className="flex flex-col divide-y divide-border">
            {q.items.map((it, i) => (
              <LineRow
                key={it.id}
                line={it}
                currency={q.currency}
                actions={
                  <Menu trigger={<IconButton label={`Actions for ${it.name}`} size="sm" icon={<MoreHorizontal />} />}>
                    <MenuItem icon={<PenLine />} onSelect={() => setEditing({ index: i, line: toDraft(it), title: "Edit line" })}>
                      Edit
                    </MenuItem>
                    {i > 0 ? (
                      <MenuItem icon={<ArrowUp />} onSelect={() => saveLines(swap(lines, i, i - 1))}>
                        Move up
                      </MenuItem>
                    ) : null}
                    {i < lines.length - 1 ? (
                      <MenuItem icon={<ArrowDown />} onSelect={() => saveLines(swap(lines, i, i + 1))}>
                        Move down
                      </MenuItem>
                    ) : null}
                    <MenuSeparator />
                    <MenuItem icon={<Trash2 />} destructive onSelect={() => saveLines(lines.filter((_, j) => j !== i))}>
                      Remove
                    </MenuItem>
                  </Menu>
                }
              />
            ))}
          </ul>
        )}
        {lines.length > 0 ? <TotalsList totals={q.totals} currency={q.currency} /> : null}
      </Card>
      {picking ? (
        <ProductPickerDialog
          orgId={orgId}
          currency={q.currency}
          onClose={() => setPicking(false)}
          onPick={(p) => {
            setPicking(false);
            setEditing({ index: null, line: lineFromProduct(p), title: `Add ${p.name}` });
          }}
        />
      ) : null}
      {editing ? (
        <LineEditorDialog
          title={editing.title}
          currency={q.currency}
          initial={editing.line}
          saving={change.isPending}
          onClose={() => setEditing(null)}
          onSave={(l) => {
            const next = editing.index === null ? [...lines, l] : lines.map((x, j) => (j === editing.index ? l : x));
            saveLines(next, () => setEditing(null));
          }}
        />
      ) : null}
    </div>
  );
}

function swap<T>(list: T[], a: number, b: number): T[] {
  const out = [...list];
  const x = out[a];
  const y = out[b];
  if (x === undefined || y === undefined) return out;
  out[a] = y;
  out[b] = x;
  return out;
}
