"use client";

import { errorMessage, formatMinor, priceInterval, useAdminPrices, useCreatePrice, useDeletePrice, useMe, useUpdatePrice, type AdminPrice } from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, Dialog, EmptyState, Field, IconButton, Input, Switch, toast } from "@scaffoldapp/ui-web";
import { Pencil, Plus, Tags, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { PageHeader } from "@/components/shell/PageHeader";
import { AdminError } from "./AdminUnavailable";
import { PaymentsAdminTabs } from "./PaymentsAdminTabs";

interface FormState {
  key: string;
  providerPriceId: string;
  label: string;
  entitlement: string;
  entitlementDays: string;
  active: boolean;
}

const EMPTY: FormState = { key: "", providerPriceId: "", label: "", entitlement: "", entitlementDays: "", active: true };

/** Create or edit a catalog entry. The server reads amount, currency and interval from the provider. */
function PriceDialog({ editing, open, onOpenChange }: { editing: AdminPrice | null; open: boolean; onOpenChange: (open: boolean) => void }) {
  const create = useCreatePrice();
  const update = useUpdatePrice();
  const [f, setF] = useState<FormState>(EMPTY);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!open) return;
    setError(null);
    setF(
      editing
        ? {
            key: editing.key,
            providerPriceId: editing.provider_price_id,
            label: editing.label,
            entitlement: editing.entitlement ?? "",
            entitlementDays: editing.entitlement_days ? String(editing.entitlement_days) : "",
            active: editing.active,
          }
        : EMPTY,
    );
  }, [open, editing]);
  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setF((prev) => ({ ...prev, [k]: v }));
  const days = f.entitlementDays.trim() ? Number(f.entitlementDays) : 0;
  const busy = create.isPending || update.isPending;

  const submit = () => {
    setError(null);
    const done = (verb: string) => () => {
      toast.success(`${verb} ${f.key}`);
      onOpenChange(false);
    };
    const fail = (e: unknown) => setError(errorMessage(e, "Couldn't save the price. Try again."));
    if (editing) {
      update.mutate(
        { key: editing.key, provider_price_id: f.providerPriceId.trim(), label: f.label.trim(), entitlement: f.entitlement.trim(), entitlement_days: Number.isFinite(days) ? days : 0, active: f.active },
        { onSuccess: done("Saved"), onError: fail },
      );
    } else {
      create.mutate(
        {
          key: f.key.trim(),
          provider_price_id: f.providerPriceId.trim(),
          ...(f.label.trim() ? { label: f.label.trim() } : {}),
          ...(f.entitlement.trim() ? { entitlement: f.entitlement.trim() } : {}),
          ...(days > 0 ? { entitlement_days: days } : {}),
          active: f.active,
        },
        { onSuccess: done("Added"), onError: fail },
      );
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={editing ? `Edit ${editing.key}` : "Add price"}
      info="Create the product and price at the payment provider first, then map it here. Amount, currency and interval come from the provider."
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button loading={busy} disabled={!f.key.trim() || !f.providerPriceId.trim()} onClick={submit}>
            {editing ? "Save" : "Add price"}
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Field label="Key" info="Stable id code and checkouts use, e.g. pro_monthly. Lowercase letters, digits and underscores. Can't be renamed.">
          <Input value={f.key} onChange={(e) => set("key", e.target.value)} disabled={!!editing} autoComplete="off" spellCheck={false} />
        </Field>
        <Field label="Provider price ID" info="Stripe price id (price_…).">
          <Input value={f.providerPriceId} onChange={(e) => set("providerPriceId", e.target.value)} autoComplete="off" spellCheck={false} />
        </Field>
        <Field label="Label" info="Shown on the plan picker and receipts. Defaults to the provider's product name.">
          <Input value={f.label} onChange={(e) => set("label", e.target.value)} maxLength={80} />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Entitlement" info="What buying it grants, e.g. pro. Features check it. Leave empty for none (a physical item).">
            <Input value={f.entitlement} onChange={(e) => set("entitlement", e.target.value)} autoComplete="off" spellCheck={false} />
          </Field>
          <Field label="Access days" info="One-time prices only: how long the entitlement lasts. Empty = no expiry.">
            <Input inputMode="numeric" value={f.entitlementDays} onChange={(e) => set("entitlementDays", e.target.value.replace(/[^0-9]/g, ""))} />
          </Field>
        </div>
        <Switch checked={f.active} onCheckedChange={(v) => set("active", v)} label="Available to buy" />
        {error && (
          <p role="alert" className="type-label text-danger-strong">
            {error}
          </p>
        )}
      </form>
    </Dialog>
  );
}

