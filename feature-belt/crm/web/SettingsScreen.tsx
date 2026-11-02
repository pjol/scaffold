"use client";

import {
  CRM_CUSTOM_TYPES,
  CRM_RECORD_ACCESS,
  CRM_TAG_TONES,
  CRM_TYPE_LABEL,
  errorMessage,
  hasErrorCode,
  useCrmCreatePipeline,
  useCrmCreateProperty,
  useCrmDeletePipeline,
  useCrmDeleteProperty,
  useCrmDeleteTag,
  useCrmPipelines,
  useCrmProperties,
  useCrmTags,
  useCrmUpdatePipeline,
  useCrmUpdateProperty,
  useCrmUpdateSettings,
  useCrmUpdateTag,
  type CrmCustomPropertyType,
  type CrmObjectType,
  type CrmOption,
  type CrmPipeline,
  type CrmProperty,
  type CrmRecordAccess,
  type CrmSourceMode,
  type CrmProvider,
  CRM_PROVIDERS,
  type CrmStageInput,
  type CrmTag,
  type CrmTagTone,
} from "@scaffoldapp/shared";
import {
  Badge,
  Button,
  Card,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Checkbox,
  InfoTip,
  ProviderIcon,
  sourcePlatformName,
  Input,
  Select,
  SegmentedControl,
  SkeletonGroup,
  SkeletonRow,
  cn,
  toast,
} from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowUp, Pencil, Plus, Tags, Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Section } from "@/components/shell/PageHeader";
import { APP_NAME } from "@/lib/app";
import { useUrlState } from "@/lib/url-state";
import { CrmFrame, useCrm } from "./CrmFrame";

const CURRENCIES = ["USD", "EUR", "GBP", "CAD", "AUD", "NZD", "CHF", "SEK", "NOK", "DKK", "JPY", "INR", "SGD", "BRL", "MXN", "ZAR"];

const TYPE_LABEL: Record<CrmCustomPropertyType, string> = {
  text: "Text",
  number: "Number",
  date: "Date",
  select: "Dropdown",
  multi_select: "Multiple choice",
  user: "Person",
  url: "Web address",
  email: "Email",
  phone: "Phone",
  currency: "Amount",
  checkbox: "Checkbox",
};

/** /crm/settings: record access, currency, pipelines and stages, custom properties, tags. */
export function CrmSettingsScreen() {
  return (
    <CrmFrame>
      <Settings />
    </CrmFrame>
  );
}

function Settings() {
  const { settings } = useCrm();
  const can = settings.can.manage_settings;
  return (
    <div className="flex flex-col gap-section">
      <SourcesSection can={can} />
      <AccessSection can={can} />
      <PipelinesSection can={can} />
      <PropertiesSection can={can} />
      <TagsSection can={can} />
    </div>
  );
}

const SOURCE_MODES: { value: CrmSourceMode; label: string; info: string }[] = [
  { value: "app", label: `${APP_NAME} CRM`, info: `Everyone works in ${APP_NAME}'s own CRM. Connected CRMs don't show here.` },
  {
    value: "connected",
    label: "Connected CRM",
    info: `People work in HubSpot or Salesforce from here, each through their own account, so the platform decides what they see. ${APP_NAME}'s own records are hidden.`,
  },
  { value: "both", label: "Both side by side", info: `${APP_NAME}'s records and each person's connected CRM, with a source filter on lists and the board.` },
];

