"use client";

import {
  CRM_LIFECYCLE_LABEL,
  errorMessage,
  useCrmChangeForm,
  useCrmForm,
  useCrmFormSubmissions,
  useCrmProperties,
  useCrmTags,
  type CrmForm,
  type CrmFormField,
  type CrmLifecycleStage,
  type CrmProperty,
  type CrmUserRef,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  Card,
  Checkbox,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  Select,
  Skeleton,
  SkeletonGroup,
  TextArea,
  ToggleChip,
  toast,
} from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowLeft, ArrowUp, ClipboardList, Copy, ExternalLink, Globe, Inbox, Plus, Trash2, Undo2, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { formatDateTime } from "@/lib/format";
import { CrmFrame, useCrm } from "./CrmFrame";
import { FormFieldsView, type FormValues } from "./PublicFormScreen";
import { MemberPicker, PersonChip, recordHref } from "./parts";

const FIELD_TYPES = new Set(["text", "long_text", "number", "date", "select", "multi_select", "url", "email", "phone", "phones", "checkbox", "domain"]);
const NOT_ON_FORMS = new Set(["owner", "team", "tags", "lifecycle_stage", "lead_status", "emails", "score"]);

/** /crm/forms/[id]: fields, settings, publishing (link and embed), a preview and the submissions. */
export function CrmFormEditorScreen({ id }: { id: string }) {
  return (
    <CrmFrame>
      <Editor id={id} />
    </CrmFrame>
  );
}

function Editor({ id }: { id: string }) {
  const { orgId } = useCrm();
  const form = useCrmForm(orgId, id);
  if (form.isPending) {
    return (
      <SkeletonGroup label="Loading the form" className="flex flex-col gap-6">
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-96 w-full" />
      </SkeletonGroup>
    );
  }
  if (form.isError) {
    return form.error.status === 404 ? (
      <Card padding="compact">
        <EmptyState icon={<ClipboardList />} title="Form not found" />
      </Card>
    ) : (
      <ErrorState title="Couldn't load the form" info={form.error.message} onRetry={() => void form.refetch()} retrying={form.isRefetching} />
    );
  }
  return <FormBody key={form.data.updated_at} form={form.data} />;
}

