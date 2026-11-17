"use client";

import {
  CRM_OBJECT_TYPES,
  CRM_PRIORITY_LABEL,
  CRM_TYPE_LABEL,
  crmFilterReady,
  errorMessage,
  useAccessTeams,
  useCrmChangeWorkflow,
  useCrmPipelines,
  useCrmProperties,
  useCrmTags,
  useCrmWorkflow,
  useCrmWorkflowRuns,
  type CrmFilter,
  type CrmObjectType,
  type CrmPriority,
  type CrmProperty,
  type CrmUserRef,
  type CrmWorkflow,
  type CrmWorkflowAction,
  type CrmWorkflowActionType,
  type CrmWorkflowTrigger,
  type CrmWorkflowTriggerType,
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
  InfoTip,
  Input,
  Select,
  Skeleton,
  SkeletonGroup,
  Switch,
  TextArea,
  toast,
} from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowLeft, ArrowUp, Copy, History, Plus, Trash2, Workflow, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { formatDateTime } from "@/lib/format";
import { ConditionsEditor } from "./Conditions";
import { CrmFrame, useCrm } from "./CrmFrame";
import { MemberPicker, PersonChip, pipelinesFor, recordHref } from "./parts";

const ACTION_LABEL: Record<CrmWorkflowActionType, string> = {
  delay: "Wait",
  set_property: "Set a property",
  create_task: "Create a task",
  rotate_owner: "Rotate the owner",
  notify: "Notify people",
  add_tag: "Add a tag",
  create_deal: "Create a deal",
  create_ticket: "Create a ticket",
  webhook: "Send to a webhook",
};

const TRIGGER_LABEL: Record<CrmWorkflowTriggerType, string> = {
  created: "A record is created",
  property_changed: "A property changes",
  stage_changed: "The stage changes",
  date_reached: "A date is reached",
};

const VALUE_TYPES = new Set(["text", "long_text", "number", "date", "select", "checkbox", "url", "email", "phone", "currency"]);

/** /crm/workflows/[id] (and /new): trigger, conditions, steps, switches and the run log. */
export function CrmWorkflowEditorScreen({ id }: { id: string }) {
  return (
    <CrmFrame>
      {id === "new" ? <Editor workflow={null} /> : <Loaded id={id} />}
    </CrmFrame>
  );
}

function Loaded({ id }: { id: string }) {
  const { orgId } = useCrm();
  const wf = useCrmWorkflow(orgId, id);
  if (wf.isPending) {
    return (
      <SkeletonGroup label="Loading the workflow" className="flex flex-col gap-6">
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-96 w-full" />
      </SkeletonGroup>
    );
  }
  if (wf.isError) {
    return wf.error.status === 404 ? (
      <Card padding="compact">
        <EmptyState icon={<Workflow />} title="Workflow not found" />
      </Card>
    ) : (
      <ErrorState title="Couldn't load the workflow" info={wf.error.message} onRetry={() => void wf.refetch()} retrying={wf.isRefetching} />
    );
  }
  return <Editor key={wf.data.updated_at} workflow={wf.data} />;
}