/** Which CRM this workspace works in: the app's own, connected HubSpot/Salesforce, or both. */
function SourcesSection({ can }: { can: boolean }) {
  const { orgId, settings } = useCrm();
  const update = useCrmUpdateSettings(orgId);
  const save = (body: { source_mode?: CrmSourceMode; source_providers?: CrmProvider[] }) =>
    update.mutate(body, { onSuccess: () => toast.success("Saved"), onError: (e) => toast.error(errorMessage(e, "Couldn't save.")) });
  const providers = settings.source_providers ?? [];
  const mode = settings.source_mode ?? "app";
  return (
    <Section title="Connected CRMs" info="Records in HubSpot or Salesforce are read and changed live with each person's own connection; nothing from there is copied here unless someone imports it.">
      <div className="grid gap-3 md:grid-cols-3" role="radiogroup" aria-label="Which CRM">
        {SOURCE_MODES.map((o) => {
          const on = mode === o.value;
          return (
            <div
              key={o.value}
              className={cn(
                "relative flex min-h-tap items-center gap-2 rounded-lg border p-4 transition-colors",
                on ? "border-primary bg-primary-soft" : "border-border bg-surface",
                can && !on && "hover:border-border-strong",
              )}
            >
              <button
                type="button"
                role="radio"
                aria-checked={on}
                disabled={!can || update.isPending}
                onClick={() => !on && save({ source_mode: o.value })}
                className="flex min-w-0 flex-1 items-center gap-2 text-left type-label font-semibold text-fg after:absolute after:inset-0 after:rounded-lg after:content-[''] disabled:cursor-default"
              >
                <span className={cn("size-4 shrink-0 rounded-pill border-2", on ? "border-primary bg-primary" : "border-border-strong")} aria-hidden />
                {o.label}
              </button>
              <span className="relative z-10">
                <InfoTip content={o.info} label={`About ${o.label}`} />
              </span>
            </div>
          );
        })}
      </div>
      {mode !== "app" ? (
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-1 type-label font-semibold text-fg">Platforms people may connect</legend>
          <div className="flex flex-wrap gap-4">
            {CRM_PROVIDERS.map((p) => {
              const on = providers.includes(p);
              return (
                <Checkbox
                  key={p}
                  disabled={!can || update.isPending || (on && providers.length === 1)}
                  checked={on}
                  onCheckedChange={(c) => save({ source_providers: c ? [...providers, p] : providers.filter((x) => x !== p) })}
                  label={
                    <span className="inline-flex items-center gap-2">
                      <ProviderIcon provider={p} size={16} />
                      {sourcePlatformName(p)}
                    </span>
                  }
                />
              );
            })}
          </div>
        </fieldset>
      ) : null}
    </Section>
  );
}

function AccessSection({ can }: { can: boolean }) {
  const { orgId, settings } = useCrm();
  const update = useCrmUpdateSettings(orgId);
  const save = (body: { record_access?: CrmRecordAccess; currency?: string }) =>
    update.mutate(body, { onSuccess: () => toast.success("Saved"), onError: (e) => toast.error(errorMessage(e, "Couldn't save.")) });
  return (
    <Section title="Record access" info="Who sees and edits records by default. People can always share a record further from its ⋮ menu, and admins see everything.">
      <div className="grid gap-3 md:grid-cols-3" role="radiogroup" aria-label="Record access">
        {CRM_RECORD_ACCESS.map((o) => {
          const on = settings.record_access === o.value;
          return (
            <div
              key={o.value}
              className={cn(
                "relative flex min-h-tap items-center gap-2 rounded-lg border p-4 transition-colors",
                on ? "border-primary bg-primary-soft" : "border-border bg-surface",
                can && !on && "hover:border-border-strong",
              )}
            >
              <button
                type="button"
                role="radio"
                aria-checked={on}
                disabled={!can || update.isPending}
                onClick={() => !on && save({ record_access: o.value })}
                className="flex min-w-0 flex-1 items-center gap-2 text-left type-label font-semibold text-fg after:absolute after:inset-0 after:rounded-lg after:content-[''] disabled:cursor-default"
              >
                <span className={cn("size-4 shrink-0 rounded-pill border-2", on ? "border-primary bg-primary" : "border-border-strong")} aria-hidden />
                {o.label}
              </button>
              <span className="relative z-10">
                <InfoTip content={o.info} label={`About ${o.label}`} />
              </span>
            </div>
          );
        })}
      </div>
      <Field label="Default currency" info="New deals use it; amounts in other currencies keep their own." className="sm:w-64">
        <Select disabled={!can} value={settings.currency} onValueChange={(v) => save({ currency: v })} options={CURRENCIES.map((c) => ({ value: c, label: c }))} />
      </Field>
    </Section>
  );
}

// ---- pipelines ---------------------------------------------------------------------------

