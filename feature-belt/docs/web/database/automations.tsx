"use client";

import {
  docsIsFilterGroup,
  docsTimeAgo,
  docsTimeZone,
  errorMessage,
  useAccessDirectory,
  useDocsAutomationRuns,
  useDocsAutomations,
  useDocsDeleteAutomation,
  useDocsSaveAutomation,
  type DocsAutomation,
  type DocsAutomationAction,
  type DocsAutomationRequest,
  type DocsAutomationTrigger,
  type DocsFilter,
  type DocsPage,
  type DocsProperty,
} from "@scaffoldapp/shared";
import { Avatar, Badge, Banner, Button, ConfirmDialog, EmptyState, ErrorState, Field, IconButton, InfoTip, Input, Select, Sheet, Skeleton, Switch, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { ArrowLeft, Check, Copy, History, Pencil, Plus, Trash2, Workflow, X } from "lucide-react";
import { useState } from "react";
import { FilterEditor } from "./panels";
import { timeZones } from "./PropertyValue";
import { AddRowTarget, settable, StepValue } from "./propertySettings";

const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const EMPTY_REFS = { users: {}, pages: {} };

function triggerText(t: DocsAutomationTrigger, schema: DocsProperty[]): string {
  switch (t.type) {
    case "row_added":
      return "When a row is added";
    case "property_edited": {
      const p = schema.find((x) => x.id === t.property);
      return `When ${p?.name ?? "a property"} changes`;
    }
    case "schedule":
      return t.every === "week" ? `Every week on ${(t.weekdays ?? []).map((d) => WEEKDAYS[d]).join(", ") || "Monday"} at ${t.time ?? "09:00"}` : `Every day at ${t.time ?? "09:00"}`;
  }
  return "";
}

function actionText(a: DocsAutomationAction, schema: DocsProperty[]): string {
  switch (a.type) {
    case "set_property":
      return `Edit ${schema.find((p) => p.id === a.property)?.name ?? "a property"}`;
    case "add_row":
      return "Add a row";
    case "notify":
      return "Notify people";
    case "webhook":
      return "Send a webhook";
  }
  return "";
}

/** The database's automations: what starts each, its conditions and steps, and its runs. Editors only. */
export function AutomationsSheet({ open, onOpenChange, db, orgId }: { open: boolean; onOpenChange: (open: boolean) => void; db: DocsPage; orgId: string }) {
  const list = useDocsAutomations(db.id);
  const save = useDocsSaveAutomation(db.id);
  const del = useDocsDeleteAutomation(db.id);
  const [editing, setEditing] = useState<DocsAutomation | "new" | null>(null);
  const [runs, setRuns] = useState<DocsAutomation | null>(null);
  const [deleting, setDeleting] = useState<DocsAutomation | null>(null);
  const schema = db.schema ?? [];
  let body;
  if (editing) {
    body = <AutomationEditor db={db} orgId={orgId} automation={editing === "new" ? null : editing} onDone={() => setEditing(null)} />;
  } else if (runs) {
    body = <Runs automation={runs} onBack={() => setRuns(null)} />;
  } else if (list.isPending) {
    body = (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-20 w-full rounded-md" />
        <Skeleton className="h-20 w-full rounded-md" />
      </div>
    );
  } else if (list.isError) {
    body = <ErrorState title="Couldn't load the automations." onRetry={() => void list.refetch()} />;
  } else if (!list.data.items.length) {
    body = <EmptyState icon={<Workflow />} title="No automations yet" action={<Button leftIcon={<Plus />} onClick={() => setEditing("new")}>New automation</Button>} />;
  } else {
    body = (
      <div className="flex flex-col gap-3">
        <ul className="flex flex-col gap-3">
          {list.data.items.map((a) => (
            <li key={a.id} className="flex flex-col gap-2 rounded-md border border-border p-3">
              <div className="flex items-start gap-2">
                <div className="min-w-0 flex-1">
                  <p className="truncate type-label font-semibold text-fg">{a.name}</p>
                  <p className="type-caption text-fg-muted">{triggerText(a.trigger, schema)}</p>
                  <p className="type-caption text-fg-muted">{a.actions.map((x) => actionText(x, schema)).join(" · ")}</p>
                </div>
                <Switch
                  aria-label={a.enabled ? `Turn off ${a.name}` : `Turn on ${a.name}`}
                  checked={a.enabled}
                  onCheckedChange={(enabled) => save.mutate({ id: a.id, enabled }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change the automation.")) })}
                />
              </div>
              {a.paused_reason ? (
                <Banner tone="warning">Paused: {a.paused_reason}</Banner>
              ) : null}
              <div className="flex flex-wrap items-center gap-1">
                <Button size="sm" variant="ghost" leftIcon={<Pencil />} onClick={() => setEditing(a)}>
                  Edit
                </Button>
                <Button size="sm" variant="ghost" leftIcon={<History />} onClick={() => setRuns(a)}>
                  Runs
                </Button>
                <Button size="sm" variant="danger-ghost" leftIcon={<Trash2 />} onClick={() => setDeleting(a)}>
                  Delete
                </Button>
                {a.last_run_at ? <span className="ml-auto type-caption text-fg-subtle">Ran {docsTimeAgo(a.last_run_at)}</span> : null}
              </div>
            </li>
          ))}
        </ul>
        <div>
          <Button leftIcon={<Plus />} onClick={() => setEditing("new")}>
            New automation
          </Button>
        </div>
      </div>
    );
  }
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title="Automations" size="lg" info="An automation runs as the person who last saved it, with that person's access; it pauses if they can no longer edit the database.">
      {body}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete “${deleting?.name ?? ""}”?`}
        confirmLabel="Delete automation"
        tone="danger"
        onConfirm={() => {
          const a = deleting;
          setDeleting(null);
          if (a) del.mutate(a.id, { onSuccess: () => toast.success("Automation deleted."), onError: (e) => toast.error(errorMessage(e, "Couldn't delete the automation.")) });
        }}
      >
        It stops running. Its run history goes with it.
      </ConfirmDialog>
    </Sheet>
  );
}

function Runs({ automation, onBack }: { automation: DocsAutomation; onBack: () => void }) {
  const runs = useDocsAutomationRuns(automation.id);
  return (
    <div className="flex flex-col gap-3">
      <div>
        <Button size="sm" variant="ghost" leftIcon={<ArrowLeft />} onClick={onBack}>
          {automation.name}
        </Button>
      </div>
      {runs.isPending ? <Skeleton className="h-24 w-full rounded-md" /> : null}
      {runs.isError ? <ErrorState title="Couldn't load the runs." onRetry={() => void runs.refetch()} /> : null}
      {runs.data && runs.data.items.length === 0 ? <p className="type-label text-fg-muted">It hasn't run yet.</p> : null}
      {runs.data?.items.length ? (
        <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
          {runs.data.items.map((r) => (
            <li key={r.id} className="flex flex-col gap-0.5 px-3 py-2">
              <div className="flex items-center gap-2">
                <Badge tone={r.status === "ok" ? "success" : r.status === "failed" ? "danger" : "neutral"} size="sm">
                  {r.status === "ok" ? "Done" : r.status === "failed" ? "Failed" : "Skipped"}
                </Badge>
                <time className="type-caption text-fg-muted" dateTime={r.created_at} title={new Date(r.created_at).toLocaleString()}>
                  {docsTimeAgo(r.created_at)}
                </time>
                {r.row_id ? (
                  <a href={`/docs/${r.row_id}`} className="ml-auto type-caption text-primary-strong underline underline-offset-2">
                    Open the row
                  </a>
                ) : null}
              </div>
              {r.detail ? <p className="type-caption text-fg">{r.detail}</p> : null}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

/** People to notify: chips and a directory search. */
function PeoplePick({ orgId, value, onChange }: { orgId: string; value: string[]; onChange: (ids: string[]) => void }) {
  const [q, setQ] = useState("");
  const [names, setNames] = useState<Record<string, string>>({});
  const dir = useAccessDirectory(orgId, q, { types: "user", limit: 8, enabled: q.trim().length > 0 });
  return (
    <div className="flex flex-col gap-2">
      {value.length ? (
        <div className="flex flex-wrap gap-1">
          {value.map((id) => (
            <span key={id} className="inline-flex min-h-6 items-center gap-1 rounded-sm bg-sunken pl-1.5 type-caption text-fg">
              {names[id] ?? "Someone"}
              <button type="button" aria-label={`Remove ${names[id] ?? "person"}`} className="flex size-6 items-center justify-center rounded-sm hover:bg-border focus-visible:outline-2 focus-visible:outline-ring" onClick={() => onChange(value.filter((x) => x !== id))}>
                <X aria-hidden className="size-3" />
              </button>
            </span>
          ))}
        </div>
      ) : null}
      <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search people" aria-label="Search people to notify" />
      {q.trim() ? (
        <ul className="flex max-h-40 flex-col overflow-y-auto rounded-md border border-border p-1">
          {(dir.data ?? []).map((p) => (
            <li key={p.id}>
              <button
                type="button"
                onClick={() => {
                  setNames((n) => ({ ...n, [p.id]: p.name }));
                  if (!value.includes(p.id)) onChange([...value, p.id]);
                  setQ("");
                }}
                className="flex min-h-control w-full items-center gap-2 rounded-sm px-2 text-left type-label hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
              >
                <Avatar size="xs" name={p.name} {...(p.avatar_url ? { src: p.avatar_url } : {})} />
                <span className="min-w-0 flex-1 truncate">{p.name}</span>
                {value.includes(p.id) ? <Check aria-hidden className="size-4 text-primary-strong" /> : null}
              </button>
            </li>
          ))}
          {dir.data && dir.data.length === 0 ? <li className="px-2 py-1 type-caption text-fg-muted">No one matches.</li> : null}
        </ul>
      ) : null}
    </div>
  );
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [done, setDone] = useState(false);
  return (
    <IconButton
      label={done ? "Copied" : label}
      icon={done ? <Check /> : <Copy />}
      size="sm"
      variant="ghost"
      onClick={() => {
        void navigator.clipboard.writeText(text).then(
          () => {
            setDone(true);
            window.setTimeout(() => setDone(false), 1500);
          },
          () => toast.error("Couldn't copy. Select the text and copy it."),
        );
      }}
    />
  );
}

function conditionsIn(c: DocsFilter[]): { filters: DocsFilter[]; conj: "and" | "or" } {
  const only = c[0];
  if (c.length === 1 && only && docsIsFilterGroup(only) && only.conj === "or") return { filters: only.filters, conj: "or" };
  return { filters: c, conj: "and" };
}

function AutomationEditor({ db, orgId, automation, onDone }: { db: DocsPage; orgId: string; automation: DocsAutomation | null; onDone: () => void }) {
  const schema = db.schema ?? [];
  const save = useDocsSaveAutomation(db.id);
  const [draft, setDraft] = useState<Required<Pick<DocsAutomationRequest, "name" | "enabled" | "trigger" | "conditions" | "actions">>>({
    name: automation?.name ?? "",
    enabled: automation?.enabled ?? true,
    trigger: automation?.trigger ?? { type: "row_added" },
    conditions: automation?.conditions ?? [],
    actions: automation?.actions ?? [{ type: "set_property" }],
  });
  const [error, setError] = useState<string | null>(null);
  const t = draft.trigger;
  const setT = (patch: Partial<DocsAutomationTrigger>) => setDraft((d) => ({ ...d, trigger: { ...d.trigger, ...patch } }));
  const setA = (i: number, a: DocsAutomationAction) => setDraft((d) => ({ ...d, actions: d.actions.map((x, j) => (j === i ? a : x)) }));
  const watched = schema.filter((p) => p.type === "title" || settable(schema).includes(p) || p.type === "files" || p.type === "relation");
  const trigProp = schema.find((p) => p.id === t.property);
  const people = schema.filter((p) => p.type === "person" || p.type === "created_by" || p.type === "last_edited_by");
  const cond = conditionsIn(draft.conditions);
  const hasWebhook = draft.actions.some((a) => a.type === "webhook");
  const submit = () => {
    setError(null);
    save.mutate(
      { ...(automation ? { id: automation.id } : {}), ...draft, name: draft.name.trim() || "Automation" },
      {
        onSuccess: () => {
          toast.success(automation ? "Automation saved." : "Automation added.");
          onDone();
        },
        onError: (e) => setError(errorMessage(e, "Couldn't save the automation.")),
      },
    );
  };
  return (
    <div className="flex flex-col gap-5">
      <div>
        <Button size="sm" variant="ghost" leftIcon={<ArrowLeft />} onClick={onDone}>
          Automations
        </Button>
      </div>
      <Field label="Name">
        <Input value={draft.name} maxLength={100} placeholder="Automation" onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))} />
      </Field>
      <section aria-label="When" className="flex flex-col gap-3">
        <h3 className="type-title-sm text-fg">When</h3>
        <Select
          aria-label="Trigger"
          value={t.type}
          options={[
            { value: "row_added", label: "A row is added" },
            { value: "property_edited", label: "A property changes" },
            { value: "schedule", label: "On a schedule" },
          ]}
          onValueChange={(v) => setDraft((d) => ({ ...d, trigger: v === "schedule" ? { type: "schedule", every: "day", time: "09:00", tz: docsTimeZone() } : { type: v as DocsAutomationTrigger["type"] } }))}
        />
        {t.type === "property_edited" ? (
          <>
            <Field label="Property">
              <Select value={t.property ?? ""} placeholder="Choose a property" options={watched.map((p) => ({ value: p.id, label: p.name }))} onValueChange={(v) => setT({ property: v, values: [] })} />
            </Field>
            {trigProp && (trigProp.type === "select" || trigProp.type === "status" || trigProp.type === "multi_select") ? (
              <fieldset className="flex flex-col gap-1">
                <legend className="pb-1 type-label text-fg-muted">To any of (none: any change)</legend>
                <div className="flex flex-wrap gap-1">
                  {(trigProp.options ?? []).map((o) => {
                    const on = (t.values ?? []).includes(o.id);
                    return (
                      <button
                        key={o.id}
                        type="button"
                        aria-pressed={on}
                        onClick={() => setT({ values: on ? (t.values ?? []).filter((x) => x !== o.id) : [...(t.values ?? []), o.id] })}
                        className={cn("min-h-8 rounded-sm border px-2 type-caption focus-visible:outline-2 focus-visible:outline-ring", on ? "border-primary bg-primary-soft text-fg" : "border-border text-fg-muted hover:bg-sunken")}
                      >
                        {o.name}
                      </button>
                    );
                  })}
                </div>
              </fieldset>
            ) : null}
            {trigProp?.type === "checkbox" ? (
              <Select
                aria-label="Changes to"
                value={t.values?.length ? String(t.values[0]) : "any"}
                options={[
                  { value: "any", label: "Any change" },
                  { value: "true", label: "Checked" },
                  { value: "false", label: "Unchecked" },
                ]}
                onValueChange={(v) => setT({ values: v === "any" ? [] : [v === "true"] })}
              />
            ) : null}
          </>
        ) : null}
        {t.type === "schedule" ? (
          <div className="flex flex-col gap-3">
            <Select
              aria-label="Every"
              value={t.every ?? "day"}
              options={[
                { value: "day", label: "Every day" },
                { value: "week", label: "Every week" },
              ]}
              onValueChange={(v) => setT({ every: v as "day" | "week", ...(v === "week" && !t.weekdays?.length ? { weekdays: [0] } : {}) })}
            />
            {t.every === "week" ? (
              <div role="group" aria-label="On" className="flex flex-wrap gap-1">
                {WEEKDAYS.map((d, i) => {
                  const on = (t.weekdays ?? []).includes(i);
                  return (
                    <button
                      key={d}
                      type="button"
                      aria-pressed={on}
                      onClick={() => setT({ weekdays: on ? (t.weekdays ?? []).filter((x) => x !== i) : [...(t.weekdays ?? []), i].sort() })}
                      className={cn("h-control min-w-11 rounded-md border px-2 type-label focus-visible:outline-2 focus-visible:outline-ring", on ? "border-primary bg-primary-soft text-fg" : "border-border text-fg-muted hover:bg-sunken")}
                    >
                      {d}
                    </button>
                  );
                })}
              </div>
            ) : null}
            <div className="flex flex-col gap-3 sm:flex-row">
              <Field label="Time" className="sm:w-36">
                <Input type="time" value={t.time ?? "09:00"} onChange={(e) => setT({ time: e.target.value })} />
              </Field>
              <Field label="Time zone" className="flex-1">
                <Select value={t.tz ?? docsTimeZone()} options={timeZones().map((z) => ({ value: z, label: z.replace(/_/g, " ") }))} onValueChange={(v) => setT({ tz: v })} />
              </Field>
            </div>
          </div>
        ) : null}
      </section>
      <section aria-label="Only if" className="flex flex-col gap-3">
        <h3 className="flex items-center gap-2 type-title-sm text-fg">
          Only if
          <InfoTip content={t.type === "schedule" ? "On a schedule, “Edit a property” changes every row that matches; the other steps run once." : "The row must match these when it's added or changed."} />
        </h3>
        <FilterEditor schema={schema} filters={cond.filters} conj={cond.conj} refs={EMPTY_REFS} onChange={(filters, conj) => setDraft((d) => ({ ...d, conditions: conj === "or" && filters.length > 1 ? [{ conj: "or", filters }] : filters }))} />
      </section>
      <section aria-label="Do" className="flex flex-col gap-3">
        <h3 className="type-title-sm text-fg">Do</h3>
        <ol className="flex flex-col gap-3">
          {draft.actions.map((a, i) => (
            <li key={i} className="flex flex-col gap-2 rounded-md border border-border p-3">
              <div className="flex items-center gap-2">
                <span className="type-caption tabular-nums text-fg-muted">{i + 1}</span>
                <Select
                  aria-label="Step"
                  className="flex-1"
                  value={a.type}
                  options={[
                    { value: "set_property", label: "Edit a property" },
                    { value: "add_row", label: "Add a row" },
                    { value: "notify", label: "Notify people" },
                    { value: "webhook", label: "Send a webhook" },
                  ]}
                  onValueChange={(v) => setA(i, { type: v as DocsAutomationAction["type"] })}
                />
                <IconButton label="Remove step" icon={<Trash2 />} size="sm" variant="ghost" disabled={draft.actions.length < 2} onClick={() => setDraft((d) => ({ ...d, actions: d.actions.filter((_, j) => j !== i) }))} />
              </div>
              {a.type === "set_property" ? (
                <div className="flex flex-col gap-2 sm:flex-row">
                  <Select aria-label="Property" className="sm:w-44" value={a.property ?? ""} placeholder="Property" options={settable(schema).map((p) => ({ value: p.id, label: p.name }))} onValueChange={(v) => setA(i, { type: "set_property", property: v })} />
                  <div className="min-w-0 flex-1">
                    <StepValue key={a.property} prop={schema.find((p) => p.id === a.property)} value={a.value} onChange={(v) => setA(i, { ...a, value: v })} />
                  </div>
                </div>
              ) : null}
              {a.type === "add_row" ? <AddRowTarget orgId={orgId} dbId={db.id} value={a.database_id ?? ""} onChange={(id) => setA(i, { type: "add_row", database_id: id })} /> : null}
              {a.type === "notify" ? (
                <div className="flex flex-col gap-3">
                  <Field label="People">
                    <PeoplePick orgId={orgId} value={a.people ?? []} onChange={(ids) => setA(i, { ...a, people: ids })} />
                  </Field>
                  {people.length ? (
                    <Field label="And the people in">
                      <Select
                        value={a.person_property ?? "none"}
                        options={[{ value: "none", label: "No property" }, ...people.map((p) => ({ value: p.id, label: p.name }))]}
                        onValueChange={(v) => {
                          const next = { ...a };
                          if (v === "none") delete next.person_property;
                          else next.person_property = v;
                          setA(i, next);
                        }}
                      />
                    </Field>
                  ) : null}
                  <Field label="Message">
                    <TextArea value={a.message ?? ""} maxLength={500} minRows={2} onChange={(e) => setA(i, { ...a, message: e.target.value })} />
                  </Field>
                </div>
              ) : null}
              {a.type === "webhook" ? (
                <Field label="Address" info="A public https address. Each run POSTs the rows it ran for as JSON.">
                  <Input type="url" placeholder="https://" value={a.url ?? ""} onChange={(e) => setA(i, { type: "webhook", url: e.target.value })} />
                </Field>
              ) : null}
            </li>
          ))}
        </ol>
        {draft.actions.length < 10 ? (
          <div>
            <Button size="sm" variant="ghost" leftIcon={<Plus />} onClick={() => setDraft((d) => ({ ...d, actions: [...d.actions, { type: "set_property" }] }))}>
              Add a step
            </Button>
          </div>
        ) : null}
      </section>
      {hasWebhook ? (
        <section aria-label="Signing secret" className="flex flex-col gap-2">
          <h3 className="flex items-center gap-2 type-title-sm text-fg">
            Signing secret
            <InfoTip content={'Each webhook carries X-Timestamp (unix seconds) and X-Signature: sha256=HMAC-SHA256(secret, "<X-Timestamp>.<body>") in hex. Compute it with the secret, compare in constant time, and reject old timestamps.'} />
          </h3>
          {automation?.secret ? (
            <div className="flex items-center gap-2 rounded-md border border-border bg-sunken px-3 py-1">
              <code className="min-w-0 flex-1 truncate font-mono type-caption text-fg">{automation.secret}</code>
              <CopyButton text={automation.secret} label="Copy the secret" />
            </div>
          ) : (
            <p className="type-label text-fg-muted">Shown here once the automation is saved.</p>
          )}
        </section>
      ) : null}
      <Switch checked={draft.enabled} onCheckedChange={(enabled) => setDraft((d) => ({ ...d, enabled }))} label="On" />
      {error ? (
        <p role="alert" className="type-label text-danger-strong">
          {error}
        </p>
      ) : null}
      <div className="flex justify-end gap-2">
        <Button variant="ghost" onClick={onDone}>
          Cancel
        </Button>
        <Button onClick={submit} loading={save.isPending}>
          {automation ? "Save" : "Add automation"}
        </Button>
      </div>
    </div>
  );
}