function Editor({ workflow }: { workflow: CrmWorkflow | null }) {
  const { orgId, settings } = useCrm();
  const router = useRouter();
  const manage = !!settings.can.manage_workflows;
  const change = useCrmChangeWorkflow(orgId);
  const [name, setName] = useState(workflow?.name ?? "");
  const [object, setObject] = useState<CrmObjectType>(workflow?.object_type ?? "contact");
  const [trigger, setTrigger] = useState<CrmWorkflowTrigger>(workflow?.trigger ?? { type: "created" });
  const [conditions, setConditions] = useState<CrmFilter[]>(workflow?.conditions ?? []);
  const [actions, setActions] = useState<CrmWorkflowAction[]>(workflow?.actions ?? [{ type: "create_task", title: "Follow up with {{record.name}}", due_days: 1, priority: "none" }]);
  const [reenroll, setReenroll] = useState(workflow?.reenroll ?? false);
  const [enabled, setEnabled] = useState(workflow?.enabled ?? false);
  const [error, setError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const props = useCrmProperties(orgId, object).data ?? [];

  const save = () => {
    if (!name.trim()) return setError("Name the workflow.");
    setError(null);
    const body = { name: name.trim(), trigger, conditions: conditions.filter(crmFilterReady), actions, reenroll, enabled, ...(workflow ? {} : { object_type: object }) };
    change.mutate(
      { op: "save", ...(workflow ? { id: workflow.id } : {}), body },
      {
        onSuccess: (w) => {
          toast.success(enabled ? "Saved: it runs as you" : "Saved");
          if (!workflow && w) router.replace(`/crm/workflows/${w.id}`);
        },
        onError: (e) => setError(errorMessage(e, "Couldn't save the workflow.")),
      },
    );
  };
  const setAction = (i: number, a: CrmWorkflowAction) => setActions(actions.map((x, j) => (j === i ? a : x)));
  const moveAction = (i: number, d: -1 | 1) => {
    const j = i + d;
    if (j < 0 || j >= actions.length) return;
    const next = [...actions];
    [next[i], next[j]] = [next[j] as CrmWorkflowAction, next[i] as CrmWorkflowAction];
    setActions(next);
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <Link href="/crm/workflows" className="inline-flex w-fit items-center gap-1 type-label text-fg-muted hover:text-fg hover:underline">
            <ArrowLeft className="size-4" aria-hidden />
            Workflows
          </Link>
          <h2 className="type-headline break-words text-fg">{workflow ? workflow.name : "New workflow"}</h2>
          {workflow ? (
            <span className="flex flex-wrap items-center gap-2 type-label text-fg-muted">
              {workflow.enabled ? (
                <Badge tone="success" dot>
                  On
                </Badge>
              ) : (
                <Badge>Off</Badge>
              )}
              {workflow.enabled && workflow.owner ? <span>Runs as {workflow.owner.name}</span> : null}
              {workflow.stats ? (
                <span>
                  {workflow.stats.enrolled} enrolled · {workflow.stats.waiting} waiting · {workflow.stats.failed} failed
                </span>
              ) : null}
            </span>
          ) : null}
        </div>
        {workflow && manage ? (
          <ResourceMenu feature="crm" title={workflow.name} orgId={orgId} actions={[{ id: "crm-wf-delete", label: "Delete workflow", icon: Trash2, destructive: true, onSelect: () => setDeleting(true) }]} />
        ) : null}
      </div>
      {!manage ? <Banner tone="info">People with the “Manage CRM workflows” permission change workflows.</Banner> : null}

      <fieldset disabled={!manage} className="grid items-start gap-6 lg:grid-cols-2">
        <div className="flex flex-col gap-6">
          <Card padding="compact" className="flex flex-col gap-4">
            <h3 className="type-title-sm text-fg">When</h3>
            <Field label="Name" required error={error}>
              <Input value={name} maxLength={120} onChange={(e) => setName(e.target.value)} placeholder="Route new leads" />
            </Field>
            <Field label="Records" info={workflow ? "A workflow's record type can't change." : undefined}>
              <Select
                value={object}
                disabled={!!workflow}
                onValueChange={(v) => {
                  setObject(v as CrmObjectType);
                  setTrigger({ type: "created" });
                  setConditions([]);
                }}
                options={CRM_OBJECT_TYPES.map((t) => ({ value: t, label: CRM_TYPE_LABEL[t].Many }))}
              />
            </Field>
            <TriggerEditor object={object} props={props} trigger={trigger} onChange={setTrigger} />
          </Card>
          <Card padding="compact" className="flex flex-col gap-3">
            <h3 className="flex items-center gap-2 type-title-sm text-fg">
              Only if
              <InfoTip label="About conditions" content="Records must match every condition, and the person it runs as must be able to open them." />
            </h3>
            <ConditionsEditor key={object} type={object} filters={conditions} onChange={setConditions} emptyText={`Every ${CRM_TYPE_LABEL[object].one} that triggers it.`} />
          </Card>
          <Card padding="compact" className="flex flex-col gap-3">
            <h3 className="type-title-sm text-fg">Switches</h3>
            <Switch checked={reenroll} onCheckedChange={setReenroll} label="Records can run it again" info="Off: a record goes through once. On: again when it triggers once more, at most once an hour (date triggers once per date)." />
            <Switch
              checked={enabled}
              onCheckedChange={setEnabled}
              label="On"
              info="It runs as whoever turns it on (or changes it while it's on), with exactly their access: records they can't open aren't enrolled, and steps they couldn't do by hand fail in the run log."
            />
            {workflow?.secret ? <SecretField secret={workflow.secret} /> : null}
          </Card>
        </div>
        <Card padding="compact" className="flex flex-col gap-3">
          <h3 className="type-title-sm text-fg">Then</h3>
          <ol className="flex flex-col gap-3">
            {actions.map((a, i) => (
              <li key={i} className="flex flex-col gap-3 border-t border-border pt-3 first:border-t-0 first:pt-0">
                <div className="flex items-center gap-2">
                  <span className="flex size-6 shrink-0 items-center justify-center rounded-pill bg-sunken type-caption font-semibold text-fg-muted">{i + 1}</span>
                  <Select
                    aria-label={`Step ${i + 1}`}
                    className="flex-1"
                    value={a.type}
                    onValueChange={(v) => setAction(i, defaultAction(v as CrmWorkflowActionType))}
                    options={(Object.keys(ACTION_LABEL) as CrmWorkflowActionType[]).map((k) => ({ value: k, label: ACTION_LABEL[k] }))}
                  />
                  <IconButton label="Move up" size="sm" icon={<ArrowUp />} disabled={i === 0} onClick={() => moveAction(i, -1)} />
                  <IconButton label="Move down" size="sm" icon={<ArrowDown />} disabled={i === actions.length - 1} onClick={() => moveAction(i, 1)} />
                  <IconButton label={`Remove step ${i + 1}`} size="sm" icon={<X />} disabled={actions.length <= 1} onClick={() => setActions(actions.filter((_, j) => j !== i))} />
                </div>
                <ActionFields object={object} props={props} action={a} onChange={(x) => setAction(i, x)} />
              </li>
            ))}
          </ol>
          <Button variant="secondary" size="sm" leftIcon={<Plus />} className="self-start" disabled={actions.length >= 20} onClick={() => setActions([...actions, defaultAction("delay")])}>
            Add step
          </Button>
        </Card>
      </fieldset>
      {manage ? (
        <div className="flex justify-end">
          <Button loading={change.isPending} onClick={save}>
            {workflow ? "Save workflow" : "Make workflow"}
          </Button>
        </div>
      ) : null}
      {workflow && manage ? <Runs workflowId={workflow.id} /> : null}
      {workflow ? (
        <ConfirmDialog
          open={deleting}
          onOpenChange={setDeleting}
          tone="danger"
          title={`Delete “${workflow.name}”?`}
          confirmLabel="Delete workflow"
          onConfirm={() =>
            change.mutateAsync({ op: "delete", id: workflow.id }).then(
              () => {
                toast.success("Workflow deleted");
                router.push("/crm/workflows");
              },
              (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete it."))),
            )
          }
        >
          Waiting runs stop. What it already changed stays.
        </ConfirmDialog>
      ) : null}
    </div>
  );
}

function defaultAction(type: CrmWorkflowActionType): CrmWorkflowAction {
  switch (type) {
    case "delay":
      return { type, minutes: 24 * 60 };
    case "create_task":
      return { type, title: "Follow up with {{record.name}}", due_days: 1, priority: "none" };
    case "notify":
      return { type, notify_owner: true, user_ids: [], body: "{{record.name}} needs a look" };
    case "create_deal":
      return { type, title: "{{record.name}}: new deal" };
    case "create_ticket":
      return { type, title: "{{record.name}}: follow-up" };
    default:
      return { type };
  }
}

function TriggerEditor({ object, props, trigger: t, onChange }: { object: CrmObjectType; props: CrmProperty[]; trigger: CrmWorkflowTrigger; onChange: (t: CrmWorkflowTrigger) => void }) {
  const { orgId } = useCrm();
  const pipelines = object === "deal" || object === "ticket" ? pipelinesFor(useCrmPipelines(orgId).data, object) : [];
  const types = (Object.keys(TRIGGER_LABEL) as CrmWorkflowTriggerType[]).filter((k) => k !== "stage_changed" || object === "deal" || object === "ticket");
  const editable = props.filter((p) => p.editable);
  const dates = props.filter((p) => p.type === "date" || p.type === "datetime");
  return (
    <>
      <Field label="Trigger">
        <Select value={t.type} onValueChange={(v) => onChange({ type: v as CrmWorkflowTriggerType })} options={types.map((k) => ({ value: k, label: TRIGGER_LABEL[k] }))} />
      </Field>
      {t.type === "property_changed" ? (
        <Field label="Property">
          <Select value={t.property || undefined} placeholder="Choose" onValueChange={(v) => onChange({ ...t, property: v })} options={editable.map((p) => ({ value: p.key, label: p.label }))} />
        </Field>
      ) : null}
      {t.type === "stage_changed" ? (
        <Field label="Into">
          <Select
            value={t.stage_id ?? "any"}
            onValueChange={(v) => onChange(v === "any" ? { type: t.type } : { ...t, stage_id: v })}
            options={[{ value: "any", label: "Any stage" }, ...pipelines.flatMap((p) => p.stages.map((s) => ({ value: s.id, label: pipelines.length > 1 ? `${p.name} · ${s.name}` : s.name })))]}
          />
        </Field>
      ) : null}
      {t.type === "date_reached" ? (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Date">
            <Select value={t.property || undefined} placeholder="Choose" onValueChange={(v) => onChange({ ...t, property: v })} options={dates.map((p) => ({ value: p.key, label: p.label }))} />
          </Field>
          <Field label="Days after" info="Negative runs before the date: -3 is three days before. Checked hourly, in UTC.">
            <Input type="number" min={-365} max={365} value={t.offset_days ?? 0} onChange={(e) => onChange({ ...t, offset_days: Math.max(-365, Math.min(365, Number(e.target.value) || 0)) })} />
          </Field>
        </div>
      ) : null}
    </>
  );
}

function ActionFields({ object, props, action: a, onChange }: { object: CrmObjectType; props: CrmProperty[]; action: CrmWorkflowAction; onChange: (a: CrmWorkflowAction) => void }) {
  const { orgId } = useCrm();
  const tags = useCrmTags(orgId).data ?? [];
  const teams = useAccessTeams(orgId).data?.items ?? [];
  const allPipelines = useCrmPipelines(orgId).data;
  const [picking, setPicking] = useState(false);
  const [people, setPeople] = useState<CrmUserRef[]>([]);
  switch (a.type) {
    case "delay": {
      const m = a.minutes ?? 60;
      const unit = m % 1440 === 0 ? "days" : m % 60 === 0 ? "hours" : "minutes";
      const n = unit === "days" ? m / 1440 : unit === "hours" ? m / 60 : m;
      const per = { minutes: 1, hours: 60, days: 1440 } as const;
      return (
        <div className="flex gap-2">
          <Input aria-label="Wait for" type="number" min={1} value={n} onChange={(e) => onChange({ ...a, minutes: Math.max(1, Number(e.target.value) || 1) * per[unit] })} wrapperClassName="w-28" />
          <Select
            aria-label="Unit"
            className="w-32"
            value={unit}
            onValueChange={(v) => onChange({ ...a, minutes: n * per[v as keyof typeof per] })}
            options={[
              { value: "minutes", label: "minutes" },
              { value: "hours", label: "hours" },
              { value: "days", label: "days" },
            ]}
          />
        </div>
      );
    }
    case "set_property": {
      const settable = props.filter((p) => p.editable && VALUE_TYPES.has(p.type) && p.key !== "owner" && p.key !== "team");
      const p = settable.find((x) => x.key === a.property);
      return (
        <div className="grid gap-3 sm:grid-cols-2">
          <Select aria-label="Property" value={a.property || undefined} placeholder="Property" onValueChange={(v) => onChange({ ...a, property: v, value: null })} options={settable.map((x) => ({ value: x.key, label: x.label }))} />
          {p ? <ValueInput prop={p} value={a.value} onChange={(v) => onChange({ ...a, value: v })} /> : null}
        </div>
      );
    }
    case "create_task":
      return (
        <div className="flex flex-col gap-3">
          <Input aria-label="Task title" value={a.title ?? ""} maxLength={200} onChange={(e) => onChange({ ...a, title: e.target.value })} placeholder="Call {{record.name}}" />
          <TextArea aria-label="Notes" value={a.body ?? ""} minRows={2} onChange={(e) => onChange({ ...a, body: e.target.value })} placeholder="Notes" />
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Due after (days)">
              <Input type="number" min={0} max={365} value={a.due_days ?? 0} onChange={(e) => onChange({ ...a, due_days: Math.max(0, Math.min(365, Number(e.target.value) || 0)) })} />
            </Field>
            <Field label="Priority">
              <Select value={a.priority ?? "none"} onValueChange={(v) => onChange({ ...a, priority: v as CrmPriority })} options={(Object.keys(CRM_PRIORITY_LABEL) as CrmPriority[]).map((k) => ({ value: k, label: CRM_PRIORITY_LABEL[k] }))} />
            </Field>
          </div>
          <p className="type-caption text-fg-muted">Assigned to the record&apos;s owner.</p>
        </div>
      );
    case "rotate_owner":
      return (
        <Field label="Among the members of" info="Round robin: each enrolled record goes to the next member, in a fixed order.">
          <Select value={a.team_id || undefined} placeholder="Choose a team" onValueChange={(v) => onChange({ ...a, team_id: v })} options={teams.map((t) => ({ value: t.id, label: t.name }))} />
        </Field>
      );
    case "notify":
      return (
        <div className="flex flex-col gap-3">
          <Input aria-label="Message" value={a.body ?? ""} maxLength={300} onChange={(e) => onChange({ ...a, body: e.target.value })} placeholder="{{record.name}} needs a look" />
          <Checkbox checked={!!a.notify_owner} onCheckedChange={(v) => onChange({ ...a, notify_owner: v === true })} label="The record's owner" />
          <div className="flex flex-wrap items-center gap-2">
            {(a.user_ids ?? []).map((id) => {
              const u = people.find((x) => x.id === id);
              return (
                <span key={id} className="inline-flex items-center gap-1 rounded-pill bg-sunken pl-2 pr-1 type-label">
                  {u ? <PersonChip user={u} /> : "A member"}
                  <IconButton label="Remove" size="sm" icon={<X />} onClick={() => onChange({ ...a, user_ids: (a.user_ids ?? []).filter((x) => x !== id) })} />
                </span>
              );
            })}
            <Button variant="ghost" size="sm" leftIcon={<Plus />} onClick={() => setPicking(true)}>
              Add person
            </Button>
          </div>
          <Dialog open={picking} onOpenChange={setPicking} title="Notify a person" size="md">
            <MemberPicker
              orgId={orgId}
              label="Find a person"
              autoFocus
              onPick={(u) => {
                setPeople([...people, u]);
                if (!(a.user_ids ?? []).includes(u.id)) onChange({ ...a, user_ids: [...(a.user_ids ?? []), u.id] });
                setPicking(false);
              }}
            />
          </Dialog>
          <p className="type-caption text-fg-muted">Only people who can open the record are notified.</p>
        </div>
      );
    case "add_tag":
      return <Select aria-label="Tag" value={a.tag_id || undefined} placeholder="Choose a tag" onValueChange={(v) => onChange({ ...a, tag_id: v })} options={tags.map((t) => ({ value: t.id, label: t.name }))} />;
    case "create_deal":
    case "create_ticket": {
      const target = a.type === "create_deal" ? "deal" : "ticket";
      const pls = pipelinesFor(allPipelines, target);
      return (
        <div className="flex flex-col gap-3">
          <Input aria-label="Name" value={a.title ?? ""} maxLength={200} onChange={(e) => onChange({ ...a, title: e.target.value })} />
          <Select
            aria-label="Stage"
            value={a.stage_id ?? "first"}
            onValueChange={(v) => onChange(v === "first" ? { ...a, stage_id: undefined } : { ...a, stage_id: v })}
            options={[{ value: "first", label: "The first open stage" }, ...pls.flatMap((p) => p.stages.filter((s) => s.outcome === "open").map((s) => ({ value: s.id, label: pls.length > 1 ? `${p.name} · ${s.name}` : s.name })))]}
          />
          <p className="type-caption text-fg-muted">
            {"{{record.name}}"} and {"{{record.owner}}"} fill in. It&apos;s linked to this {CRM_TYPE_LABEL[object].one}
            {object === "contact" ? " and its company" : ""}.
          </p>
        </div>
      );
    }
    case "webhook":
      return (
        <Field label="URL" info="A public https address. The record goes as JSON in a POST signed with the workflow's secret (shown under Switches once saved).">
          <Input type="url" value={a.url ?? ""} onChange={(e) => onChange({ ...a, url: e.target.value })} placeholder="https://hooks.example.com/crm" />
        </Field>
      );
  }
}

function ValueInput({ prop, value, onChange }: { prop: CrmProperty; value: unknown; onChange: (v: unknown) => void }) {
  if (prop.type === "select") {
    return <Select aria-label={prop.label} value={typeof value === "string" ? value : undefined} placeholder="Value" onValueChange={onChange} options={prop.options} />;
  }
  if (prop.type === "checkbox") return <Checkbox checked={value === true} onCheckedChange={(v) => onChange(v === true)} label={prop.label} />;
  const numeric = prop.type === "number" || prop.type === "currency";
  return (
    <Input
      aria-label={prop.label}
      type={prop.type === "date" ? "date" : numeric ? "number" : "text"}
      value={value === null || value === undefined ? "" : String(value)}
      onChange={(e) => onChange(numeric ? (e.target.value === "" ? null : Number(e.target.value)) : e.target.value)}
      placeholder={prop.type === "currency" ? "Minor units (cents)" : "Value (empty clears it)"}
    />
  );
}

function SecretField({ secret }: { secret: string }) {
  return (
    <Field label="Webhook secret" info="Webhook steps sign each POST: X-CRM-Signature: t=<unix time>,v1=<hex HMAC-SHA256 of “t.body” with this secret>. Check it, and the time, before trusting a call.">
      <div className="flex gap-2">
        <Input readOnly value={secret} className="font-mono" onFocus={(e) => e.target.select()} wrapperClassName="flex-1" />
        <IconButton label="Copy the secret" icon={<Copy />} onClick={() => void navigator.clipboard?.writeText(secret).then(() => toast.success("Secret copied"))} />
      </div>
    </Field>
  );
}

function Runs({ workflowId }: { workflowId: string }) {
  const { orgId } = useCrm();
  const runs = useCrmWorkflowRuns(orgId, workflowId);
  return (
    <section className="flex flex-col gap-3" aria-label="Runs">
      <h3 className="flex items-center gap-2 type-title-sm text-fg">
        <History className="size-5 text-fg-muted" aria-hidden />
        Runs
      </h3>
      {runs.isPending ? (
        <Skeleton className="h-24 w-full" />
      ) : runs.isError ? (
        <ErrorState compact title="Couldn't load runs" info={runs.error.message} onRetry={() => void runs.refetch()} />
      ) : runs.data.items.length === 0 ? (
        <Card padding="compact">
          <EmptyState compact icon={<History />} title="No runs yet" />
        </Card>
      ) : (
        <Card padding="none" className="overflow-hidden">
          <ul className="divide-y divide-border">
            {runs.data.items.map((r) => (
              <li key={r.id} className="flex flex-col gap-1 px-4 py-3">
                <span className="flex flex-wrap items-center gap-2">
                  <Badge tone={r.status === "done" ? "success" : r.status === "failed" ? "danger" : r.status === "waiting" ? "info" : "neutral"}>{r.status}</Badge>
                  {r.record.name ? (
                    <Link href={recordHref(r.record.type, r.record.id)} className="type-label font-semibold text-fg hover:underline">
                      {r.record.name}
                    </Link>
                  ) : (
                    <span className="type-label text-fg-muted">A record you can&apos;t open</span>
                  )}
                  <time className="type-caption text-fg-subtle" dateTime={r.created_at}>
                    {formatDateTime(r.created_at)}
                  </time>
                  {r.resume_at ? <span className="type-caption text-fg-muted">next step {formatDateTime(r.resume_at)}</span> : null}
                </span>
                {r.log.length > 0 ? (
                  <ol className="flex flex-col gap-0.5 pl-2">
                    {r.log.map((l, i) => (
                      <li key={i} className="type-caption text-fg-muted">
                        <span className={l.status === "failed" ? "font-semibold text-danger-strong" : "text-fg"}>{l.step + 1}.</span> {l.message}
                      </li>
                    ))}
                  </ol>
                ) : null}
              </li>
            ))}
          </ul>
        </Card>
      )}
      {runs.data ? <LoadMore hasNextPage={!!runs.hasNextPage} isFetchingNextPage={runs.isFetchingNextPage} fetchNextPage={() => runs.fetchNextPage()} /> : null}
    </section>
  );
}