function PipelinesSection({ can }: { can: boolean }) {
  const { orgId } = useCrm();
  const list = useCrmPipelines(orgId);
  const [editing, setEditing] = useState<CrmPipeline | "new" | null>(null);
  return (
    <Section
      title="Pipelines"
      info="Stages in order, each with the chance a deal there closes. Won and lost stages close deals."
      action={
        can ? (
          <Button variant="secondary" size="sm" leftIcon={<Plus />} onClick={() => setEditing("new")}>
            New pipeline
          </Button>
        ) : undefined
      }
    >
      {list.isPending ? (
        <Card padding="none">
          <SkeletonGroup label="Loading pipelines">
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        </Card>
      ) : list.isError ? (
        <ErrorState compact title="Couldn't load pipelines" onRetry={() => void list.refetch()} />
      ) : (
        <div className="flex flex-col gap-3">
          {list.data.map((p) => (
            <Card key={p.id} padding="compact" className="flex flex-col gap-3">
              <div className="flex items-center gap-2">
                <h3 className="flex-1 type-title-sm text-fg">{p.name}</h3>
                {can ? (
                  <Button variant="ghost" size="sm" leftIcon={<Pencil />} onClick={() => setEditing(p)}>
                    Edit
                  </Button>
                ) : null}
              </div>
              <ol className="flex flex-wrap gap-2">
                {p.stages.map((s) => (
                  <li key={s.id}>
                    <Badge tone={s.outcome === "won" ? "success" : s.outcome === "lost" ? "danger" : "neutral"}>
                      {s.name} · {s.probability}%
                    </Badge>
                  </li>
                ))}
              </ol>
            </Card>
          ))}
        </div>
      )}
      {editing ? <PipelineDialog pipeline={editing === "new" ? null : editing} canDelete={(list.data?.length ?? 0) > 1} onClose={() => setEditing(null)} /> : null}
    </Section>
  );
}

