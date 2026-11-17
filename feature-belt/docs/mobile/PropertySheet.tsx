import {
  DOCS_STATUS_GROUP_LABEL,
  docsCurrencyCode,
  docsIsComputed,
  docsNumberScale,
  errorMessage,
  useDocsPressButton,
  useDocsSetRowValues,
  type DocsDateValue,
  type DocsFileValue,
  type DocsOption,
  type DocsProperty,
  type DocsRefs,
  type DocsValue,
} from "@scaffoldapp/shared";
import { Button, Field, Input, ListRow, Sheet, Switch, Text, TextArea, toast, useTheme } from "@scaffoldapp/ui-native";
import * as WebBrowser from "expo-web-browser";
import { Check, ExternalLink, Mail, Phone } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Linking, ScrollView, View } from "react-native";
import { DateEditor, dateDraft, dateFromDraft, FilesEditor, PersonPicker, RelationPicker, type DateDraft } from "./database/editors";
import { openButtonResult } from "./database/open";
import { OptionDot, ValueView, type DocsRowMeta } from "./database/values";

const PHONE_RE = /^\+?[0-9 ().\-/]{3,30}(\s*(x|ext\.?)\s*[0-9]{1,6})?$/;
const EMAIL_RE = /^[^@\s]+@[^@\s]+\.[^@\s]+$/;
const URL_RE = /^https?:\/\/[^\s.]+\.[^\s]+$/i;

const COMPUTED_INFO: Partial<Record<DocsProperty["type"], string>> = {
  formula: "Worked out by a formula from this row's properties.",
  rollup: "Worked out from the related rows you can open.",
  created_time: "When the row was added.",
  created_by: "Who added the row.",
  last_edited_time: "When the row last changed.",
  last_edited_by: "Who changed the row last.",
  unique_id: "Numbered when the row was added.",
};

export interface PropertySheetProps {
  prop: DocsProperty | null;
  value: DocsValue | undefined;
  orgId: string;
  open: boolean;
  onClose: () => void;
  /** Saves the value (the caller closes the sheet). */
  onSave: (v: DocsValue) => void;
  /** Names of people and related rows. */
  refs?: DocsRefs | undefined;
  /** The row and its database: files upload to the row and save as they change, buttons run. */
  rowId?: string | null | undefined;
  dbId?: string | null | undefined;
  /** Created / edited time and people for read-only values. */
  row?: DocsRowMeta | undefined;
  onOpenPage?: ((id: string) => void) | undefined;
  /** Show the value without changing it (the viewer can't edit the row). */
  readOnly?: boolean;
  /** A public link: files open through it. */
  token?: string | null;
}

function numberText(p: DocsProperty, v: DocsValue | undefined): string {
  if (typeof v !== "number") return "";
  return String(Math.round((v / docsNumberScale(p)) * 1e6) / 1e6);
}

