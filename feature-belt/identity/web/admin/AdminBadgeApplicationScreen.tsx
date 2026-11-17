"use client";

import {
  errorMessage,
  useDecideIdentityBadgeApplication,
  useIdentityAdminBadgeApplication,
  useMe,
  type IdentityAdminBadgeApplicationDetail,
  type IdentityBadgeDecision,
  type IdentityEvidenceKind,
} from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, Dialog, ErrorState, Field, Input, ListGroup, ListRow, Select, Skeleton, SkeletonGroup, TextArea, toast } from "@scaffoldapp/ui-web";
import { Check, ChevronLeft, ExternalLink, MessageCircleQuestion, X } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDate, formatDateTime } from "@/lib/format";
import { IdentityBadgeMark, identityBadgeName } from "../BadgeMark";
import { APPLICATION_STATUS } from "./AdminBadgeApplicationsScreen";

const EVIDENCE_LABEL: Record<IdentityEvidenceKind, string> = { url: "Link", domain: "Domain", account: "Account", text: "Note" };

type Kind = IdentityBadgeDecision["decision"];

const COPY: Record<Kind, { title: string; button: string; done: string }> = {
  approve: { title: "Approve this application?", button: "Approve", done: "Approved: the badge is live" },
  needs_info: { title: "Ask for more?", button: "Ask for more", done: "Asked the applicant for more" },
  reject: { title: "Reject this application?", button: "Reject", done: "Rejected" },
};

function DecisionDialog({ a, decision }: { a: IdentityAdminBadgeApplicationDetail; decision: Kind }) {
  const decide = useDecideIdentityBadgeApplication();
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<"org" | "notable">(a.category_kind || "org");
  const [label, setLabel] = useState(a.display_label);
  const [days, setDays] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);
  const approve = decision === "approve";
  const submit = () => {
    setError(null);
    const n = days.trim() ? Number(days) : undefined;
    if (n !== undefined && (!Number.isInteger(n) || n < 1 || n > 730)) {
      setError("Expiry is 1 to 730 days, or empty for the default.");
      return;
    }
    if (!approve && note.trim().length < 3) {
      setError("Say why in the note. The applicant sees it.");
      return;
    }
    decide.mutate(
      { id: a.id, decision: { decision, ...(approve ? { kind, label: label.trim(), expires_in_days: n } : {}), note: note.trim() } },
      {
        onSuccess: () => {
          setOpen(false);
          toast.success(COPY[decision].done);
        },
        onError: (e) => setError(errorMessage(e, "Couldn't save the decision. Try again.")),
      },
    );
  };
  const icon = approve ? <Check /> : decision === "reject" ? <X /> : <MessageCircleQuestion />;
  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      title={COPY[decision].title}
      trigger={
        <Button size="auto" variant={approve ? "primary" : decision === "reject" ? "danger-ghost" : "secondary"} leftIcon={icon}>
          {COPY[decision].button}
        </Button>
      }
      footer={
        <>
          <Button variant="ghost" size="auto" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button variant={decision === "reject" ? "danger" : "primary"} size="auto" loading={decide.isPending} onClick={submit}>
            {COPY[decision].button}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {approve ? (
          <>
            <Field label="Badge">
              <Select
                options={[
                  { value: "org", label: "Organization" },
                  { value: "notable", label: "Notable" },
                ]}
                value={kind}
                onValueChange={(v) => setKind(v as "org" | "notable")}
              />
            </Field>
            <Field label="Label" info="What the badge says next to their name.">
              <Input value={label} maxLength={60} onChange={(e) => setLabel(e.target.value)} />
            </Field>
            <Field label="Expires in days" info="Empty uses the default (IDENTITY_BADGE_TTL_DAYS). They apply again after it expires.">
              <Input inputMode="numeric" value={days} placeholder="365" onChange={(e) => setDays(e.target.value.replace(/\D/g, ""))} />
            </Field>
          </>
        ) : null}
        <Field label={approve ? "Note (optional)" : "Note"} required={!approve} info="The applicant sees it.">
          <TextArea value={note} maxLength={1000} minRows={3} onChange={(e) => setNote(e.target.value)} />
        </Field>
        {error ? <Banner tone="danger">{error}</Banner> : null}
      </div>
    </Dialog>
  );
}