function PipelineDialog({ pipeline, canDelete, onClose }: { pipeline: CrmPipeline | null; canDelete: boolean; onClose: () => void }) {
  const { orgId } = useCrm();
  const create = useCrmCreatePipeline(orgId);
  const update = useCrmUpdatePipeline(orgId);
  const del = useCrmDeletePipeline(orgId);
  const [name, setName] = useState(pipeline?.name ?? "");
  const [stages, setStages] = useState<CrmStageInput[]>(
    pipeline?.stages.map((s) => ({ id: s.id, name: s.name, probability: s.probability, outcome: s.outcome })) ?? [
      { name: "Qualified", probability: 10, outcome: "open" },
      { name: "Proposal", probability: 50, outcome: "open" },
      { name: "Won", probability: 100, outcome: "won" },
      { name: "Lost", probability: 0, outcome: "lost" },
    ],
  );
  const [moves, setMoves] = useState<Record<string, string>>({});
  const [blocked, setBlocked] = useState<{ stageId: string; count: number } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const setStage = (i: number, s: Partial<CrmStageInput>) => setStages(stages.map((x, j) => (j === i ? { ...x, ...s } : x)));
  const move = (i: number, d: -1 | 1) => {
    const j = i + d;
    if (j < 0 || j >= stages.length) return;
    const next = [...stages];
    [next[i], next[j]] = [next[j] as CrmStageInput, next[i] as CrmStageInput];
    setStages(next);
  };
  const save = () => {
    setError(null);
    const body = { name: name.trim(), stages, ...(Object.keys(moves).length ? { stage_moves: moves } : {}) };
    const done = { onSuccess: () => (toast.success("Pipeline saved"), onClose()), onError: (e: unknown) => onFail(e) };
    if (pipeline) update.mutate({ id: pipeline.id, ...body }, done);
    else create.mutate(body, done);
  };
  const onFail = (e: unknown) => {
    if (hasErrorCode(e, "stage_has_deals")) {
      const d = (e as { details?: { stage_id: string; deal_count: number } }).details;
      if (d) setBlocked({ stageId: d.stage_id, count: d.deal_count });
    }
    setError(errorMessage(e, "Couldn't save the pipeline."));
  };
  const removed = pipeline?.stages.find((s) => s.id === blocked?.stageId);
  const keptIds = stages.filter((s) => s.id).map((s) => s.id as string);
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={pipeline ? `Edit ${pipeline.name}` : "New pipeline"}
      size="lg"
      footer={
        <>
          {pipeline && canDelete ? (
            <Button variant="danger-ghost" className="sm:mr-auto" onClick={() => setDeleting(true)}>
              Delete pipeline
            </Button>
          ) : null}
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button loading={create.isPending || update.isPending} onClick={save}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Name" required>
          <Input value={name} maxLength={80} onChange={(e) => setName(e.target.value)} placeholder="Partnerships" />
        </Field>
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-2 type-label font-semibold text-fg">Stages</legend>
          {stages.map((s, i) => (
            <div key={s.id ?? `new-${i}`} className="flex flex-wrap items-center gap-2 rounded-md bg-sunken p-2">
              <Input aria-label={`Stage ${i + 1} name`} value={s.name} maxLength={60} onChange={(e) => setStage(i, { name: e.target.value })} wrapperClassName="min-w-40 flex-1" />
              <Select
                aria-label={`Stage ${i + 1} outcome`}
                className="w-28"
                value={s.outcome}
                onValueChange={(v) => setStage(i, { outcome: v as CrmStageInput["outcome"], probability: v === "won" ? 100 : v === "lost" ? 0 : s.probability })}
                options={[
                  { value: "open", label: "Open" },
                  { value: "won", label: "Won" },
                  { value: "lost", label: "Lost" },
                ]}
              />
              <Input
                aria-label={`Stage ${i + 1} probability`}
                type="number"
                min={0}
                max={100}
                disabled={s.outcome !== "open"}
                value={s.probability}
                onChange={(e) => setStage(i, { probability: Math.max(0, Math.min(100, Number(e.target.value))) })}
                trailing={<span className="px-2 type-caption">%</span>}
                wrapperClassName="w-28"
              />
              <IconButton label="Move up" size="sm" icon={<ArrowUp />} disabled={i === 0} onClick={() => move(i, -1)} />
              <IconButton label="Move down" size="sm" icon={<ArrowDown />} disabled={i === stages.length - 1} onClick={() => move(i, 1)} />
              <IconButton label={`Remove ${s.name || "stage"}`} size="sm" icon={<X />} disabled={stages.length <= 1} onClick={() => setStages(stages.filter((_, j) => j !== i))} />
            </div>
          ))}
          <Button variant="ghost" size="sm" leftIcon={<Plus />} className="self-start" onClick={() => setStages([...stages, { name: "", probability: 50, outcome: "open" }])}>
            Add stage
          </Button>
        </fieldset>
        {blocked && removed ? (
          <Field label={`Move the ${blocked.count} deals in “${removed.name}” to`}>
            <Select
              value={moves[blocked.stageId]}
              onValueChange={(v) => setMoves({ ...moves, [blocked.stageId]: v })}
              placeholder="Choose a stage"
              options={stages.filter((s) => s.id && keptIds.includes(s.id)).map((s) => ({ value: s.id as string, label: s.name }))}
            />
          </Field>
        ) : null}
        {error ? (
          <p role="alert" className="type-label text-danger-strong">
            {error}
          </p>
        ) : null}
      </div>
      {pipeline ? (
        <ConfirmDialog
          open={deleting}
          onOpenChange={setDeleting}
          tone="danger"
          title={`Delete ${pipeline.name}?`}
          confirmLabel="Delete pipeline"
          onConfirm={() =>
            del.mutateAsync(pipeline.id).then(
              () => {
                toast.success("Pipeline deleted");
                onClose();
              },
              (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete the pipeline."))),
            )
          }
        >
          Only a pipeline without deals can be deleted.
        </ConfirmDialog>
      ) : null}
    </Dialog>
  );
}

// ---- properties -------------------------------------------------------------------------

function PropertiesSection({ can }: { can: boolean }) {
  const { orgId } = useCrm();
  const [type, setType] = useUrlState<CrmObjectType>("object", "contact");
  const props = useCrmProperties(orgId, type);
  const [editing, setEditing] = useState<CrmProperty | "new" | null>(null);
  const [deleting, setDeleting] = useState<CrmProperty | null>(null);
  const del = useCrmDeleteProperty(orgId);
  const custom = (props.data ?? []).filter((p) => !p.builtin);
  return (
    <Section
      title="Custom properties"
      info="Fields your team adds to records, shown on record pages, in columns, filters and imports."
      action={
        can ? (
          <Button variant="secondary" size="sm" leftIcon={<Plus />} onClick={() => setEditing("new")}>
            New property
          </Button>
        ) : undefined
      }
    >
      <SegmentedControl<CrmObjectType>
        aria-label="Object"
        className="sm:max-w-md"
        value={type}
        onValueChange={setType}
        options={[
          { value: "contact", label: "Contacts" },
          { value: "company", label: "Companies" },
          { value: "deal", label: "Deals" },
        ]}
      />
      <Card padding="none" className="p-1">
        {props.isPending ? (
          <SkeletonGroup label="Loading properties">
            <SkeletonRow leading={false} />
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        ) : props.isError ? (
          <ErrorState compact title="Couldn't load properties" onRetry={() => void props.refetch()} />
        ) : custom.length === 0 ? (
          <EmptyState compact icon={<Tags />} title={`No custom ${CRM_TYPE_LABEL[type].one} properties`} />
        ) : (
          <ul className="flex flex-col">
            {custom.map((p) => (
              <li key={p.key} className="flex min-h-row items-center gap-3 px-3 py-2">
                <span className="min-w-0 flex-1 truncate type-label font-semibold text-fg">{p.label}</span>
                <span className="hidden type-caption text-fg-muted sm:inline">{p.key}</span>
                <Badge>{TYPE_LABEL[p.type as CrmCustomPropertyType] ?? p.type}</Badge>
                {can ? (
                  <>
                    <IconButton label={`Edit ${p.label}`} size="sm" icon={<Pencil />} onClick={() => setEditing(p)} />
                    <IconButton label={`Delete ${p.label}`} size="sm" icon={<Trash2 />} onClick={() => setDeleting(p)} />
                  </>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </Card>
      {editing ? <PropertyDialog type={type} property={editing === "new" ? null : editing} onClose={() => setEditing(null)} /> : null}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
        tone="danger"
        title={`Delete “${deleting?.label}”?`}
        confirmLabel="Delete property"
        onConfirm={() =>
          del.mutateAsync(deleting?.id ?? "").then(
            () => toast.success("Property deleted"),
            (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete it."))),
          )
        }
      >
        Its values are removed from every {CRM_TYPE_LABEL[type].one}. It can&apos;t be undone.
      </ConfirmDialog>
    </Section>
  );
}

function PropertyDialog({ type, property, onClose }: { type: CrmObjectType; property: CrmProperty | null; onClose: () => void }) {
  const { orgId } = useCrm();
  const create = useCrmCreateProperty(orgId);
  const update = useCrmUpdateProperty(orgId);
  const [label, setLabel] = useState(property?.label ?? "");
  const [ptype, setPtype] = useState<CrmCustomPropertyType>((property?.type as CrmCustomPropertyType) ?? "text");
  const [options, setOptions] = useState<CrmOption[]>(property?.options ?? [{ value: "", label: "" }]);
  const [error, setError] = useState<string | null>(null);
  const hasOptions = ptype === "select" || ptype === "multi_select";
  const save = () => {
    if (!label.trim()) return setError("Give the property a label.");
    const opts = options.filter((o) => o.label.trim());
    if (hasOptions && opts.length === 0) return setError("Add at least one option.");
    setError(null);
    const done = { onSuccess: () => (toast.success("Property saved"), onClose()), onError: (e: unknown) => setError(errorMessage(e, "Couldn't save the property.")) };
    if (property?.id) update.mutate({ id: property.id, label: label.trim(), ...(hasOptions ? { options: opts } : {}) }, done);
    else create.mutate({ object_type: type, label: label.trim(), type: ptype, ...(hasOptions ? { options: opts } : {}) }, done);
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={property ? `Edit ${property.label}` : `New ${CRM_TYPE_LABEL[type].one} property`}
      size="md"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button loading={create.isPending || update.isPending} onClick={save}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Label" required error={error}>
          <Input value={label} maxLength={60} autoFocus onChange={(e) => setLabel(e.target.value)} placeholder="Renewal date" />
        </Field>
        <Field label="Type" info={property ? "A property's type can't change after it's created." : undefined}>
          <Select disabled={!!property} value={ptype} onValueChange={(v) => setPtype(v as CrmCustomPropertyType)} options={CRM_CUSTOM_TYPES.map((t) => ({ value: t, label: TYPE_LABEL[t] }))} />
        </Field>
        {hasOptions ? (
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-2 type-label font-semibold text-fg">Options</legend>
            {options.map((o, i) => (
              <div key={i} className="flex items-center gap-2">
                <Input aria-label={`Option ${i + 1}`} value={o.label} maxLength={80} onChange={(e) => setOptions(options.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)))} wrapperClassName="flex-1" />
                <IconButton label="Remove option" size="sm" icon={<X />} onClick={() => setOptions(options.filter((_, j) => j !== i))} />
              </div>
            ))}
            <Button variant="ghost" size="sm" leftIcon={<Plus />} className="self-start" onClick={() => setOptions([...options, { value: "", label: "" }])}>
              Add option
            </Button>
          </fieldset>
        ) : null}
      </div>
    </Dialog>
  );
}

// ---- tags -----------------------------------------------------------------------------------

function TagsSection({ can }: { can: boolean }) {
  const { orgId } = useCrm();
  const tags = useCrmTags(orgId);
  const [editing, setEditing] = useState<CrmTag | null>(null);
  return (
    <Section title="Tags" info="Anyone can add tags while tagging a record. Renaming or deleting one changes every record.">
      <Card padding="compact">
        {tags.isPending ? (
          <SkeletonGroup label="Loading tags">
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        ) : tags.isError ? (
          <ErrorState compact title="Couldn't load tags" onRetry={() => void tags.refetch()} />
        ) : tags.data.length === 0 ? (
          <EmptyState compact icon={<Tags />} title="No tags yet" />
        ) : (
          <ul className="flex flex-wrap gap-2">
            {tags.data.map((t) => (
              <li key={t.id}>
                {can ? (
                  <button type="button" onClick={() => setEditing(t)} aria-label={`Edit tag ${t.name}`} className="rounded-sm">
                    <Badge tone={t.tone} size="md">
                      {t.name}
                    </Badge>
                  </button>
                ) : (
                  <Badge tone={t.tone} size="md">
                    {t.name}
                  </Badge>
                )}
              </li>
            ))}
          </ul>
        )}
      </Card>
      {editing ? <TagDialog tag={editing} onClose={() => setEditing(null)} /> : null}
    </Section>
  );
}

function TagDialog({ tag, onClose }: { tag: CrmTag; onClose: () => void }) {
  const { orgId } = useCrm();
  const update = useCrmUpdateTag(orgId);
  const del = useCrmDeleteTag(orgId);
  const [name, setName] = useState(tag.name);
  const [tone, setTone] = useState<CrmTagTone>(tag.tone);
  const [deleting, setDeleting] = useState(false);
  useEffect(() => {
    setName(tag.name);
    setTone(tag.tone);
  }, [tag]);
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title="Edit tag"
      size="sm"
      footer={
        <>
          <Button variant="danger-ghost" className="sm:mr-auto" onClick={() => setDeleting(true)}>
            Delete
          </Button>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button
            loading={update.isPending}
            onClick={() =>
              update.mutate(
                { id: tag.id, name: name.trim(), tone },
                { onSuccess: () => (toast.success("Tag saved"), onClose()), onError: (e) => toast.error(errorMessage(e, "Couldn't save the tag.")) },
              )
            }
          >
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Name">
          <Input value={name} maxLength={40} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Colour">
          <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Colour">
            {CRM_TAG_TONES.map((t) => (
              <button key={t} type="button" role="radio" aria-checked={tone === t} aria-label={t} onClick={() => setTone(t)} className={cn("rounded-sm p-0.5", tone === t && "ring-2 ring-ring")}>
                <Badge tone={t} size="md">
                  {name || tag.name}
                </Badge>
              </button>
            ))}
          </div>
        </Field>
      </div>
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        tone="danger"
        title={`Delete the tag “${tag.name}”?`}
        confirmLabel="Delete tag"
        onConfirm={() =>
          del.mutateAsync(tag.id).then(
            () => {
              toast.success("Tag deleted");
              onClose();
            },
            (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete the tag."))),
          )
        }
      >
        It comes off every record.
      </ConfirmDialog>
    </Dialog>
  );
}
