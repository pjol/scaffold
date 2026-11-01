"use client";

import {
  CRM_LIFECYCLE_LABEL,
  CRM_TYPE_LABEL,
  crmParseMoney,
  errorMessage,
  hasErrorCode,
  useCrmCreateRecord,
  useCrmDuplicates,
  useCrmPipelines,
  type CrmDuplicate,
  type CrmLifecycleStage,
  type CrmObjectType,
  type CrmSearchHit,
} from "@scaffoldapp/shared";
import { Banner, Button, Dialog, Field, IconButton, Input, Select, toast } from "@scaffoldapp/ui-web";
import { X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { useCrm } from "./CrmFrame";
import { RecordPicker, TYPE_ICON, recordHref } from "./parts";

/** What the create endpoint takes (field values by property key, plus associations). */
type CreateBody = Partial<Record<"name" | "domain" | "phone" | "first_name" | "last_name" | "emails" | "phones" | "job_title" | "lifecycle_stage" | "company_id" | "amount" | "stage" | "close_date" | "contact_ids" | "allow_duplicate", unknown>>;

interface Draft {
  name: string;
  domain: string;
  first: string;
  last: string;
  email: string;
  phone: string;
  title: string;
  lifecycle: CrmLifecycleStage;
  amount: string;
  stageId: string;
  closeDate: string;
  company: CrmSearchHit | null;
  contacts: CrmSearchHit[];
}

const empty: Draft = { name: "", domain: "", first: "", last: "", email: "", phone: "", title: "", lifecycle: "lead", amount: "", stageId: "", closeDate: "", company: null, contacts: [] };

/** New company, contact or deal: the key fields, a duplicate warning while typing, then the record page. */
export function CreateRecordDialog({
  type,
  open,
  onOpenChange,
  initial,
}: {
  type: CrmObjectType;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  /** Prefill associations (a contact from a company page, a deal from a contact page). */
  initial?: { company?: CrmSearchHit; contact?: CrmSearchHit };
}) {
  const { orgId, settings } = useCrm();
  const router = useRouter();
  const [d, setD] = useState<Draft>(empty);
  const [error, setError] = useState<string | null>(null);
  const [dups, setDups] = useState<CrmDuplicate[]>([]);
  const create = useCrmCreateRecord(orgId, type);
  const pipelines = useCrmPipelines(type === "deal" ? orgId : null);
  const stages = useMemo(() => (pipelines.data ?? []).flatMap((p) => p.stages.filter((s) => s.outcome === "open").map((s) => ({ ...s, pipeline: p }))), [pipelines.data]);
  const live = useCrmDuplicates(orgId, type === "contact" ? { object: "contact", emails: d.email.includes("@") ? [d.email.trim()] : [] } : { object: "company", domain: type === "company" ? d.domain.trim() : "" });

  useEffect(() => {
    if (open) {
      setD({ ...empty, company: initial?.company ?? null, contacts: initial?.contact ? [initial.contact] : [] });
      setError(null);
      setDups([]);
    }
  }, [open, initial]);
  useEffect(() => {
    if (!d.stageId && stages[0]) setD((x) => ({ ...x, stageId: stages[0]?.id ?? "" }));
  }, [stages, d.stageId]);

  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setD((x) => ({ ...x, [k]: v }));
  const matches = live.data && live.data.length > 0 ? live.data : dups;

  const submit = (allowDuplicate: boolean) => {
    const body: CreateBody = {};
    if (type === "company") {
      if (!d.name.trim()) return setError("Enter the company's name.");
      body.name = d.name.trim();
      if (d.domain.trim()) body.domain = d.domain.trim();
      if (d.phone.trim()) body.phone = d.phone.trim();
    } else if (type === "contact") {
      if (!d.first.trim() && !d.last.trim() && !d.email.trim()) return setError("Enter a name or an email address.");
      body.first_name = d.first.trim();
      body.last_name = d.last.trim();
      if (d.email.trim()) body.emails = [d.email.trim()];
      if (d.phone.trim()) body.phones = [d.phone.trim()];
      if (d.title.trim()) body.job_title = d.title.trim();
      body.lifecycle_stage = d.lifecycle;
      if (d.company) body.company_id = d.company.id;
    } else {
      if (!d.name.trim()) return setError("Enter the deal's name.");
      body.name = d.name.trim();
      if (d.amount.trim()) {
        const minor = crmParseMoney(d.amount, settings.currency);
        if (minor === null) return setError("Enter the amount as a number, like 12500.");
        body.amount = minor;
      }
      if (d.stageId) body.stage = d.stageId;
      if (d.closeDate) body.close_date = d.closeDate;
      if (d.company) body.company_id = d.company.id;
      if (d.contacts.length) body.contact_ids = d.contacts.map((c) => c.id);
    }
    if (allowDuplicate) body.allow_duplicate = true;
    setError(null);
    create.mutate(body, {
      onSuccess: (rec) => {
        toast.success(`${CRM_TYPE_LABEL[type].One} added`);
        onOpenChange(false);
        router.push(recordHref(type, rec.id));
      },
      onError: (e) => {
        if (hasErrorCode(e, "duplicate")) {
          setDups(((e.details as { matches?: CrmDuplicate[] } | undefined)?.matches ?? []) as CrmDuplicate[]);
          return;
        }
        setError(errorMessage(e, `Couldn't add the ${CRM_TYPE_LABEL[type].one}.`));
      },
    });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={`New ${CRM_TYPE_LABEL[type].one}`}
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          {dups.length > 0 ? (
            <Button variant="secondary" loading={create.isPending} onClick={() => submit(true)}>
              Add anyway
            </Button>
          ) : null}
          <Button loading={create.isPending && dups.length === 0} onClick={() => submit(false)}>
            Add {CRM_TYPE_LABEL[type].one}
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          submit(false);
        }}
      >
        {matches.length > 0 ? (
          <Banner tone="warning">
            {type === "contact" ? "A contact with this email already exists: " : "A company with this domain already exists: "}
            {matches.map((m, i) => (
              <span key={m.id}>
                {i > 0 ? ", " : ""}
                <Link className="font-semibold underline underline-offset-2" href={recordHref(m.type, m.id)}>
                  {m.name}
                </Link>
              </span>
            ))}
            .
          </Banner>
        ) : null}
        {type === "company" ? (
          <>
            <Field label="Name" required error={error}>
              <Input value={d.name} maxLength={200} autoFocus onChange={(e) => set("name", e.target.value)} placeholder="Northwind Outfitters" />
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Domain" info="The website address without https:// — used to spot duplicates and match contacts.">
                <Input value={d.domain} inputMode="url" autoCapitalize="none" onChange={(e) => set("domain", e.target.value)} placeholder="northwind.com" />
              </Field>
              <Field label="Phone">
                <Input value={d.phone} inputMode="tel" onChange={(e) => set("phone", e.target.value)} />
              </Field>
            </div>
          </>
        ) : type === "contact" ? (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="First name" error={error}>
                <Input value={d.first} maxLength={200} autoFocus onChange={(e) => set("first", e.target.value)} />
              </Field>
              <Field label="Last name">
                <Input value={d.last} maxLength={200} onChange={(e) => set("last", e.target.value)} />
              </Field>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Email">
                <Input type="email" value={d.email} autoCapitalize="none" onChange={(e) => set("email", e.target.value)} placeholder="dana@example.com" />
              </Field>
              <Field label="Phone">
                <Input value={d.phone} inputMode="tel" onChange={(e) => set("phone", e.target.value)} />
              </Field>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Job title">
                <Input value={d.title} maxLength={200} onChange={(e) => set("title", e.target.value)} />
              </Field>
              <Field label="Lifecycle stage">
                <Select value={d.lifecycle} onValueChange={(v) => set("lifecycle", v as CrmLifecycleStage)} options={(Object.keys(CRM_LIFECYCLE_LABEL) as CrmLifecycleStage[]).map((k) => ({ value: k, label: CRM_LIFECYCLE_LABEL[k] }))} />
              </Field>
            </div>
            <Field label="Company">
              <AssocPick orgId={orgId} type="company" value={d.company} onChange={(v) => set("company", v)} />
            </Field>
          </>
        ) : (
          <>
            <Field label="Name" required error={error}>
              <Input value={d.name} maxLength={200} autoFocus onChange={(e) => set("name", e.target.value)} placeholder="Brightline: 40 memberships" />
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Amount">
                <Input value={d.amount} inputMode="decimal" onChange={(e) => set("amount", e.target.value)} trailing={<span className="px-2 type-caption">{settings.currency}</span>} placeholder="12500" />
              </Field>
              <Field label="Close date">
                <Input type="date" value={d.closeDate} onChange={(e) => set("closeDate", e.target.value)} />
              </Field>
            </div>
            <Field label="Stage">
              <Select
                value={d.stageId}
                onValueChange={(v) => set("stageId", v)}
                options={stages.map((s) => ({ value: s.id, label: (pipelines.data?.length ?? 0) > 1 ? `${s.pipeline.name} · ${s.name}` : s.name }))}
              />
            </Field>
            <Field label="Company">
              <AssocPick orgId={orgId} type="company" value={d.company} onChange={(v) => set("company", v)} />
            </Field>
            <Field label="Contacts">
              <div className="flex flex-col gap-2">
                {d.contacts.map((c) => (
                  <Picked key={c.id} hit={c} onRemove={() => set("contacts", d.contacts.filter((x) => x.id !== c.id))} />
                ))}
                <RecordPicker orgId={orgId} types={["contact"]} exclude={new Set(d.contacts.map((c) => c.id))} label="Add a contact" onPick={(h) => set("contacts", [...d.contacts, h])} />
              </div>
            </Field>
          </>
        )}
        <button type="submit" hidden aria-hidden tabIndex={-1} />
      </form>
    </Dialog>
  );
}

function AssocPick({ orgId, type, value, onChange }: { orgId: string; type: CrmObjectType; value: CrmSearchHit | null; onChange: (v: CrmSearchHit | null) => void }) {
  if (value) return <Picked hit={value} onRemove={() => onChange(null)} />;
  return <RecordPicker orgId={orgId} types={[type]} label={`Find a ${CRM_TYPE_LABEL[type].one}`} onPick={onChange} />;
}

function Picked({ hit, onRemove }: { hit: CrmSearchHit; onRemove: () => void }) {
  const Icon = TYPE_ICON[hit.type];
  return (
    <div className="flex min-h-control items-center gap-2 rounded-md border border-border pl-3 pr-1">
      <Icon className="size-4 text-fg-muted" aria-hidden />
      <span className="min-w-0 flex-1 truncate type-label text-fg">{hit.title}</span>
      <IconButton label={`Remove ${hit.title}`} size="sm" icon={<X />} onClick={onRemove} />
    </div>
  );
}
