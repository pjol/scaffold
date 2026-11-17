"use client";

import {
  docsCurrencyCode,
  docsNumberScale,
  errorMessage,
  isApiError,
  useDocsForm,
  useDocsPublicForm,
  useDocsPublicSubmitForm,
  useDocsSubmitForm,
  type DocsFormConfig,
  type DocsFormDetail,
  type DocsFormQuestion,
  type DocsFormQuestionDetail,
  type DocsProperty,
  type DocsValue,
} from "@scaffoldapp/shared";
import { Button, Checkbox, EmptyState, ErrorState, Field, IconButton, Input, Select, Skeleton, Switch, TextArea, cn } from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowUp, CircleCheck, ExternalLink, Link2Off, Lock, Trash2 } from "lucide-react";
import Link from "next/link";
import { useState, type FormEvent } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { PROPERTY_ICON } from "../format";
import { parseNumberInput } from "./PropertyValue";
import type { ViewProps } from "./types";

/** Properties a form can ask for (mirrors the server). */
export function formAnswerable(p: DocsProperty): boolean {
  return ["title", "text", "number", "select", "multi_select", "status", "date", "checkbox", "url", "email", "phone"].includes(p.type);
}

/** Text that commits when it loses focus. */
function LazyInput({ value, onCommit, label, multiline, placeholder, max }: { value: string; onCommit: (v: string) => void; label: string; multiline?: boolean; placeholder?: string; max: number }) {
  const [v, setV] = useState(value);
  const [prev, setPrev] = useState(value);
  if (value !== prev) {
    setPrev(value);
    setV(value);
  }
  const commit = () => v !== value && onCommit(v);
  return multiline ? (
    <TextArea aria-label={label} value={v} maxLength={max} minRows={2} maxRows={6} placeholder={placeholder} onChange={(e) => setV(e.target.value)} onBlur={commit} />
  ) : (
    <Input aria-label={label} value={v} maxLength={max} placeholder={placeholder} onChange={(e) => setV(e.target.value)} onBlur={commit} onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()} />
  );
}

