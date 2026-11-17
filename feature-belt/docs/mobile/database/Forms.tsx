import {
  docsCurrencyCode,
  docsFormatDateValue,
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
import { Banner, Button, Checkbox, Chip, EmptyState, ErrorState, Field, IconButton, InfoTip, Input, ListRow, Screen, Sheet, Skeleton, Switch, Text, TextArea, useTheme } from "@scaffoldapp/ui-native";
import { Stack, useLocalSearchParams } from "expo-router";
import { ArrowDown, ArrowUp, CircleCheck, ClipboardList, Lock, Plus, X } from "lucide-react-native";
import { useEffect, useState } from "react";
import { ScrollView, View } from "react-native";
import type { ViewCtx } from "./context";
import { DateEditor, dateDraft, dateFromDraft, type DateDraft } from "./editors";

const EMAIL_RE = /^[^@\s]+@[^@\s]+\.[^@\s]+$/;
const PHONE_RE = /^\+?[0-9 ().\-/]{3,30}(\s*(x|ext\.?)\s*[0-9]{1,6})?$/;
const URL_RE = /^https?:\/\/[^\s.]+\.[^\s]+$/i;

/** Properties a form can ask for (mirrors Go formAnswerable). */
export function formAnswerable(p: DocsProperty): boolean {
  return ["title", "text", "number", "select", "multi_select", "status", "date", "checkbox", "url", "email", "phone"].includes(p.type);
}

type Answer = string | string[] | boolean | DateDraft | null;

function emptyAnswer(p: DocsProperty): Answer {
  if (p.type === "multi_select") return [];
  if (p.type === "checkbox") return false;
  if (p.type === "date") return dateDraft(null);
  return p.type === "select" || p.type === "status" ? null : "";
}

/** An answer as the value the server stores, or an error. */
function toValue(q: DocsFormQuestionDetail, a: Answer): { value: DocsValue; error?: string } {
  const p = q.property;
  if (p.type === "date") {
    const r = dateFromDraft(a as DateDraft);
    if (r.error) return { value: null, error: r.error };
    return { value: r.value };
  }
  if (p.type === "checkbox") return { value: a === true };
  if (p.type === "multi_select") return { value: (a as string[]) ?? [] };
  if (p.type === "select" || p.type === "status") return { value: (a as string | null) ?? null };
  const t = typeof a === "string" ? a.trim() : "";
  if (!t) return { value: null };
  switch (p.type) {
    case "number": {
      const n = Number(t.replace(/[,\s$€£¥%]/g, ""));
      if (!Number.isFinite(n)) return { value: null, error: "Enter a number." };
      return { value: docsCurrencyCode(p.number_format) ? Math.round(n * docsNumberScale(p)) : n };
    }
    case "email":
      return EMAIL_RE.test(t) ? { value: t } : { value: null, error: "Enter an email address." };
    case "phone":
      return PHONE_RE.test(t) ? { value: t } : { value: null, error: "Enter a phone number." };
    case "url": {
      const u = t.includes("://") ? t : `https://${t}`;
      return URL_RE.test(u) ? { value: u } : { value: null, error: "Enter a web address." };
    }
  }
  return { value: t };
}

function isBlank(v: DocsValue): boolean {
  return v === null || v === "" || (Array.isArray(v) && v.length === 0) || v === false;
}

function Question({ q, answer, onChange, error }: { q: DocsFormQuestionDetail; answer: Answer; onChange: (a: Answer) => void; error?: string | undefined }) {
  const theme = useTheme();
  const s = theme.space;
  const p = q.property;
  const [picking, setPicking] = useState(false);
  const info = q.description || undefined;
  if (p.type === "checkbox") {
    return (
      <View style={{ gap: s[1] }}>
        <Checkbox label={q.label} {...(info ? { info } : {})} checked={answer === true} onCheckedChange={(v) => onChange(v)} invalid={!!error} />
        {error ? (
          <Text role="caption" tone="danger">
            {error}
          </Text>
        ) : null}
      </View>
    );
  }
  let input;
  if (p.type === "select" || p.type === "status" || p.type === "multi_select") {
    const multi = p.type === "multi_select";
    const chosen = multi ? ((answer as string[]) ?? []) : answer ? [answer as string] : [];
    input = (
      <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }} accessibilityRole={multi ? undefined : "radiogroup"}>
        {(p.options ?? []).map((o) => {
          const on = chosen.includes(o.id);
          return (
            <Chip key={o.id} size="sm" selected={on} onPress={() => onChange(multi ? (on ? chosen.filter((x) => x !== o.id) : [...chosen, o.id]) : on ? null : o.id)}>
              {o.name}
            </Chip>
          );
        })}
      </View>
    );
  } else if (p.type === "date") {
    const d = answer as DateDraft;
    const r = dateFromDraft(d);
    input = picking ? (
      <View style={{ gap: s[2] }}>
        <DateEditor prop={p} draft={d} onChange={onChange} allowReminder={false} />
        <Button variant="secondary" onPress={() => setPicking(false)}>
          Done
        </Button>
      </View>
    ) : (
      <Button variant="outline" onPress={() => setPicking(true)}>
        {r.value ? docsFormatDateValue(r.value, p) : "Choose a date"}
      </Button>
    );
  } else if (p.type === "text") {
    input = <TextArea value={(answer as string) ?? ""} onChangeText={onChange} accessibilityLabel={q.label} invalid={!!error} />;
  } else {
    const currency = p.type === "number" ? docsCurrencyCode(p.number_format) : null;
    input = (
      <Input
        value={(answer as string) ?? ""}
        onChangeText={onChange}
        accessibilityLabel={q.label}
        invalid={!!error}
        keyboardType={p.type === "number" ? "decimal-pad" : p.type === "email" ? "email-address" : p.type === "url" ? "url" : p.type === "phone" ? "phone-pad" : "default"}
        autoCapitalize={p.type === "title" ? "sentences" : "none"}
        autoCorrect={p.type === "title"}
        {...(currency ? { trailing: <Text role="label" tone="muted">{currency.toUpperCase()}</Text> } : {})}
      />
    );
  }
  return (
    <Field label={q.label} required={q.required} error={error} {...(info ? { info } : {})}>
      {input}
    </Field>
  );
}

