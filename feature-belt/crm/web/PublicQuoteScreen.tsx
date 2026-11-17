"use client";

import { errorMessage, isApiError, useCrmDecideQuote, useCrmPublicQuote, type CrmPublicQuote } from "@scaffoldapp/shared";
import { Banner, Button, Card, Checkbox, EmptyState, ErrorState, Field, Input, Skeleton, SkeletonGroup, TextArea } from "@scaffoldapp/ui-web";
import { CalendarX2, CheckCircle2, LinkIcon, Printer, XCircle } from "lucide-react";
import { useRef, useState } from "react";
import { formatDate, formatDateTime } from "@/lib/format";
import { QuoteDocument, QuotePrintStyles, QuoteStatusBadge, usePrintInLight } from "./QuoteDocument";

/** /q/[token]: the buyer's quote — read, print, accept with a typed name, or decline. No account needed. */
export function CrmPublicQuoteScreen({ token }: { token: string }) {
  const quote = useCrmPublicQuote(token);
  if (quote.isPending) {
    return (
      <Shell>
        <SkeletonGroup label="Loading the quote" className="flex flex-col gap-6">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-96 w-full" />
        </SkeletonGroup>
      </Shell>
    );
  }
  if (quote.isError) {
    const e = quote.error;
    if (isApiError(e) && e.code === "quote_expired") {
      const d = (e.details ?? {}) as { number?: string; title?: string; seller_name?: string; sender_name?: string; sender_email?: string; expires_at?: string };
      return (
        <Shell>
          <Card padding="card">
            <EmptyState
              icon={<CalendarX2 />}
              title={`Quote ${d.number ?? ""} expired`}
              info={`“${d.title ?? ""}” from ${d.seller_name ?? ""} was valid until ${d.expires_at ? formatDate(d.expires_at) : "an earlier date"}.`}
              action={
                d.sender_email ? (
                  <Button asChild size="auto" variant="secondary">
                    <a href={`mailto:${d.sender_email}?subject=${encodeURIComponent(`Quote ${d.number ?? ""}`)}`}>Ask {d.sender_name || "the sender"} for a new one</a>
                  </Button>
                ) : undefined
              }
            />
          </Card>
        </Shell>
      );
    }
    if (isApiError(e) && e.status === 404) {
      return (
        <Shell>
          <Card padding="card">
            <EmptyState icon={<LinkIcon />} title="This link doesn't work anymore" info="The quote was taken back, replaced or deleted. Ask the person who sent it for a new link." />
          </Card>
        </Shell>
      );
    }
    return (
      <Shell>
        <ErrorState title="Couldn't load the quote" info={e.message} onRetry={() => void quote.refetch()} retrying={quote.isRefetching} />
      </Shell>
    );
  }
  return <QuotePage token={token} q={quote.data} />;
}

function Shell({ children }: { children: React.ReactNode }) {
  return <main className="mx-auto flex w-full max-w-4xl flex-col gap-6 px-gutter py-section print:max-w-none print:p-0">{children}</main>;
}

function QuotePage({ token, q }: { token: string; q: CrmPublicQuote }) {
  const docRef = useRef<HTMLDivElement>(null);
  usePrintInLight(docRef);
  return (
    <Shell>
      <QuotePrintStyles />
      <div className="flex flex-wrap items-center gap-3 print:hidden">
        <QuoteStatusBadge status={q.status} size="md" />
        <span className="type-label text-fg-muted">Valid until {formatDate(q.expires_at)}</span>
        <Button variant="secondary" size="sm" leftIcon={<Printer />} className="ml-auto" onClick={() => window.print()}>
          Print
        </Button>
      </div>
      <Card padding="card" ref={docRef} data-print-root className="print:border-0 print:shadow-none">
        <QuoteDocument quote={q} />
        {q.status !== "published" ? (
          <p className="mt-8 border-t border-border pt-4 type-label text-fg-muted">
            {q.status === "accepted" ? "Accepted" : "Declined"} by {q.signer_name}
            {q.decided_at ? ` on ${formatDateTime(q.decided_at)}` : ""}.
          </p>
        ) : null}
      </Card>
      {q.status === "published" ? <Decision token={token} q={q} /> : <Decided q={q} />}
    </Shell>
  );
}

function Decided({ q }: { q: CrmPublicQuote }) {
  const accepted = q.status === "accepted";
  return (
    <Card padding="card" className="print:hidden">
      <EmptyState
        tone="primary"
        icon={accepted ? <CheckCircle2 /> : <XCircle />}
        title={accepted ? `You accepted quote ${q.number}` : `You declined quote ${q.number}`}
        info={`${q.sender_name || "The sender"} at ${q.seller_name} has been told${accepted ? " and will be in touch about next steps" : ""}.${q.decline_reason ? ` Your reason: “${q.decline_reason}”.` : ""}`}
      />
    </Card>
  );
}

function Decision({ token, q }: { token: string; q: CrmPublicQuote }) {
  const decide = useCrmDecideQuote(token);
  const [mode, setMode] = useState<"accept" | "decline">("accept");
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [agree, setAgree] = useState(false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    if (name.trim().length < 2) return setError("Type your full name.");
    if (mode === "accept" && !agree) return setError("Tick the box to accept the quote.");
    setError(null);
    decide.mutate(
      { accept: mode === "accept", body: { name: name.trim(), ...(email.trim() ? { email: email.trim() } : {}), ...(mode === "decline" && reason.trim() ? { reason: reason.trim() } : {}) } },
      { onError: (e) => setError(errorMessage(e, "That didn't go through. Try again.")) },
    );
  };
  return (
    <Card padding="card" className="flex flex-col gap-4 print:hidden">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="flex-1 type-title text-fg">{mode === "accept" ? "Accept this quote" : "Decline this quote"}</h2>
        <Button variant="ghost" size="sm" onClick={() => setMode(mode === "accept" ? "decline" : "accept")}>
          {mode === "accept" ? "Decline instead" : "Accept instead"}
        </Button>
      </div>
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Your full name" required info={mode === "accept" ? "Typing your name signs the acceptance. The time and your network address are recorded with it." : undefined}>
            <Input value={name} maxLength={200} autoComplete="name" onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="Your email">
            <Input type="email" value={email} maxLength={254} autoComplete="email" onChange={(e) => setEmail(e.target.value)} />
          </Field>
        </div>
        {mode === "accept" ? (
          <Checkbox
            checked={agree}
            onCheckedChange={(v) => setAgree(v === true)}
            label={`I accept quote ${q.number} from ${q.seller_name} on these terms`}
          />
        ) : (
          <Field label="Reason">
            <TextArea value={reason} maxLength={2000} onChange={(e) => setReason(e.target.value)} placeholder="Optional: what would change your mind" />
          </Field>
        )}
        {error ? <Banner tone="danger">{error}</Banner> : null}
        <div className="flex justify-end">
          <Button type="submit" loading={decide.isPending} variant={mode === "accept" ? "primary" : "danger"}>
            {mode === "accept" ? "Accept quote" : "Decline quote"}
          </Button>
        </div>
      </form>
    </Card>
  );
}