/** Edits one row value in a sheet: every type a person sets; computed values shown; buttons run. */
export function PropertySheet({ prop, value, orgId, open, onClose, onSave, refs, rowId, dbId, row, onOpenPage, readOnly: viewOnly = false, token = null }: PropertySheetProps) {
  const theme = useTheme();
  const s = theme.space;
  const [text, setText] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [chosen, setChosen] = useState<string[]>([]);
  const [date, setDate] = useState<DateDraft>(() => dateDraft(null));
  const [files, setFiles] = useState<DocsFileValue[]>([]);
  const [checked, setChecked] = useState(false);
  const [q, setQ] = useState("");
  const setValues = useDocsSetRowValues(dbId ?? "");
  const press = useDocsPressButton(dbId ?? "");
  useEffect(() => {
    if (!prop || !open) return;
    setError(null);
    setQ("");
    if (prop.type === "date") setDate(dateDraft(value as DocsDateValue | null));
    else if (prop.type === "multi_select" || prop.type === "person" || prop.type === "relation") setChosen(Array.isArray(value) ? (value as string[]) : []);
    else if (prop.type === "files") setFiles(Array.isArray(value) ? (value as DocsFileValue[]) : []);
    else if (prop.type === "checkbox") setChecked(value === true);
    else if (prop.type === "number") setText(numberText(prop, value));
    else setText(typeof value === "string" ? value : "");
  }, [prop, value, open]);
  if (!prop) return null;

  const computed = (docsIsComputed(prop) && prop.type !== "button") || !!prop.read_only;
  const readOnly = computed || (viewOnly && prop.type !== "button");
  const save = () => {
    const t = text.trim();
    switch (prop.type) {
      case "number": {
        if (!t) return onSave(null);
        const n = Number(t.replace(/[,\s$€£¥%]/g, ""));
        if (!Number.isFinite(n)) return setError("Enter a number.");
        return onSave(docsCurrencyCode(prop.number_format) ? Math.round(n * docsNumberScale(prop)) : n);
      }
      case "date": {
        const r = dateFromDraft(date);
        if (r.error) return setError(r.error);
        return onSave(r.value);
      }
      case "email":
        if (t && !EMAIL_RE.test(t)) return setError("Enter an email address.");
        break;
      case "phone":
        if (t && !PHONE_RE.test(t)) return setError("Enter a phone number.");
        break;
      case "url": {
        const u = t && !t.includes("://") ? `https://${t}` : t;
        if (u && !URL_RE.test(u)) return setError("Enter a web address.");
        return onSave(u || null);
      }
      case "multi_select":
      case "person":
      case "relation":
        return onSave(chosen);
      case "checkbox":
        return onSave(checked);
    }
    onSave(t || null);
  };
  const commitFiles = (next: DocsFileValue[]) => {
    setFiles(next);
    if (!rowId || !dbId) return;
    setValues.mutate({ rowId, values: { [prop.id]: next.length ? next : null } }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save the files.")) });
  };
  const runButton = () => {
    if (!rowId || !dbId) return;
    press.mutate(
      { rowId, propId: prop.id },
      {
        onSuccess: (res) => {
          onClose();
          openButtonResult(res);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't run that button.")),
      },
    );
  };

  const single = prop.type === "select" || prop.type === "status";
  const listy = prop.type === "multi_select" || prop.type === "person" || prop.type === "relation";
  const footer =
    readOnly || single || prop.type === "files" || prop.type === "button" ? undefined : (
      <View style={{ flexDirection: "row", gap: s[2] }}>
        {prop.type !== "title" && prop.type !== "checkbox" ? (
          <Button variant="secondary" style={{ flex: 1 }} onPress={() => onSave(listy ? [] : null)}>
            Clear
          </Button>
        ) : null}
        <Button style={{ flex: 1 }} onPress={save}>
          Save
        </Button>
      </View>
    );

  let body;
  if (readOnly && prop.type === "files") {
    body = (
      <ScrollView>
        <FilesEditor rowId={rowId} files={Array.isArray(value) ? (value as DocsFileValue[]) : []} onCommit={() => undefined} readOnly token={token} />
      </ScrollView>
    );
  } else if (readOnly) {
    body = (
      <View style={{ gap: s[3] }}>
        <ValueView prop={prop} value={value} refs={refs ?? { users: {}, pages: {} }} row={row} onOpenPage={onOpenPage} />
        {prop.type === "formula" && prop.formula?.error ? (
          <Text role="label" tone="danger">
            {prop.formula.error}
          </Text>
        ) : null}
      </View>
    );
  } else if (single) {
    const needle = q.trim().toLowerCase();
    const opts = (prop.options ?? []).filter((o) => !needle || o.name.toLowerCase().includes(needle));
    const groups: { label: string | null; options: DocsOption[] }[] =
      prop.type === "status"
        ? (["todo", "doing", "done"] as const).map((g) => ({ label: DOCS_STATUS_GROUP_LABEL[g], options: opts.filter((o) => (o.group ?? "todo") === g) })).filter((g) => g.options.length > 0)
        : [{ label: null, options: opts }];
    body = (
      <View style={{ flex: 1, gap: s[3] }}>
        {(prop.options ?? []).length > 8 ? <Input value={q} onChangeText={setQ} placeholder="Search options" accessibilityLabel="Search options" autoCorrect={false} /> : null}
        <ScrollView keyboardShouldPersistTaps="handled">
          {groups.map((g) => (
            <View key={g.label ?? "all"}>
              {g.label ? (
                <Text role="caption" tone="muted" weight={600} style={{ paddingTop: s[2] }}>
                  {g.label}
                </Text>
              ) : null}
              {g.options.map((o) => (
                <ListRow
                  key={o.id}
                  title={o.name}
                  leading={<OptionDot color={o.color} />}
                  trailing={value === o.id ? <Check size={18} color={theme.colors.primaryStrong} accessibilityLabel="Chosen" /> : undefined}
                  onPress={() => onSave(value === o.id ? null : o.id)}
                  chevron={false}
                />
              ))}
            </View>
          ))}
          {!(prop.options ?? []).length ? (
            <Text role="label" tone="muted">
              No options yet. Add them on the web.
            </Text>
          ) : null}
          {value ? <ListRow title="Clear" onPress={() => onSave(null)} chevron={false} /> : null}
        </ScrollView>
      </View>
    );
  } else if (prop.type === "multi_select") {
    body = (
      <ScrollView>
        {(prop.options ?? []).map((o) => {
          const on = chosen.includes(o.id);
          return (
            <ListRow
              key={o.id}
              title={o.name}
              leading={<OptionDot color={o.color} />}
              trailing={on ? <Check size={18} color={theme.colors.primaryStrong} accessibilityLabel="Chosen" /> : undefined}
              onPress={() => setChosen((c) => (on ? c.filter((x) => x !== o.id) : [...c, o.id]))}
              chevron={false}
            />
          );
        })}
      </ScrollView>
    );
  } else if (prop.type === "person") {
    body = <PersonPicker orgId={orgId} chosen={chosen} onChange={setChosen} refs={refs} enabled={open} />;
  } else if (prop.type === "relation") {
    body = <RelationPicker prop={prop} chosen={chosen} onChange={setChosen} refs={refs} />;
  } else if (prop.type === "date") {
    body = (
      <ScrollView keyboardShouldPersistTaps="handled">
        <DateEditor
          prop={prop}
          draft={date}
          onChange={(d) => {
            setDate(d);
            setError(null);
          }}
          error={error}
        />
      </ScrollView>
    );
  } else if (prop.type === "files") {
    body = (
      <ScrollView keyboardShouldPersistTaps="handled">
        <FilesEditor
          rowId={rowId}
          files={files}
          onCommit={
            rowId && dbId
              ? commitFiles
              : (next) => {
                  setFiles(next);
                  onSave(next.length ? next : null);
                }
          }
        />
      </ScrollView>
    );
  } else if (prop.type === "checkbox") {
    body = <Switch label={prop.name} value={checked} onValueChange={setChecked} />;
  } else if (prop.type === "button") {
    const steps = prop.button?.actions.length ?? 0;
    body = (
      <View style={{ gap: s[3] }}>
        <Text role="body">{`Runs ${steps} ${steps === 1 ? "step" : "steps"} on this row.`}</Text>
        <Button loading={press.isPending} disabled={!rowId || !dbId || viewOnly} onPress={runButton}>
          {prop.button?.label || prop.name}
        </Button>
      </View>
    );
  } else {
    const currency = prop.type === "number" ? docsCurrencyCode(prop.number_format) : null;
    const field =
      prop.type === "text" ? (
        <TextArea
          value={text}
          onChangeText={(t) => {
            setText(t);
            setError(null);
          }}
          autoFocus
          accessibilityLabel={prop.name}
        />
      ) : (
        <Input
          value={text}
          onChangeText={(t) => {
            setText(t);
            setError(null);
          }}
          autoFocus
          keyboardType={prop.type === "number" ? "decimal-pad" : prop.type === "email" ? "email-address" : prop.type === "url" ? "url" : prop.type === "phone" ? "phone-pad" : "default"}
          autoCapitalize={prop.type === "title" ? "sentences" : "none"}
          autoCorrect={prop.type === "title"}
          returnKeyType="done"
          onSubmitEditing={save}
          accessibilityLabel={prop.name}
        />
      );
    const unit = currency ? currency.toUpperCase() : prop.type === "number" && prop.number_format === "percent" ? "%" : null;
    const current = typeof value === "string" && value ? value : "";
    body = (
      <View style={{ gap: s[3] }}>
        <Field
          label={prop.name}
          error={error}
          labelEnd={
            unit ? (
              <Text role="label" tone="muted">
                {unit}
              </Text>
            ) : undefined
          }
        >
          {field}
        </Field>
        {current && prop.type === "url" ? (
          <Button variant="ghost" leftIcon={ExternalLink} onPress={() => void WebBrowser.openBrowserAsync(current)}>
            Open link
          </Button>
        ) : null}
        {current && prop.type === "email" ? (
          <Button variant="ghost" leftIcon={Mail} onPress={() => void Linking.openURL(`mailto:${current}`)}>
            Send email
          </Button>
        ) : null}
        {current && prop.type === "phone" ? (
          <Button variant="ghost" leftIcon={Phone} onPress={() => void Linking.openURL(`tel:${current.replace(/[^0-9+]/g, "")}`)}>
            Call
          </Button>
        ) : null}
      </View>
    );
  }

  const info = computed ? prop.description || COMPUTED_INFO[prop.type] || "Comes from where this database is connected." : prop.description || undefined;
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={prop.name}
      {...(info ? { info } : {})}
      snapPoints={readOnly || prop.type === "checkbox" || prop.type === "button" ? ["40%"] : prop.type === "date" || listy || prop.type === "files" ? ["85%"] : ["60%"]}
      footer={footer}
    >
      {body}
    </Sheet>
  );
}