/** A form view in the database: its questions and settings (editors), or a link to fill it. */
export function FormBuilder(props: ViewProps) {
  const form: DocsFormConfig = props.config.form ?? { questions: [] };
  const detail = useDocsForm(props.view.id);
  const editable = !!props.configEditable;
  const set = (patch: Partial<DocsFormConfig>) => props.onConfigChange({ ...props.config, form: { ...form, ...patch } });
  const setQ = (i: number, q: DocsFormQuestion) => set({ questions: form.questions.map((x, j) => (j === i ? q : x)) });
  const move = (i: number, d: -1 | 1) => {
    const j = i + d;
    if (j < 0 || j >= form.questions.length) return;
    const next = [...form.questions];
    [next[i], next[j]] = [next[j]!, next[i]!];
    set({ questions: next });
  };
  const asked = new Set(form.questions.map((q) => q.property));
  const available = props.schema.filter((p) => formAnswerable(p) && !asked.has(p.id));
  const href = `/docs/forms/${props.view.id}`;
  if (!editable) {
    return (
      <div className="flex flex-col items-start gap-3 rounded-md border border-border p-4">
        <p className="type-title-sm text-fg">{form.title || props.view.name}</p>
        <Button asChild leftIcon={<ExternalLink />}>
          <Link href={href}>Open the form</Link>
        </Button>
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-4 lg:flex-row lg:items-start">
      <div className="flex min-w-0 flex-1 flex-col gap-3 rounded-md border border-border p-4">
        <LazyInput label="Form title" value={form.title ?? ""} placeholder={props.view.name} max={200} onCommit={(title) => set({ title })} />
        <LazyInput label="Form description" value={form.description ?? ""} placeholder="Description (optional)" max={2000} multiline onCommit={(description) => set({ description })} />
        <ol className="flex flex-col gap-3">
          {form.questions.map((q, i) => {
            const p = props.schema.find((x) => x.id === q.property);
            if (!p) return null;
            const Icon = PROPERTY_ICON[p.type];
            return (
              <li key={q.property} className="flex flex-col gap-2 rounded-md border border-border bg-sunken/40 p-3">
                <div className="flex items-center gap-2">
                  <Icon aria-hidden className="size-4 shrink-0 text-fg-muted" />
                  <span className="min-w-0 flex-1 truncate type-caption text-fg-muted">{p.name}</span>
                  <IconButton label="Move up" icon={<ArrowUp />} size="sm" variant="ghost" disabled={i === 0} onClick={() => move(i, -1)} />
                  <IconButton label="Move down" icon={<ArrowDown />} size="sm" variant="ghost" disabled={i === form.questions.length - 1} onClick={() => move(i, 1)} />
                  <IconButton label={`Remove ${q.label || p.name}`} icon={<Trash2 />} size="sm" variant="ghost" onClick={() => set({ questions: form.questions.filter((_, j) => j !== i) })} />
                </div>
                <LazyInput label="Question" value={q.label ?? ""} placeholder={p.name} max={200} onCommit={(label) => setQ(i, { ...q, label })} />
                <LazyInput label="Question description" value={q.description ?? ""} placeholder="Description (optional)" max={500} onCommit={(description) => setQ(i, { ...q, description })} />
                <Switch checked={!!q.required} onCheckedChange={(required) => setQ(i, { ...q, required })} label="Required" />
              </li>
            );
          })}
        </ol>
        {available.length ? (
          <Select
            aria-label="Add a question"
            className="w-56"
            placeholder="Add a question"
            value=""
            options={available.map((p) => ({ value: p.id, label: p.name }))}
            onValueChange={(id) => set({ questions: [...form.questions, { property: id }] })}
          />
        ) : (
          <p className="type-caption text-fg-muted">Every property a form can ask for is in it.</p>
        )}
      </div>
      <aside className="flex w-full flex-col gap-3 rounded-md border border-border p-4 lg:w-80">
        <div className="flex items-center gap-2">
          <Button asChild variant="secondary" leftIcon={<ExternalLink />} className="flex-1">
            <Link href={href}>Open form</Link>
          </Button>
          {props.publicView ? null : <ResourceMenu feature="docs" kind="docs.form" id={props.view.id} title={form.title || props.view.name} label="Share the form" />}
        </div>
        {detail.data?.responses !== undefined ? (
          <p className="type-label text-fg">
            {detail.data.responses} {detail.data.responses === 1 ? "response" : "responses"}
          </p>
        ) : null}
        <Switch checked={!form.closed} onCheckedChange={(on) => set({ closed: !on })} label="Accepting responses" />
        <Switch checked={!!form.anonymous} onCheckedChange={(anonymous) => set({ anonymous })} label="Anonymous" info="Responses from people signed in aren't linked to them." />
        <Field label="Submit button">
          <LazyInput label="Submit button" value={form.submit_label ?? ""} placeholder="Submit" max={40} onCommit={(submit_label) => set({ submit_label })} />
        </Field>
        <Field label="After submitting">
          <LazyInput label="After submitting" value={form.success_message ?? ""} placeholder="Thanks for your response." max={500} multiline onCommit={(success_message) => set({ success_message })} />
        </Field>
      </aside>
    </div>
  );
}

// ---- filling a form ----------------------------------------------------------------

type Answers = Record<string, string | string[] | boolean>;

function answerValue(p: DocsProperty, a: Answers[string] | undefined): DocsValue | undefined {
  if (a === undefined) return undefined;
  switch (p.type) {
    case "checkbox":
      return a === true;
    case "multi_select":
      return Array.isArray(a) && a.length ? a : undefined;
    case "number": {
      const n = typeof a === "string" ? parseNumberInput(a) : null;
      if (n === null) return undefined;
      return docsCurrencyCode(p.number_format) ? Math.round(n * docsNumberScale(p)) : n;
    }
    case "date":
      return typeof a === "string" && a ? { start: a } : undefined;
  }
  return typeof a === "string" && a.trim() ? a.trim() : undefined;
}

function Question({ q, value, onChange, error }: { q: DocsFormQuestionDetail; value: Answers[string] | undefined; onChange: (v: Answers[string]) => void; error?: string | undefined }) {
  const p = q.property;
  const label = q.label || p.name;
  const common = { label, ...(q.description ? { info: q.description } : {}), required: q.required, ...(error ? { error } : {}) };
  switch (p.type) {
    case "checkbox":
      return <Checkbox checked={value === true} onCheckedChange={(c) => onChange(c)} label={label} {...(q.description ? { info: q.description } : {})} required={q.required} invalid={!!error} />;
    case "select":
    case "status":
      return (
        <Field {...common}>
          <Select value={typeof value === "string" ? value : ""} placeholder="Choose" options={(p.options ?? []).map((o) => ({ value: o.id, label: o.name }))} onValueChange={onChange} />
        </Field>
      );
    case "multi_select": {
      const chosen = Array.isArray(value) ? value : [];
      return (
        <fieldset className="flex flex-col gap-2">
          <legend className="pb-1 type-label font-medium text-fg">
            {label}
            {q.required ? <span className="text-danger-strong"> *</span> : null}
          </legend>
          {q.description ? <p className="sr-only">{q.description}</p> : null}
          {(p.options ?? []).map((o) => (
            <Checkbox key={o.id} checked={chosen.includes(o.id)} onCheckedChange={(c) => onChange(c ? [...chosen, o.id] : chosen.filter((x) => x !== o.id))} label={o.name} />
          ))}
          {error ? <p className="type-caption text-danger-strong">{error}</p> : null}
        </fieldset>
      );
    }
    case "text":
      return (
        <Field {...common}>
          <TextArea value={typeof value === "string" ? value : ""} onChange={(e) => onChange(e.target.value)} maxLength={10000} />
        </Field>
      );
    case "date":
      return (
        <Field {...common}>
          <Input type="date" value={typeof value === "string" ? value : ""} onChange={(e) => onChange(e.target.value)} />
        </Field>
      );
  }
  return (
    <Field {...common}>
      <Input
        value={typeof value === "string" ? value : ""}
        onChange={(e) => onChange(e.target.value)}
        type={p.type === "email" ? "email" : p.type === "url" ? "url" : p.type === "phone" ? "tel" : "text"}
        {...(p.type === "number" && docsCurrencyCode(p.number_format) ? { placeholder: docsCurrencyCode(p.number_format)!.toUpperCase() } : {})}
        inputMode={p.type === "number" ? "decimal" : p.type === "email" ? "email" : p.type === "phone" ? "tel" : p.type === "url" ? "url" : "text"}
        maxLength={p.type === "title" ? 300 : 2000}
      />
    </Field>
  );
}

/** A form's questions and its submit button; then its message. */
export function FormFill({ detail, submit, submitting }: { detail: DocsFormDetail; submit: (values: Record<string, DocsValue>) => Promise<{ message: string }>; submitting: boolean }) {
  const [answers, setAnswers] = useState<Answers>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [failed, setFailed] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  if (done !== null) {
    return (
      <div className="flex flex-col items-center gap-4 rounded-lg border border-border bg-surface p-8 text-center">
        <CircleCheck aria-hidden className="size-10 text-success" />
        <p className="type-title-sm text-fg">{done || detail.success_message || "Thanks for your response."}</p>
        <Button
          variant="secondary"
          onClick={() => {
            setAnswers({});
            setDone(null);
          }}
        >
          Submit another response
        </Button>
      </div>
    );
  }
  if (detail.closed) {
    return <EmptyState icon={<Lock />} title="This form isn't taking responses" />;
  }
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const values: Record<string, DocsValue> = {};
    const errs: Record<string, string> = {};
    for (const q of detail.questions) {
      const raw = answers[q.property.id];
      const v = answerValue(q.property, raw);
      if (q.property.type === "number" && typeof raw === "string" && raw.trim() && v === undefined) errs[q.property.id] = "Enter a number.";
      else if (q.required && (v === undefined || v === false)) errs[q.property.id] = "Answer this question.";
      else if (v !== undefined) values[q.property.id] = v;
    }
    setErrors(errs);
    setFailed(null);
    if (Object.keys(errs).length) return;
    submit(values).then(
      (r) => setDone(r.message ?? ""),
      (err) => setFailed(isApiError(err) && err.code === "form_closed" ? "This form isn't taking responses." : errorMessage(err, "Couldn't send your response. Try again.")),
    );
  };
  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-6 rounded-lg border border-border bg-surface p-5 sm:p-8">
      <div className="flex flex-col gap-2">
        <h1 className="type-title text-fg">{detail.title}</h1>
        {detail.description ? <p className="whitespace-pre-line type-body text-fg-muted">{detail.description}</p> : null}
      </div>
      {detail.questions.map((q) => (
        <Question key={q.property.id} q={q} value={answers[q.property.id]} error={errors[q.property.id]} onChange={(v) => setAnswers((a) => ({ ...a, [q.property.id]: v }))} />
      ))}
      {failed ? (
        <p role="alert" className="type-label text-danger-strong">
          {failed}
        </p>
      ) : null}
      <div>
        <Button type="submit" loading={submitting}>
          {detail.submit_label || "Submit"}
        </Button>
      </div>
      {detail.anonymous ? <p className="type-caption text-fg-muted">Responses are anonymous.</p> : null}
    </form>
  );
}

