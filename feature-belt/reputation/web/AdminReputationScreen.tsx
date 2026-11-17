"use client";

import {
  REPUTATION_FLAG_KIND_INFO,
  errorMessage,
  reputationFlagKindLabel,
  reputationMetricLabel,
  useAdminReputation,
  useDecideReputationFlag,
  useReputationFlags,
  type ReputationFlag,
  type ReputationFlagStatus,
  type ReputationPerson,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, EmptyState, ErrorState, Field, InfoTip, SegmentedControl, Sheet, Skeleton, SkeletonGroup, TextArea, toast } from "@scaffoldapp/ui-web";
import { ShieldCheck } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { ReputationDetail } from "./ReputationDetail";

function personName(p: ReputationPerson): string {
  if (p.status === "deleted") return "Deleted user";
  return p.display_name || p.email;
}

function PersonCell({ p }: { p: ReputationPerson }) {
  const name = personName(p);
  return (
    <span className="flex min-w-0 items-center gap-2">
      <AppAvatar name={name} src={p.avatar_url} size="xs" />
      <span className="truncate text-fg">{name}</span>
    </span>
  );
}

function KindBadge({ kind }: { kind: string }) {
  const info = REPUTATION_FLAG_KIND_INFO[kind];
  return (
    <span className="inline-flex items-center gap-1">
      <Badge tone="warning">{reputationFlagKindLabel(kind)}</Badge>
      {info && <InfoTip content={info} label={`What ${reputationFlagKindLabel(kind).toLowerCase()} means`} />}
    </span>
  );
}

function Decide({ flag, decision, onDone }: { flag: ReputationFlag; decision: "clear" | "confirm"; onDone: () => void }) {
  const decide = useDecideReputationFlag();
  const [note, setNote] = useState("");
  const name = personName(flag.user);
  const clear = decision === "clear";
  return (
    <ConfirmDialog
      trigger={
        <Button variant={clear ? "secondary" : "danger"} size="auto">
          {clear ? "Clear" : "Confirm"}
        </Button>
      }
      title={clear ? `Clear this flag on ${name}?` : `Confirm this flag on ${name}?`}
      confirmLabel={clear ? "Clear flag" : "Confirm flag"}
      tone={clear ? "primary" : "danger"}
      onConfirm={() =>
        decide.mutateAsync({ id: flag.id, decision, note }).then(() => {
          toast.success(clear ? "Flag cleared" : "Flag confirmed");
          onDone();
        })
      }
      errorText={(e) => errorMessage(e, "Couldn't save the decision. Try again.")}
    >
      <div className="flex flex-col gap-4">
        <p className="type-body text-fg">
          {clear ? "Their score shows again unless another flag is open." : "Their score stays hidden for 90 days."}
        </p>
        <Field label="Note" info="Kept with the decision and in the audit log.">
          <TextArea value={note} onChange={(e) => setNote(e.target.value)} maxLength={1000} minRows={2} />
        </Field>
      </div>
    </ConfirmDialog>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 border-b border-border py-2">
      <dt className="type-label text-fg-muted">{label}</dt>
      <dd className="min-w-0 text-right type-label text-fg">{children}</dd>
    </div>
  );
}

