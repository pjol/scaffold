"use client";

import {
  errorMessage,
  useGrantIdentityBadge,
  useIdentityAdminBadges,
  useMe,
  useRevokeIdentityBadge,
  type IdentityAdminBadge,
  type IdentityBadgeKind,
} from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, Input, Select, TextArea, toast } from "@scaffoldapp/ui-web";
import { Award, Plus } from "lucide-react";
import { useState } from "react";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { IdentityBadgeMark, identityBadgeName } from "../BadgeMark";
import { IdentityAdminTabs } from "./IdentityAdminTabs";

type State = "active" | "ended" | "held";
const STATES: { value: State; label: string }[] = [
  { value: "active", label: "Active" },
  { value: "ended", label: "Ended" },
  { value: "held", label: "Removed paid badges" },
];
const KINDS: { value: IdentityBadgeKind | "all"; label: string }[] = [
  { value: "all", label: "Every kind" },
  { value: "official", label: "Official" },
  { value: "org", label: "Organization" },
  { value: "notable", label: "Notable" },
  { value: "paid", label: "Paid" },
];
const SOURCE: Record<IdentityAdminBadge["source"], string> = { review: "Application", admin: "Staff", billing: "Paid plan" };

function GrantDialog() {
  const grant = useGrantIdentityBadge();
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [kind, setKind] = useState<Exclude<IdentityBadgeKind, "paid">>("official");
  const [label, setLabel] = useState("");
  const [days, setDays] = useState("");
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    setError(null);
    grant.mutate(
      { email: email.trim(), kind, label: label.trim(), expires_in_days: days ? Number(days) : 0 },
      {
        onSuccess: () => {
          setOpen(false);
          setEmail("");
          setLabel("");
          setDays("");
          toast.success("Badge granted");
        },
        onError: (e) => setError(errorMessage(e, "Couldn't grant the badge. Try again.")),
      },
    );
  };
  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      title="Grant a badge"
      info="Organization and notable badges usually come from applications. Paid badges come from a plan."
      trigger={
        <Button size="auto" leftIcon={<Plus />}>
          Grant
        </Button>
      }
      footer={
        <>
          <Button variant="ghost" size="auto" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button size="auto" loading={grant.isPending} disabled={!email.includes("@")} onClick={submit}>
            Grant
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Email" required>
          <Input type="email" autoComplete="off" value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <Field label="Badge">
          <Select options={KINDS.filter((k) => k.value !== "all" && k.value !== "paid")} value={kind} onValueChange={(v) => setKind(v as Exclude<IdentityBadgeKind, "paid">)} />
        </Field>
        <Field label="Label" info="What the badge says. Empty uses the kind's name.">
          <Input value={label} maxLength={60} onChange={(e) => setLabel(e.target.value)} />
        </Field>
        <Field label="Expires in days" info="Empty: never.">
          <Input inputMode="numeric" value={days} onChange={(e) => setDays(e.target.value.replace(/\D/g, "").slice(0, 3))} />
        </Field>
        {error ? <Banner tone="danger">{error}</Banner> : null}
      </div>
    </Dialog>
  );
}

function RevokeDialog({ b }: { b: IdentityAdminBadge }) {
  const revoke = useRevokeIdentityBadge();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    if (reason.trim().length < 3) {
      setError("Give a reason of at least 3 characters. The person sees it.");
      return;
    }
    revoke.mutate(
      { userId: b.user.id, kind: b.kind, reason: reason.trim() },
      {
        onSuccess: () => {
          setOpen(false);
          toast.success("Badge removed");
        },
        onError: (e) => setError(errorMessage(e, "Couldn't remove the badge. Try again.")),
      },
    );
  };
  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      title="Remove this badge?"
      info={b.kind === "paid" ? "Their plan stops renewing, and the plan can't bring the badge back until an admin lifts the removal." : undefined}
      trigger={
        <Button size="sm" variant="danger-ghost">
          Remove
        </Button>
      }
      footer={
        <>
          <Button variant="ghost" size="auto" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button variant="danger" size="auto" loading={revoke.isPending} onClick={submit}>
            Remove
          </Button>
        </>
      }
    >
      <Field label="Reason" required error={error ?? undefined} info="The person sees this.">
        <TextArea value={reason} maxLength={500} minRows={3} onChange={(e) => setReason(e.target.value)} />
      </Field>
    </Dialog>
  );
}