function FormSkeleton() {
  return (
    <div className="flex flex-col gap-4 rounded-lg border border-border p-8" aria-busy aria-label="Loading form">
      <Skeleton className="h-8 w-2/3" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-10 w-full" />
      <Skeleton className="h-10 w-full" />
    </div>
  );
}

/** /docs/forms/<view id>: a form shared with the signed-in person. */
export function FormScreen({ viewId }: { viewId: string }) {
  const q = useDocsForm(viewId);
  const submit = useDocsSubmitForm(viewId);
  return (
    <main className="mx-auto flex w-full max-w-2xl flex-col gap-4 px-gutter py-section">
      {q.isPending ? (
        <FormSkeleton />
      ) : q.isError ? (
        q.error.status === 403 || q.error.status === 404 ? (
          <EmptyState icon={<Lock />} title="You can't open this form" />
        ) : (
          <ErrorState title="Couldn't load the form." onRetry={() => void q.refetch()} />
        )
      ) : (
        <>
          <FormFill detail={q.data} submit={(v) => submit.mutateAsync(v)} submitting={submit.isPending} />
          {q.data.my_level === "edit" || q.data.my_level === "manage" ? (
            <Link href={`/docs/${q.data.database_id}`} className={cn("w-fit rounded-sm type-label text-fg-muted underline underline-offset-4 hover:text-fg focus-visible:outline-2 focus-visible:outline-ring")}>
              Open the responses
            </Link>
          ) : null}
        </>
      )}
    </main>
  );
}

/** /p/form/<token>: a public form, no account. */
export function PublicFormScreen({ token }: { token: string }) {
  const q = useDocsPublicForm(token);
  const submit = useDocsPublicSubmitForm(token);
  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-4 px-gutter py-section">
      {q.isPending ? (
        <FormSkeleton />
      ) : q.isError ? (
        q.error.status === 404 ? (
          <EmptyState icon={<Link2Off />} title="This link doesn't work anymore" />
        ) : (
          <ErrorState title="Couldn't load the form." onRetry={() => void q.refetch()} />
        )
      ) : (
        <FormFill detail={q.data} submit={(v) => submit.mutateAsync(v)} submitting={submit.isPending} />
      )}
    </div>
  );
}
