"use client";

import {
  errorMessage,
  useIdentityBadges,
  useWithdrawIdentityBadgeApplication,
  type IdentityApplicationStatus,
  type IdentityBadgeApplication,
  type IdentityBadges,
} from "@scaffoldapp/shared";
import { Badge, Banner, Button, ConfirmDialog, EmptyState, ErrorState, ListGroup, ListRow, Skeleton, SkeletonGroup, type BadgeTone } from "@scaffoldapp/ui-web";
import { Award, Check, X } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { Section } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { IDENTITY_BADGE_MEANING, IdentityBadgeMark, identityBadgeName } from "../BadgeMark";
import { identityPaidBadgeLink } from "../seams";
import { ApplyDialog } from "./ApplyDialog";
import { DomainProofs } from "./DomainProofs";

const STATUS: Record<IdentityApplicationStatus, { label: string; tone: BadgeTone }> = {
  submitted: { label: "In review", tone: "info" },
  needs_info: { label: "Needs more", tone: "warning" },
  approved: { label: "Approved", tone: "success" },
  rejected: { label: "Not approved", tone: "danger" },
  withdrawn: { label: "Withdrawn", tone: "neutral" },
};

const kindLabel = (st: IdentityBadges, kind: string) => st.kinds.find((k) => k.kind === kind)?.label ?? kind;

function YourBadges({ st }: { st: IdentityBadges }) {
  return (
    <Section title="Badges" info="The highest one shows next to your name: official, then organization, notable, supporter.">
      {st.badges.length === 0 ? (
        <EmptyState compact icon={<Award />} title="No badges yet" />
      ) : (
        <ListGroup>
          {st.badges.map((b) => (
            <ListRow
              key={b.kind}
              leading={<IdentityBadgeMark kind={b.kind} />}
              title={identityBadgeName(b)}
              info={[IDENTITY_BADGE_MEANING[b.kind], `Since ${formatDate(b.since)}.`, b.expires_at ? `${b.kind === "paid" ? "Renews or ends" : "Reviewed again"} ${formatDate(b.expires_at)}.` : ""]
                .filter(Boolean)
                .join(" ")}
            />
          ))}
        </ListGroup>
      )}
    </Section>
  );
}

function PaidBadge({ st }: { st: IdentityBadges }) {
  const paid = st.paid;
  const label = kindLabel(st, "paid");
  const href = identityPaidBadgeLink(paid.entitlement);
  if (!href && !paid.active && !paid.held) return null;
  return (
    <Section title={`${label} badge`} info={IDENTITY_BADGE_MEANING.paid}>
      {paid.held ? (
        <Banner tone="warning">Staff removed your {label.toLowerCase()} badge. Contact support to ask about it.</Banner>
      ) : paid.active ? (
        <ListGroup>
          <ListRow leading={<IdentityBadgeMark kind="paid" />} title="Active" info={paid.expires_at ? `Renews or ends ${formatDate(paid.expires_at)}.` : undefined} />
        </ListGroup>
      ) : (
        <>
          <ListGroup title="To get it">
            {paid.requirements.map((r) => (
              <ListRow
                key={r.key}
                leading={
                  r.met ? (
                    <Check className="size-5 text-success-strong" aria-label="Met" role="img" />
                  ) : (
                    <X className="size-5 text-danger-strong" aria-label="Not met" role="img" />
                  )
                }
                title={r.label}
                info={r.detail}
              />
            ))}
          </ListGroup>
          {href ? (
            paid.eligible ? (
              <Button asChild size="auto" className="self-start">
                <Link href={href}>Get the {label.toLowerCase()} badge</Link>
              </Button>
            ) : (
              <Button size="auto" className="self-start" disabled>
                Get the {label.toLowerCase()} badge
              </Button>
            )
          ) : null}
        </>
      )}
    </Section>
  );
}