function FlagSheet({ flag, onClose }: { flag: ReputationFlag | null; onClose: () => void }) {
  const rep = useAdminReputation(flag?.user.id);
  const metrics = Object.entries(flag?.metrics ?? {});
  return (
    <Sheet
      open={!!flag}
      onOpenChange={(o) => !o && onClose()}
      title={flag ? reputationFlagKindLabel(flag.kind) : "Flag"}
      size="lg"
      {...(flag?.status === "open"
        ? {
            footer: (
              <div className="flex justify-end gap-2">
                <Decide flag={flag} decision="clear" onDone={onClose} />
                <Decide flag={flag} decision="confirm" onDone={onClose} />
              </div>
            ),
          }
        : {})}
    >
      {flag && (
        <div className="flex flex-col gap-section">
          <dl className="flex flex-col">
            <Fact label="Check">
              <KindBadge kind={flag.kind} />
            </Fact>
            <Fact label="Person">{personName(flag.user)}</Fact>
            {flag.counterparty && <Fact label="With">{personName(flag.counterparty)}</Fact>}
            <Fact label="Raised">{formatDateTime(flag.created_at)}</Fact>
            {flag.decided_at && (
              <Fact label={flag.status === "cleared" ? "Cleared" : "Confirmed"}>
                {formatDateTime(flag.decided_at)}
                {flag.decided_by_name ? ` by ${flag.decided_by_name}` : ""}
              </Fact>
            )}
            {flag.note && <Fact label="Note">{flag.note}</Fact>}
          </dl>
          {metrics.length > 0 && (
            <Section title="What was detected">
              <dl className="flex flex-col">
                {metrics.map(([k, v]) => (
                  <Fact key={k} label={reputationMetricLabel(k)}>
                    <span className="tabular-nums">{v}</span>
                  </Fact>
                ))}
              </dl>
            </Section>
          )}
          <Section title="Their reputation" info="What the person would see, including the score the flag hides.">
            {rep.isPending ? (
              <SkeletonGroup label="Loading their reputation" className="flex flex-col gap-3">
                <Skeleton className="h-20" />
                <Skeleton className="h-48" />
              </SkeletonGroup>
            ) : rep.isError ? (
              <ErrorState compact title="Couldn't load their reputation" info={rep.error.message} onRetry={() => void rep.refetch()} />
            ) : (
              <ReputationDetail rep={rep.data} />
            )}
          </Section>
        </div>
      )}
    </Sheet>
  );
}

/** Admin → Reputation: integrity flags; open ones oldest first, each cleared or confirmed with a note (audited). */
export function AdminReputationScreen() {
  const [status, setStatus] = useUrlState<ReputationFlagStatus>("status", "open");
  const flags = useReputationFlags(status);
  const [selected, setSelected] = useState<ReputationFlag | null>(null);
  const rows = flags.data?.items ?? [];
  const open = status === "open";
  return (
    <div className="flex flex-col gap-section">
      <PageHeader
        title="Reputation"
        info="Integrity checks flag possible gaming. An open flag hides the person's score until you clear or confirm it; a confirmed flag keeps it hidden for 90 days."
      />
      <div className="flex flex-col gap-4">
        <SegmentedControl<ReputationFlagStatus>
          aria-label="Flags"
          className="self-start"
          fullWidth={false}
          value={status}
          onValueChange={setStatus}
          options={[
            { value: "open", label: "Open" },
            { value: "cleared", label: "Cleared" },
            { value: "confirmed", label: "Confirmed" },
          ]}
        />
        <Card padding="none">
          {flags.isPending ? (
            <TableSkeleton rows={5} cols={4} />
          ) : flags.isError ? (
            <ErrorState compact title="Couldn't load flags" info={flags.error.message} onRetry={() => void flags.refetch()} retrying={flags.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={<ShieldCheck />} title={open ? "No open flags" : status === "cleared" ? "No cleared flags" : "No confirmed flags"} />
          ) : (
            <>
              <Table label="Integrity flags">
                <THead>
                  <TH>Person</TH>
                  <TH>Kind</TH>
                  <TH className="hidden md:table-cell">With</TH>
                  <TH align="right">{open ? "Raised" : "Decided"}</TH>
                </THead>
                <tbody>
                  {rows.map((f) => (
                    <TR key={f.id} onActivate={() => setSelected(f)}>
                      <TD className="max-w-0 sm:max-w-none">
                        <PersonCell p={f.user} />
                      </TD>
                      <TD className="whitespace-nowrap">
                        <Badge tone="warning">{reputationFlagKindLabel(f.kind)}</Badge>
                      </TD>
                      <TD className="hidden md:table-cell">{f.counterparty ? <PersonCell p={f.counterparty} /> : <span className="text-fg-muted">None</span>}</TD>
                      <TD align="right" className="whitespace-nowrap tabular-nums text-fg-muted">
                        {formatRelative(open ? f.created_at : (f.decided_at ?? f.created_at))}
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={flags.hasNextPage} isFetchingNextPage={flags.isFetchingNextPage} fetchNextPage={flags.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
      <FlagSheet flag={selected} onClose={() => setSelected(null)} />
    </div>
  );
}