/** Admin → Verification → one badge application: the applicant, evidence and the decision. */
export function AdminBadgeApplicationScreen({ id }: { id: string }) {
  const me = useMe();
  const q = useIdentityAdminBadgeApplication(id);
  const a = q.data;
  const self = !!a && a.user.id === me.data?.id;
  const open = a?.status === "submitted" || a?.status === "needs_info";
  return (
    <div className="flex flex-col gap-2">
      <Link href="/admin/identity/applications" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
        <ChevronLeft className="size-4" aria-hidden />
        Badge applications
      </Link>
      {q.isPending ? (
        <SkeletonGroup label="Loading application" className="flex flex-col gap-section">
          <Skeleton className="h-8 w-56" />
          <Skeleton className="h-32" />
        </SkeletonGroup>
      ) : q.isError ? (
        <ErrorState title="Couldn't load this application" info={errorMessage(q.error)} onRetry={() => void q.refetch()} retrying={q.isFetching} />
      ) : a ? (
        <div className="flex flex-col gap-section">
          <PageHeader title={a.display_label} action={<Badge tone={APPLICATION_STATUS[a.status].tone}>{APPLICATION_STATUS[a.status].label}</Badge>} />
          {self ? <Banner tone="info">Another staff member has to review your own application.</Banner> : null}
          {a.reviewer_note ? <Banner tone={a.status === "rejected" ? "danger" : "neutral"}>{a.reviewer_note}</Banner> : null}

          <Section title="Applicant">
            <Card>
              <dl className="grid gap-x-6 gap-y-2 type-label sm:grid-cols-[10rem_minmax(0,1fr)]">
                {(
                  [
                    ["Name", a.user.display_name || "—"],
                    ["Email", a.user.email],
                    ["Identity", a.identity_verified ? "Verified" : "Not verified"],
                    ["Category", a.category],
                    ["Sent", formatDateTime(a.submitted_at)],
                    ["Decided", a.decided_at ? formatDateTime(a.decided_at) : "—"],
                  ] as const
                ).map(([k, v]) => (
                  <div key={k} className="contents">
                    <dt className="text-fg-muted">{k}</dt>
                    <dd className="break-words text-fg">{v}</dd>
                  </div>
                ))}
              </dl>
            </Card>
          </Section>

          {a.badges.length > 0 ? (
            <Section title="Badges they hold">
              <ListGroup>
                {a.badges.map((b) => (
                  <ListRow key={b.kind} leading={<IdentityBadgeMark kind={b.kind} />} title={identityBadgeName(b)} info={`Since ${formatDate(b.since)}.`} />
                ))}
              </ListGroup>
            </Section>
          ) : null}

          <Section title="Evidence" info="Checked: a domain they proved with DNS, or an account they linked. Open links in a new tab.">
            <ListGroup>
              {a.evidence.map((e, i) => (
                <ListRow
                  key={i}
                  title={
                    e.kind === "url" ? (
                      <a href={e.ref} target="_blank" rel="noopener noreferrer nofollow" className="inline-flex items-center gap-1 underline-offset-2 hover:underline">
                        {e.ref}
                        <ExternalLink className="size-4 shrink-0" aria-hidden />
                      </a>
                    ) : (
                      e.ref
                    )
                  }
                  leading={<span className="w-16 type-caption text-fg-muted">{EVIDENCE_LABEL[e.kind]}</span>}
                  trailing={e.verified ? <Badge tone="success">Checked</Badge> : null}
                />
              ))}
            </ListGroup>
          </Section>

          {a.note ? (
            <Section title="Their note">
              <p className="whitespace-pre-wrap break-words type-body text-fg">{a.note}</p>
            </Section>
          ) : null}

          {open && !self ? (
            <div className="flex flex-wrap gap-2">
              <DecisionDialog a={a} decision="approve" />
              <DecisionDialog a={a} decision="needs_info" />
              <DecisionDialog a={a} decision="reject" />
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