function LiftRemoval({ b }: { b: IdentityAdminBadge }) {
  const grant = useGrantIdentityBadge();
  return (
    <ConfirmDialog
      title="Lift this removal?"
      confirmLabel="Lift removal"
      trigger={
        <Button size="sm" variant="secondary">
          Lift removal
        </Button>
      }
      onConfirm={() => grant.mutateAsync({ user_id: b.user.id, kind: "paid" })}
      errorText={(e) => errorMessage(e, "Couldn't lift the removal. Try again.")}
    >
      Their plan may show the badge again. A renewal stopped by the removal stays stopped until they resume it in billing.
    </ConfirmDialog>
  );
}

/** Admin → Verification → Badges: who holds what, grants (admins), removals and lifted paid-badge removals. Every change is audited. */
export function AdminBadgesScreen() {
  const me = useMe();
  const isAdmin = me.data?.admin_role === "admin";
  const [state, setState] = useUrlState<State>("state", "active");
  const [kind, setKind] = useUrlState<IdentityBadgeKind | "all">("kind", "all");
  const list = useIdentityAdminBadges({ state, kind: kind === "all" ? undefined : kind, limit: 50 });
  const rows = list.data?.items ?? [];
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Verification" info="Official badges are granted and removed by admins only." action={isAdmin ? <GrantDialog /> : null} />
      <div className="flex flex-col gap-4">
        <IdentityAdminTabs />
        <div className="flex flex-col gap-2 sm:flex-row">
          <Select aria-label="State" options={STATES} value={state} onValueChange={(v) => setState(v as State)} className="sm:w-56" />
          <Select aria-label="Kind" options={KINDS} value={kind} onValueChange={(v) => setKind(v as IdentityBadgeKind | "all")} className="sm:w-48" />
        </div>
        <Card padding="none">
          {list.isPending ? (
            <TableSkeleton rows={5} cols={4} />
          ) : list.isError ? (
            <ErrorState compact title="Couldn't load badges" info={errorMessage(list.error)} onRetry={() => void list.refetch()} retrying={list.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={<Award />} title="No badges here" />
          ) : (
            <>
              <Table label="Badges">
                <THead>
                  <TH>Person</TH>
                  <TH>Badge</TH>
                  <TH className="hidden md:table-cell">From</TH>
                  <TH className="hidden sm:table-cell" align="right">
                    {state === "active" ? "Expires" : "Ended"}
                  </TH>
                  <TH align="right">
                    <span className="sr-only">Actions</span>
                  </TH>
                </THead>
                <tbody>
                  {rows.map((b) => (
                    <TR key={`${b.user.id}:${b.kind}`}>
                      <TD>
                        <span className="block truncate font-semibold text-fg">{b.user.display_name || b.user.email}</span>
                        <span className="block truncate type-caption text-fg-muted">{b.user.email}</span>
                      </TD>
                      <TD>
                        <span className="flex min-w-0 items-center gap-2">
                          <IdentityBadgeMark kind={b.kind} className="size-4" />
                          <span className="truncate">{identityBadgeName(b)}</span>
                        </span>
                        {b.revoked_reason && state !== "active" ? <span className="block truncate type-caption text-fg-muted">{b.revoked_reason}</span> : null}
                      </TD>
                      <TD className="hidden text-fg-muted md:table-cell">{b.held ? <Badge tone="danger">Removed by staff</Badge> : SOURCE[b.source]}</TD>
                      <TD className="hidden whitespace-nowrap text-fg-muted sm:table-cell" align="right">
                        {state === "active" ? (b.expires_at ? formatDate(b.expires_at) : "Never") : b.revoked_at ? formatDate(b.revoked_at) : "—"}
                      </TD>
                      <TD align="right">
                        {b.held ? (isAdmin ? <LiftRemoval b={b} /> : null) : b.active && (b.kind !== "official" || isAdmin) && b.user.id !== me.data?.id ? <RevokeDialog b={b} /> : null}
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
    </div>
  );
}