/** Fills a form: each question by its property's type, required answers checked, then the success message. */
export function FormFill({ form, submit, submitting, signedIn }: { form: DocsFormDetail; submit: (values: Record<string, DocsValue>) => Promise<{ message?: string }>; submitting: boolean; signedIn: boolean }) {
  const theme = useTheme();
  const s = theme.space;
  const fresh = () => Object.fromEntries(form.questions.map((q) => [q.property.id, emptyAnswer(q.property)])) as Record<string, Answer>;
  const [answers, setAnswers] = useState<Record<string, Answer>>(fresh);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [failure, setFailure] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  useEffect(() => {
    setAnswers(fresh());
    setErrors({});
    // A different form (or its questions changed): start over.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.view_id, form.questions.length]);
  if (form.closed) return <EmptyState icon={Lock} title="This form isn't taking responses" />;
  if (done !== null) {
    return (
      <View style={{ alignItems: "center", gap: s[4], paddingVertical: s[8] }}>
        <CircleCheck size={40} color={theme.colors.successStrong} />
        <Text role="title" align="center" accessibilityLiveRegion="polite">
          {done || "Thank you! Your response was sent."}
        </Text>
        <Button
          variant="secondary"
          onPress={() => {
            setAnswers(fresh());
            setDone(null);
          }}
        >
          Send another response
        </Button>
      </View>
    );
  }
  const send = async () => {
    const values: Record<string, DocsValue> = {};
    const errs: Record<string, string> = {};
    for (const q of form.questions) {
      const r = toValue(q, answers[q.property.id] ?? null);
      if (r.error) errs[q.property.id] = r.error;
      else if (q.required && isBlank(r.value) && q.property.type !== "checkbox") errs[q.property.id] = "Answer this question.";
      else if (!isBlank(r.value) || q.property.type === "checkbox") values[q.property.id] = r.value;
    }
    setErrors(errs);
    setFailure(null);
    if (Object.keys(errs).length) return;
    try {
      const res = await submit(values);
      setDone(res.message ?? form.success_message ?? "");
    } catch (e) {
      if (isApiError(e) && e.code === "form_closed") setFailure("This form isn't taking responses anymore.");
      else setFailure(errorMessage(e, "Couldn't send your response. Try again."));
    }
  };
  return (
    <View style={{ gap: s[5] }}>
      <View style={{ gap: s[2] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text role="headline" accessibilityRole="header" style={{ flexShrink: 1 }}>
            {form.title}
          </Text>
          <InfoTip content={form.anonymous || !signedIn ? "Your name isn't recorded with your answers." : "Your name is recorded with your answers."} />
        </View>
        {form.description ? <Text role="body">{form.description}</Text> : null}
      </View>
      {form.questions.map((q) => (
        <Question
          key={q.property.id}
          q={q}
          answer={answers[q.property.id] ?? emptyAnswer(q.property)}
          error={errors[q.property.id]}
          onChange={(a) => {
            setAnswers((cur) => ({ ...cur, [q.property.id]: a }));
            if (errors[q.property.id]) setErrors(({ [q.property.id]: _gone, ...rest }) => rest);
          }}
        />
      ))}
      {failure ? <Banner tone="danger" title={failure} /> : null}
      <Button size="lg" loading={submitting} onPress={() => void send()}>
        {form.submit_label || "Send"}
      </Button>
    </View>
  );
}

/** A form view inside its database: fill it here; editors see the response count and edit the form. */
export function FormView({ ctx }: { ctx: ViewCtx }) {
  const theme = useTheme();
  const s = theme.space;
  const form = useDocsForm(ctx.token ? null : ctx.view.id);
  const submit = useDocsSubmitForm(ctx.view.id);
  const [editing, setEditing] = useState(false);
  if (ctx.token) {
    return (
      <Text role="label" tone="muted">
        This form takes responses through its own link.
      </Text>
    );
  }
  if (form.isPending) return <Skeleton height={240} />;
  if (form.isError) {
    if (form.error.status === 403 || form.error.status === 404) return <EmptyState icon={Lock} title="You can't open this form" compact />;
    return <ErrorState title="Couldn't load the form." info={errorMessage(form.error)} onRetry={() => void form.refetch()} compact />;
  }
  return (
    <View style={{ gap: s[4] }}>
      {ctx.editable ? (
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text role="label" tone="muted" style={{ flex: 1 }}>
            {form.data.responses !== undefined ? `${form.data.responses} ${form.data.responses === 1 ? "response" : "responses"}` : ""}
          </Text>
          <Button variant="secondary" size="md" leftIcon={ClipboardList} onPress={() => setEditing(true)}>
            Edit form
          </Button>
        </View>
      ) : null}
      <View style={{ padding: s[4], borderRadius: theme.radius.lg, borderWidth: 1, borderColor: theme.colors.border, backgroundColor: theme.colors.surface }}>
        <FormFill form={form.data} submit={(v) => submit.mutateAsync(v)} submitting={submit.isPending} signedIn />
      </View>
      {ctx.editable ? <FormBuilder ctx={ctx} open={editing} onClose={() => setEditing(false)} /> : null}
    </View>
  );
}

/** Edits a form view's questions and settings (config.form). */
export function FormBuilder({ ctx, open, onClose }: { ctx: ViewCtx; open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const [draft, setDraft] = useState<DocsFormConfig>(() => ctx.config.form ?? { questions: [] });
  const [adding, setAdding] = useState(false);
  useEffect(() => {
    if (open) {
      setDraft(ctx.config.form ?? { questions: [] });
      setAdding(false);
    }
  }, [open, ctx.config.form]);
  const set = (patch: Partial<DocsFormConfig>) => setDraft((d) => ({ ...d, ...patch }));
  const setQ = (i: number, patch: Partial<DocsFormQuestion>) => set({ questions: draft.questions.map((q, j) => (j === i ? { ...q, ...patch } : q)) });
  const move = (i: number, d: -1 | 1) => {
    const qs = [...draft.questions];
    const j = i + d;
    if (j < 0 || j >= qs.length) return;
    [qs[i], qs[j]] = [qs[j]!, qs[i]!];
    set({ questions: qs });
  };
  const addable = ctx.schema.filter((p) => formAnswerable(p) && !draft.questions.some((q) => q.property === p.id));
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title="Edit form"
      snapPoints={["90%"]}
      footer={
        <Button
          onPress={() => {
            ctx.saveConfig({ ...ctx.config, form: draft });
            onClose();
          }}
        >
          Save
        </Button>
      }
    >
      <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={{ gap: s[4], paddingBottom: s[4] }}>
        <Switch label="Taking responses" value={!draft.closed} onValueChange={(v) => set({ closed: !v })} />
        <Switch label="Anonymous responses" info="People's names aren't recorded with what they send." value={!!draft.anonymous} onValueChange={(v) => set({ anonymous: v })} />
        <Field label="Title">
          <Input value={draft.title ?? ""} onChangeText={(t) => set({ title: t })} placeholder={ctx.view.name} maxLength={200} />
        </Field>
        <Field label="Description">
          <TextArea value={draft.description ?? ""} onChangeText={(t) => set({ description: t })} maxLength={2000} />
        </Field>
        <Text role="label" weight={600}>
          Questions
        </Text>
        {draft.questions.map((q, i) => {
          const p = ctx.schema.find((x) => x.id === q.property);
          if (!p) return null;
          return (
            <View key={q.property} style={{ gap: s[2], padding: s[3], borderRadius: theme.radius.md, backgroundColor: theme.colors.sunken }}>
              <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
                <Text role="label" tone="muted" style={{ flex: 1 }} numberOfLines={1}>
                  {p.name}
                </Text>
                <IconButton label={`Move ${p.name} up`} icon={ArrowUp} variant="ghost" disabled={i === 0} onPress={() => move(i, -1)} />
                <IconButton label={`Move ${p.name} down`} icon={ArrowDown} variant="ghost" disabled={i === draft.questions.length - 1} onPress={() => move(i, 1)} />
                <IconButton label={`Remove ${p.name}`} icon={X} variant="ghost" onPress={() => set({ questions: draft.questions.filter((_, j) => j !== i) })} />
              </View>
              <Input value={q.label ?? ""} onChangeText={(t) => setQ(i, { label: t })} placeholder={p.name} accessibilityLabel={`Question for ${p.name}`} maxLength={200} />
              <Input value={q.description ?? ""} onChangeText={(t) => setQ(i, { description: t })} placeholder="Help text" accessibilityLabel={`Help text for ${p.name}`} maxLength={500} />
              <Switch label="Required" value={!!q.required} onValueChange={(v) => setQ(i, { required: v })} />
            </View>
          );
        })}
        {adding ? (
          <View>
            {addable.map((p) => (
              <ListRow
                key={p.id}
                title={p.name}
                chevron={false}
                onPress={() => {
                  set({ questions: [...draft.questions, { property: p.id }] });
                  setAdding(false);
                }}
              />
            ))}
            {!addable.length ? (
              <Text role="label" tone="muted">
                Every property a form can ask for is already a question.
              </Text>
            ) : null}
          </View>
        ) : (
          <Button variant="secondary" leftIcon={Plus} onPress={() => setAdding(true)}>
            Add a question
          </Button>
        )}
        <Field label="Button">
          <Input value={draft.submit_label ?? ""} onChangeText={(t) => set({ submit_label: t })} placeholder="Send" maxLength={40} />
        </Field>
        <Field label="After sending">
          <TextArea value={draft.success_message ?? ""} onChangeText={(t) => set({ success_message: t })} placeholder="Thank you! Your response was sent." maxLength={500} />
        </Field>
      </ScrollView>
    </Sheet>
  );
}

/** docs/forms/[viewId]: a form shared with you, filled while signed in. */
export function FormScreen() {
  const { viewId } = useLocalSearchParams<{ viewId: string }>();
  const form = useDocsForm(viewId);
  const submit = useDocsSubmitForm(viewId ?? "");
  return (
    <Screen edges={["bottom"]} scroll keyboard {...(form.isError ? { centered: true } : {})}>
      <Stack.Screen options={{ title: form.data?.title ?? "" }} />
      {form.isPending ? (
        <>
          <Skeleton height={32} width="70%" />
          <Skeleton height={48} />
          <Skeleton height={48} />
        </>
      ) : form.isError ? (
        form.error.status === 403 || form.error.status === 404 ? (
          <EmptyState icon={Lock} title="You can't open this form" />
        ) : (
          <ErrorState title="Couldn't load the form." info={errorMessage(form.error)} onRetry={() => void form.refetch()} />
        )
      ) : (
        <FormFill form={form.data} submit={(v) => submit.mutateAsync(v)} submitting={submit.isPending} signedIn />
      )}
    </Screen>
  );
}

/** p/form/[token]: a public form, filled without an account. */
export function PublicFormScreen() {
  const { token } = useLocalSearchParams<{ token: string }>();
  const form = useDocsPublicForm(token);
  const submit = useDocsPublicSubmitForm(token ?? "");
  return (
    <Screen edges={["bottom"]} scroll keyboard {...(form.isError ? { centered: true } : {})}>
      <Stack.Screen options={{ title: form.data?.title ?? "" }} />
      {form.isPending ? (
        <>
          <Skeleton height={32} width="70%" />
          <Skeleton height={48} />
          <Skeleton height={48} />
        </>
      ) : form.isError ? (
        form.error.status === 404 || form.error.status === 403 ? (
          <EmptyState icon={Lock} title="This form's link doesn't work anymore" />
        ) : (
          <ErrorState title="Couldn't load the form." info={errorMessage(form.error)} onRetry={() => void form.refetch()} />
        )
      ) : (
        <FormFill form={form.data} submit={(v) => submit.mutateAsync(v)} submitting={submit.isPending} signedIn={false} />
      )}
    </Screen>
  );
}
