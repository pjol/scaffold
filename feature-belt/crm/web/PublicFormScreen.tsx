"use client";

import { errorMessage, isApiError, useCrmPublicForm, useCrmSubmitForm, type CrmFormField } from "@scaffoldapp/shared";
import { Banner, Button, Card, Checkbox, EmptyState, ErrorState, Field, Input, Select, Skeleton, SkeletonGroup, TextArea, ToggleChip, cn } from "@scaffoldapp/ui-web";
import { CheckCircle2, LinkIcon } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { useState } from "react";

export type FormValues = Record<string, string | boolean | string[]>;

const keyOf = (f: Pick<CrmFormField, "object" | "key">) => `${f.object}.${f.key}`;

/** The inputs of a lead form by property type (the public page and the editor's preview). */
export function FormFieldsView({ fields, values, onChange, disabled }: { fields: CrmFormField[]; values: FormValues; onChange: (v: FormValues) => void; disabled?: boolean }) {
  const set = (k: string, v: string | boolean | string[]) => onChange({ ...values, [k]: v });
  return (
    <div className="flex flex-col gap-4">
      {fields.map((f) => {
        const k = keyOf(f);
        const v = values[k];
        const text = typeof v === "string" ? v : "";
        switch (f.type) {
          case "long_text":
            return (
              <Field key={k} label={f.label} required={f.required} disabled={disabled}>
                <TextArea value={text} maxLength={5000} onChange={(e) => set(k, e.target.value)} />
              </Field>
            );
          case "select":
            return (
              <Field key={k} label={f.label} required={f.required} disabled={disabled}>
                <Select value={text || undefined} onValueChange={(x) => set(k, x)} placeholder="Choose" options={f.options ?? []} disabled={disabled} />
              </Field>
            );
          case "multi_select": {
            const list = Array.isArray(v) ? v : [];
            return (
              <Field key={k} label={f.label} required={f.required} disabled={disabled}>
                <div className="flex flex-wrap gap-2" role="group" aria-label={f.label}>
                  {(f.options ?? []).map((o) => (
                    <ToggleChip key={o.value} pressed={list.includes(o.value)} disabled={disabled} onPressedChange={(on) => set(k, on ? [...list, o.value] : list.filter((x) => x !== o.value))}>
                      {o.label}
                    </ToggleChip>
                  ))}
                </div>
              </Field>
            );
          }
          case "checkbox":
            return <Checkbox key={k} checked={v === true} disabled={disabled} onCheckedChange={(x) => set(k, x === true)} label={f.label} required={f.required} />;
          case "date":
            return (
              <Field key={k} label={f.label} required={f.required} disabled={disabled}>
                <Input type="date" value={text} onChange={(e) => set(k, e.target.value)} />
              </Field>
            );
          default:
            return (
              <Field key={k} label={f.label} required={f.required} disabled={disabled}>
                <Input
                  value={text}
                  maxLength={500}
                  type={f.type === "email" || f.key === "email" ? "email" : f.type === "url" ? "url" : f.type === "number" ? "number" : "text"}
                  inputMode={f.type === "phone" || f.type === "phones" ? "tel" : undefined}
                  autoComplete={autoComplete(f)}
                  onChange={(e) => set(k, e.target.value)}
                />
              </Field>
            );
        }
      })}
    </div>
  );
}

function autoComplete(f: CrmFormField): string | undefined {
  if (f.object === "company") return f.key === "name" ? "organization" : undefined;
  return ({ first_name: "given-name", last_name: "family-name", email: "email", phones: "tel", job_title: "organization-title", city: "address-level2", country: "country-name" } as Record<string, string>)[f.key];
}

/** /forms/[token]: a lead form anyone with the link (or the embed) fills in. No account needed. */
export function CrmPublicFormScreen({ token }: { token: string }) {
  const embed = useSearchParams()?.get("embed") === "1";
  const form = useCrmPublicForm(token);
  const submit = useCrmSubmitForm(token);
  const [values, setValues] = useState<FormValues>({});
  const [consent, setConsent] = useState(false);
  const [honeypot, setHoneypot] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const wrap = (children: React.ReactNode) => <main className={cn("mx-auto flex w-full flex-col gap-6", embed ? "max-w-xl p-4" : "max-w-xl px-gutter py-section")}>{children}</main>;

  if (form.isPending) {
    return wrap(
      <SkeletonGroup label="Loading the form" className="flex flex-col gap-4">
        <Skeleton className="h-8 w-2/3" />
        <Skeleton className="h-64 w-full" />
      </SkeletonGroup>,
    );
  }
  if (form.isError) {
    return wrap(
      isApiError(form.error) && form.error.status === 404 ? (
        <Card padding="card">
          <EmptyState icon={<LinkIcon />} title="This form isn't available anymore" />
        </Card>
      ) : (
        <ErrorState title="Couldn't load the form" info={form.error.message} onRetry={() => void form.refetch()} retrying={form.isRefetching} />
      ),
    );
  }
  const f = form.data;
  if (done !== null) {
    return wrap(
      <Card padding="card">
        <EmptyState tone="primary" icon={<CheckCircle2 />} title={done || "Thanks — we got it"} />
      </Card>,
    );
  }
  const send = () => {
    for (const fl of f.fields) {
      const v = values[keyOf(fl)];
      const empty = v === undefined || v === "" || v === false || (Array.isArray(v) && v.length === 0);
      if (fl.required && empty) return setError(`${fl.label} is required.`);
    }
    if (f.consent_text && !consent) return setError("Tick the box to agree before sending.");
    setError(null);
    submit.mutate(
      { values, consent, website: honeypot, page_url: typeof document !== "undefined" ? (embed ? document.referrer : window.location.href).slice(0, 500) : "" },
      { onSuccess: (r) => setDone(r.message), onError: (e) => setError(errorMessage(e, "That didn't go through. Try again.")) },
    );
  };
  return wrap(
    <Card padding="card" className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        {f.seller_name ? <p className="type-label font-semibold text-fg-muted">{f.seller_name}</p> : null}
        <h1 className="type-title text-fg">{f.name}</h1>
      </header>
      <form
        className="flex flex-col gap-4"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          send();
        }}
      >
        <FormFieldsView fields={f.fields} values={values} onChange={setValues} />
        {/* People never see or fill this; bots that fill every input are answered as if it worked. */}
        <div aria-hidden className="sr-only">
          <label>
            Website
            <input type="text" name="website" tabIndex={-1} autoComplete="off" value={honeypot} onChange={(e) => setHoneypot(e.target.value)} />
          </label>
        </div>
        {f.consent_text ? <Checkbox checked={consent} onCheckedChange={(v) => setConsent(v === true)} label={f.consent_text} required /> : null}
        {error ? <Banner tone="danger">{error}</Banner> : null}
        <Button type="submit" loading={submit.isPending} className="self-start">
          {f.submit_label || "Submit"}
        </Button>
      </form>
    </Card>,
  );
}