function FormBody({ form }: { form: CrmForm }) {
  const { orgId, settings } = useCrm();
  const router = useRouter();
  const manage = !!settings.can.manage_forms;
  const change = useCrmChangeForm(orgId);
  const contactProps = useCrmProperties(orgId, "contact").data ?? [];
  const companyProps = useCrmProperties(orgId, "company").data ?? [];
  const tags = useCrmTags(orgId).data ?? [];
  const [name, setName] = useState(form.name);
  const [fields, setFields] = useState<CrmFormField[]>(form.fields);
  const [submitLabel, setSubmitLabel] = useState(form.submit_label);
  const [success, setSuccess] = useState(form.success_message);
  const [consent, setConsent] = useState(form.consent_text);
  const [owner, setOwner] = useState<CrmUserRef | null>(form.owner);
  const [notify, setNotify] = useState<CrmUserRef[]>(form.notify);
  const [lifecycle, setLifecycle] = useState<CrmLifecycleStage | "">(form.lifecycle_stage ?? "");
  const [tagIds, setTagIds] = useState<string[]>(form.tag_ids);
  const [picking, setPicking] = useState<"owner" | "notify" | null>(null);
  const [adding, setAdding] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [preview, setPreview] = useState<FormValues>({});
  const [error, setError] = useState<string | null>(null);
  const fail = (fallback: string) => (e: unknown) => toast.error(errorMessage(e, fallback));

  const propOf = (f: CrmFormField): CrmProperty | undefined => (f.object === "company" ? companyProps : contactProps).find((p) => p.key === f.key);
  const previewFields = fields.map((f) => {
    const p = propOf(f);
    return { ...f, type: f.key === "email" && f.object === "contact" ? ("email" as const) : (p?.type ?? f.type), options: p?.options ?? f.options ?? [] };
  });
  const save = () => {
    if (!name.trim()) return setError("Name the form.");
    setError(null);
    change.mutate(
      {
        op: "save",
        id: form.id,
        body: {
          name: name.trim(),
          fields: fields.map(({ key, object, label, required }) => ({ key, object, label, required })),
          submit_label: submitLabel,
          success_message: success,
          consent_text: consent,
          ...(owner ? { owner_id: owner.id } : {}),
          notify_ids: notify.map((u) => u.id),
          lifecycle_stage: lifecycle,
          tag_ids: tagIds,
        },
      },
      { onSuccess: () => toast.success("Form saved"), onError: (e) => setError(errorMessage(e, "Couldn't save the form.")) },
    );
  };
  const move = (i: number, d: -1 | 1) => {
    const j = i + d;
    if (j < 0 || j >= fields.length) return;
    const next = [...fields];
    [next[i], next[j]] = [next[j] as CrmFormField, next[i] as CrmFormField];
    setFields(next);
  };
  const candidates = useMemo(() => {
    const taken = new Set(fields.map((f) => `${f.object}.${f.key}`));
    const of = (object: "contact" | "company", list: CrmProperty[]) =>
      list.filter((p) => p.editable && FIELD_TYPES.has(p.type) && !NOT_ON_FORMS.has(p.key) && !taken.has(`${object}.${p.key}`)).map((p) => ({ object, p }));
    return [...of("contact", contactProps), ...of("company", companyProps)];
  }, [fields, contactProps, companyProps]);

  return (
    <div className="flex flex-col gap-6">
      <Card padding="compact" className="flex flex-col gap-4">
        <div className="flex items-start gap-3">
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <Link href="/crm/forms" className="inline-flex w-fit items-center gap-1 type-label text-fg-muted hover:text-fg hover:underline">
              <ArrowLeft className="size-4" aria-hidden />
              Forms
            </Link>
            <div className="flex flex-wrap items-center gap-3">
              <h2 className="type-headline break-words text-fg">{form.name}</h2>
              {form.published ? (
                <Badge tone="success" dot size="md">
                  Published
                </Badge>
              ) : (
                <Badge size="md">Not published</Badge>
              )}
              <span className="type-label text-fg-muted">{form.submission_count === 1 ? "1 submission" : `${form.submission_count} submissions`}</span>
            </div>
          </div>
          <ResourceMenu
            feature="crm"
            kind="crm.form"
            id={form.id}
            title={form.name}
            orgId={orgId}
            actions={
              manage
                ? [
                    ...(form.published ? [{ id: "crm-form-unpublish", label: "Unpublish", icon: Undo2, onSelect: () => change.mutate({ op: "publish", id: form.id, publish: false }, { onSuccess: () => toast.success("Unpublished: the link and embeds stopped working"), onError: fail("Couldn't unpublish it.") }) }] : []),
                    { id: "crm-form-delete", label: "Delete form", icon: Trash2, destructive: true, onSelect: () => setDeleting(true) },
                  ]
                : []
            }
          />
        </div>
        {form.published && form.url ? (
          <PublishedLinks url={form.url} embed={form.embed_html ?? ""} />
        ) : manage ? (
          <div className="flex flex-wrap items-center gap-3">
            <Button leftIcon={<Globe />} loading={change.isPending} onClick={() => change.mutate({ op: "publish", id: form.id, publish: true }, { onSuccess: () => toast.success("Published"), onError: fail("Couldn't publish it.") })}>
              Publish
            </Button>
            <span className="type-label text-fg-muted">Publishing makes a link and an embed code anyone can fill in.</span>
          </div>
        ) : null}
        {!manage ? <Banner tone="info">People with the “Manage CRM forms” permission change and publish forms.</Banner> : null}
      </Card>

      <div className="grid items-start gap-6 lg:grid-cols-2">
        <div className="flex flex-col gap-6">
          <Card padding="compact" className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              <h2 className="flex-1 type-title-sm text-fg">Fields</h2>
              {manage ? (
                <Button variant="ghost" size="sm" leftIcon={<Plus />} disabled={candidates.length === 0} onClick={() => setAdding(true)}>
                  Add field
                </Button>
              ) : null}
            </div>
            <ul className="flex flex-col gap-2">
              {fields.map((f, i) => {
                const fixed = f.object === "contact" && f.key === "email";
                return (
                  <li key={`${f.object}.${f.key}`} className="flex flex-wrap items-center gap-2 rounded-md bg-sunken p-2">
                    <Badge>{f.object === "company" ? "Company" : "Contact"}</Badge>
                    <Input aria-label={`Label of ${f.key}`} value={f.label} maxLength={120} disabled={!manage} onChange={(e) => setFields(fields.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)))} wrapperClassName="min-w-32 flex-1" />
                    <Checkbox checked={f.required} disabled={!manage || fixed} onCheckedChange={(v) => setFields(fields.map((x, j) => (j === i ? { ...x, required: v === true } : x)))} label="Required" />
                    {manage ? (
                      <>
                        <IconButton label="Move up" size="sm" icon={<ArrowUp />} disabled={i === 0} onClick={() => move(i, -1)} />
                        <IconButton label="Move down" size="sm" icon={<ArrowDown />} disabled={i === fields.length - 1} onClick={() => move(i, 1)} />
                        <IconButton label={`Remove ${f.label}`} size="sm" icon={<X />} disabled={fixed} onClick={() => setFields(fields.filter((_, j) => j !== i))} />
                      </>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          </Card>
          <Card padding="compact" className="flex flex-col gap-4">
            <h2 className="type-title-sm text-fg">Settings</h2>
            <Field label="Name" required error={error}>
              <Input value={name} maxLength={120} disabled={!manage} onChange={(e) => setName(e.target.value)} />
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Button">
                <Input value={submitLabel} maxLength={40} disabled={!manage} onChange={(e) => setSubmitLabel(e.target.value)} />
              </Field>
              <Field label="New contacts' lifecycle stage">
                <Select
                  value={lifecycle || "none"}
                  disabled={!manage}
                  onValueChange={(v) => setLifecycle(v === "none" ? "" : (v as CrmLifecycleStage))}
                  options={[{ value: "none", label: "Default (lead)" }, ...(Object.keys(CRM_LIFECYCLE_LABEL) as CrmLifecycleStage[]).map((k) => ({ value: k, label: CRM_LIFECYCLE_LABEL[k] }))]}
                />
              </Field>
            </div>
            <Field label="Thank-you message">
              <TextArea value={success} maxLength={500} disabled={!manage} onChange={(e) => setSuccess(e.target.value)} placeholder="Thanks — we'll be in touch within a day." />
            </Field>
            <Field label="Consent text" info="When set, people must tick a box with this text before sending; the tick is kept with the submission.">
              <TextArea value={consent} maxLength={1000} disabled={!manage} onChange={(e) => setConsent(e.target.value)} />
            </Field>
            <Field label="Owner" info="New contacts belong to this person, and submissions run with their access (they must still use the CRM).">
              <div className="flex flex-wrap items-center gap-2">
                <PersonChip user={owner} size="sm" />
                {manage ? (
                  <Button variant="ghost" size="sm" onClick={() => setPicking("owner")}>
                    Change
                  </Button>
                ) : null}
              </div>
            </Field>
            <Field label="Notify" info="These people get a notification for each submission, when they can open the contact.">
              <div className="flex flex-wrap items-center gap-2">
                {notify.map((u) => (
                  <span key={u.id} className="inline-flex items-center gap-1 rounded-pill bg-sunken pl-1 pr-1">
                    <PersonChip user={u} />
                    {manage ? <IconButton label={`Stop notifying ${u.name}`} size="sm" icon={<X />} onClick={() => setNotify(notify.filter((x) => x.id !== u.id))} /> : null}
                  </span>
                ))}
                {manage ? (
                  <Button variant="ghost" size="sm" leftIcon={<Plus />} onClick={() => setPicking("notify")}>
                    Add person
                  </Button>
                ) : null}
              </div>
            </Field>
            {tags.length > 0 ? (
              <Field label="Tag new contacts">
                <div className="flex flex-wrap gap-2" role="group" aria-label="Tags">
                  {tags.map((t) => (
                    <ToggleChip key={t.id} pressed={tagIds.includes(t.id)} disabled={!manage} onPressedChange={(on) => setTagIds(on ? [...tagIds, t.id] : tagIds.filter((x) => x !== t.id))}>
                      {t.name}
                    </ToggleChip>
                  ))}
                </div>
              </Field>
            ) : null}
            {manage ? (
              <div className="flex justify-end">
                <Button loading={change.isPending} onClick={save}>
                  Save form
                </Button>
              </div>
            ) : null}
          </Card>
        </div>
        <div className="flex flex-col gap-6">
          <Card padding="compact" className="flex flex-col gap-4">
            <h2 className="type-title-sm text-fg">Preview</h2>
            <FormFieldsView fields={previewFields} values={preview} onChange={setPreview} />
            {consent ? <Checkbox checked={false} disabled label={consent} /> : null}
            <Button disabled className="self-start">
              {submitLabel || "Submit"}
            </Button>
          </Card>
          <Submissions formId={form.id} />
        </div>
      </div>

      <Dialog open={picking !== null} onOpenChange={(o) => !o && setPicking(null)} title={picking === "owner" ? "Owner of new contacts" : "Notify a person"} size="md">
        <MemberPicker
          orgId={orgId}
          label="Find a person"
          autoFocus
          onPick={(u) => {
            if (picking === "owner") setOwner(u);
            else if (!notify.some((x) => x.id === u.id)) setNotify([...notify, u]);
            setPicking(null);
          }}
        />
      </Dialog>
      <Dialog open={adding} onOpenChange={setAdding} title="Add a field" size="md">
        <ul className="flex max-h-96 flex-col gap-1 overflow-y-auto">
          {candidates.map(({ object, p }) => (
            <li key={`${object}.${p.key}`}>
              <button
                type="button"
                className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                onClick={() => {
                  setFields([...fields, { key: p.key, object, label: object === "company" && p.key === "name" ? "Company" : p.label, required: false }]);
                  setAdding(false);
                }}
              >
                <Badge>{object === "company" ? "Company" : "Contact"}</Badge>
                <span className="type-label text-fg">{p.label}</span>
              </button>
            </li>
          ))}
        </ul>
      </Dialog>
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        tone="danger"
        title={`Delete “${form.name}”?`}
        confirmLabel="Delete form"
        onConfirm={() =>
          change.mutateAsync({ op: "delete", id: form.id }).then(
            () => {
              toast.success("Form deleted");
              router.push("/crm/forms");
            },
            (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete it."))),
          )
        }
      >
        Its link and embeds stop working. Contacts it created stay.
      </ConfirmDialog>
    </div>
  );
}

function PublishedLinks({ url, embed }: { url: string; embed: string }) {
  const copy = (text: string, what: string) => void navigator.clipboard?.writeText(text).then(() => toast.success(`${what} copied`));
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <Input aria-label="Form link" readOnly value={url} onFocus={(e) => e.target.select()} wrapperClassName="flex-1" />
        <div className="flex gap-2">
          <Button variant="secondary" leftIcon={<Copy />} onClick={() => copy(url, "Link")}>
            Copy link
          </Button>
          <Button asChild variant="secondary" leftIcon={<ExternalLink />}>
            <a href={url} target="_blank" rel="noreferrer noopener">
              Open
            </a>
          </Button>
        </div>
      </div>
      {embed ? (
        <Field label="Embed code" info="Paste it into a page of your website: the form shows there in a frame." labelEnd={<Button variant="ghost" size="sm" leftIcon={<Copy />} onClick={() => copy(embed, "Embed code")}>Copy</Button>}>
          <TextArea readOnly value={embed} minRows={2} className="font-mono type-caption" onFocus={(e) => e.target.select()} />
        </Field>
      ) : null}
    </div>
  );
}

function Submissions({ formId }: { formId: string }) {
  const { orgId, settings } = useCrm();
  const subs = useCrmFormSubmissions(settings.can.manage_forms ? orgId : null, formId);
  if (!settings.can.manage_forms) return null;
  return (
    <Card padding="compact" className="flex flex-col gap-3">
      <h2 className="type-title-sm text-fg">Submissions</h2>
      {subs.isPending ? (
        <SkeletonGroup label="Loading submissions" className="flex flex-col gap-2">
          <Skeleton className="h-10 w-full" />
        </SkeletonGroup>
      ) : subs.isError ? (
        <ErrorState compact title="Couldn't load submissions" info={subs.error.message} onRetry={() => void subs.refetch()} />
      ) : subs.data.items.length === 0 ? (
        <EmptyState compact icon={<Inbox />} title="No submissions yet" />
      ) : (
        <>
          <ul className="flex flex-col divide-y divide-border">
            {subs.data.items.map((s) => (
              <li key={s.id} className="flex flex-col gap-0.5 py-2">
                <span className="flex flex-wrap items-center gap-2">
                  {s.contact ? (
                    <Link href={recordHref("contact", s.contact.id)} className="type-label font-semibold text-fg hover:underline">
                      {s.contact.name}
                    </Link>
                  ) : (
                    <span className="type-label text-fg-muted">A contact you can&apos;t open</span>
                  )}
                  <Badge tone={s.created ? "success" : "neutral"}>{s.created ? "New contact" : "Updated"}</Badge>
                  {s.consent ? <Badge>Consented</Badge> : null}
                </span>
                {Object.keys(s.values).length > 0 ? (
                  <span className="type-caption text-fg-muted">
                    {Object.entries(s.values)
                      .map(([k, v]) => `${k.replace(/^(contact|company)\./, "").replace(/_/g, " ")}: ${v}`)
                      .join(" · ")}
                  </span>
                ) : null}
                <time className="type-caption text-fg-subtle" dateTime={s.created_at}>
                  {formatDateTime(s.created_at)}
                  {s.page_url ? ` · ${s.page_url}` : ""}
                </time>
              </li>
            ))}
          </ul>
          <LoadMore hasNextPage={!!subs.hasNextPage} isFetchingNextPage={subs.isFetchingNextPage} fetchNextPage={() => subs.fetchNextPage()} />
        </>
      )}
    </Card>
  );
}