function ApplicationRow({ a, st, onResubmit }: { a: IdentityBadgeApplication; st: IdentityBadges; onResubmit: (a: IdentityBadgeApplication) => void }) {
  const withdraw = useWithdrawIdentityBadgeApplication();
  const cat = st.categories.find((c) => c.key === a.category)?.label ?? a.category;
  const open = a.status === "submitted" || a.status === "needs_info";
  const checked = a.evidence.filter((e) => e.verified).length;
  return (
    <div className="flex flex-col gap-2 py-3">
      <div className="flex min-h-control items-center gap-2">
        <span className="min-w-0 flex-1 truncate type-body font-medium text-fg">{a.display_label && a.display_label !== cat ? `${cat} · ${a.display_label}` : cat}</span>
        <Badge tone={STATUS[a.status].tone}>{STATUS[a.status].label}</Badge>
      </div>
      <p className="type-caption text-fg-muted">
        Sent {formatDate(a.submitted_at)}
        {a.decided_at ? ` · decided ${formatDate(a.decided_at)}` : ""} · {checked} of {a.evidence.length} evidence checked
      </p>
      {a.reviewer_note && (a.status === "needs_info" || a.status === "rejected" || a.status === "approved") ? (
        <Banner tone={a.status === "rejected" ? "danger" : a.status === "approved" ? "success" : "warning"}>{a.reviewer_note}</Banner>
      ) : null}
      {open ? (
        <div className="flex flex-wrap gap-2">
          {a.status === "needs_info" ? (
            <Button size="sm" onClick={() => onResubmit(a)}>
              Update and send
            </Button>
          ) : null}
          <ConfirmDialog
            title="Withdraw this application?"
            confirmLabel="Withdraw"
            tone="danger"
            trigger={
              <Button size="sm" variant="ghost">
                Withdraw
              </Button>
            }
            onConfirm={() => withdraw.mutateAsync(a.id)}
            errorText={(e) => errorMessage(e, "Couldn't withdraw. Try again.")}
          >
            You can apply again later.
          </ConfirmDialog>
        </div>
      ) : null}
    </div>
  );
}

/**
 * Settings → Verification, below ID verification: the person's badges, the paid badge (when
 * the app sells it: `setIdentityPaidBadgeLink`), applications for reviewed badges and domain proofs.
 */
export function IdentityBadgesSections() {
  const q = useIdentityBadges();
  const [dialog, setDialog] = useState<{ open: boolean; resubmit: IdentityBadgeApplication | null; n: number }>({ open: false, resubmit: null, n: 0 });
  if (q.isPending) {
    return (
      <SkeletonGroup label="Loading badges" className="flex max-w-form flex-col gap-4">
        <Skeleton className="h-6 w-32" />
        <Skeleton className="h-16" />
      </SkeletonGroup>
    );
  }
  if (q.isError) {
    return <ErrorState title="Couldn't load your badges" info={errorMessage(q.error)} onRetry={() => void q.refetch()} retrying={q.isFetching} />;
  }
  const st = q.data;
  const openDialog = (resubmit: IdentityBadgeApplication | null) => setDialog((d) => ({ open: true, resubmit, n: d.n + 1 }));
  return (
    <>
      <YourBadges st={st} />
      <PaidBadge st={st} />
      {st.categories.length > 0 ? (
        <Section
          title="Apply for a badge"
          info={`For ${st.categories.map((c) => c.label.toLowerCase()).join(", ")}. Staff review it; badges are reviewed again after a while.`}
        >
          <Button size="auto" variant="secondary" className="self-start" onClick={() => openDialog(null)}>
            Apply
          </Button>
        </Section>
      ) : null}
      {st.applications.length > 0 ? (
        <Section title="Applications">
          <ListGroup>
            {st.applications.map((a) => (
              <ApplicationRow key={a.id} a={a} st={st} onResubmit={openDialog} />
            ))}
          </ListGroup>
        </Section>
      ) : null}
      <DomainProofs domains={st.domains} />
      <ApplyDialog key={dialog.n} st={st} open={dialog.open} onOpenChange={(open) => setDialog((d) => ({ ...d, open }))} resubmit={dialog.resubmit} />
    </>
  );
}