/** Admin → Payments → Prices: the catalog of stable keys mapped to provider prices. */
export function PricesScreen() {
  const prices = useAdminPrices();
  const del = useDeletePrice();
  const me = useMe();
  const isAdmin = me.data?.admin_role === "admin";
  const [editing, setEditing] = useState<AdminPrice | null>(null);
  const [adding, setAdding] = useState(false);
  const [deleting, setDeleting] = useState<AdminPrice | null>(null);
  const rows = prices.data ?? [];

  return (
    <div className="flex flex-col gap-section">
      <PageHeader
        title="Payments"
        action={
          isAdmin ? (
            <Button size="sm" leftIcon={<Plus />} onClick={() => setAdding(true)}>
              Add price
            </Button>
          ) : null
        }
      />
      <div className="flex flex-col gap-4">
        <PaymentsAdminTabs />
        <Card padding="none">
          {prices.isPending ? (
            <TableSkeleton rows={4} cols={4} />
          ) : prices.isError ? (
            <AdminError error={prices.error} title="Couldn't load prices" onRetry={() => void prices.refetch()} retrying={prices.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState
              compact
              icon={<Tags />}
              title="No prices yet"
              action={
                isAdmin ? (
                  <Button size="auto" leftIcon={<Plus />} onClick={() => setAdding(true)}>
                    Add price
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <Table label="Prices" className="min-w-160">
              <THead>
                <TH>Key</TH>
                <TH className="hidden sm:table-cell">Label</TH>
                <TH align="right">Price</TH>
                <TH className="hidden md:table-cell">Grants</TH>
                <TH>Status</TH>
                <TH align="right" className="relative">
                  <span className="sr-only">Actions</span>
                </TH>
              </THead>
              <tbody>
                {rows.map((p) => {
                  const every = priceInterval(p);
                  return (
                    <TR key={p.id}>
                      <TD className="font-mono font-semibold">{p.key}</TD>
                      <TD className="hidden max-w-48 truncate sm:table-cell">{p.label}</TD>
                      <TD align="right" className="whitespace-nowrap">
                        {p.amount_minor === null ? "At checkout" : `${formatMinor(p.amount_minor, p.currency)}${every ? ` / ${every}` : ""}`}
                      </TD>
                      <TD className="hidden text-fg-muted md:table-cell">{p.entitlement ? (p.entitlement_days ? `${p.entitlement} for ${p.entitlement_days} days` : p.entitlement) : "—"}</TD>
                      <TD>
                        <Badge tone={p.active ? "success" : "neutral"}>{p.active ? "Active" : "Inactive"}</Badge>
                      </TD>
                      <TD align="right">
                        {isAdmin && (
                          <span className="inline-flex gap-1">
                            <IconButton label={`Edit ${p.key}`} icon={<Pencil />} size="sm" variant="ghost" tooltip onClick={() => setEditing(p)} />
                            <IconButton label={`Delete ${p.key}`} icon={<Trash2 />} size="sm" variant="ghost" tooltip onClick={() => setDeleting(p)} />
                          </span>
                        )}
                      </TD>
                    </TR>
                  );
                })}
              </tbody>
            </Table>
          )}
        </Card>
      </div>
      <PriceDialog editing={editing} open={adding || editing !== null} onOpenChange={(o) => {
        if (!o) {
          setAdding(false);
          setEditing(null);
        }
      }} />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete ${deleting?.key ?? "this price"}?`}
        tone="danger"
        confirmLabel="Delete price"
        errorText={(e) => errorMessage(e, "Couldn't delete the price.")}
        onConfirm={async () => {
          if (!deleting) return;
          await del.mutateAsync(deleting.key);
          toast.success(`Deleted ${deleting.key}`);
        }}
      >
        <p className="type-body text-fg">A price with purchases can't be deleted; turn off Available to buy instead.</p>
      </ConfirmDialog>
    </div>
  );
}
