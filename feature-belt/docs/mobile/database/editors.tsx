import {
  docsFormatDateValue,
  docsTimeZone,
  docsTitle,
  errorMessage,
  useAccessDirectory,
  useDebouncedValue,
  useDocsFileUrl,
  useDocsRows,
  useDocsUploadFile,
  type DocsDateValue,
  type DocsFileValue,
  type DocsProperty,
  type DocsRefs,
  type DocsReminder,
} from "@scaffoldapp/shared";
import { Avatar, Button, Chip, Field, FileIcon, FilePreview, IconButton, Input, ListRow, Skeleton, Switch, Text, pickPhotos, toast, useTheme } from "@scaffoldapp/ui-native";
import * as DocumentPicker from "expo-document-picker";
import * as WebBrowser from "expo-web-browser";
import { Check, FileUp, ImagePlus, Link2, Lock, X } from "lucide-react-native";
import { useState } from "react";
import { ScrollView, View } from "react-native";
import { addDays, dayOf, normTime, timeOf, todayYmd, YMD_RE } from "./dates";
import { MonthGrid } from "./MonthGrid";

// ---- dates -----------------------------------------------------------------------------

export const REMINDER_LABEL: Record<DocsReminder, string> = {
  at_time: "At the time",
  "5m": "5 minutes before",
  "10m": "10 minutes before",
  "15m": "15 minutes before",
  "30m": "30 minutes before",
  "1h": "1 hour before",
  "2h": "2 hours before",
  "1d": "1 day before",
  "2d": "2 days before",
  "1w": "1 week before",
};

const DAY_REMINDERS: DocsReminder[] = ["at_time", "1d", "2d", "1w"];
const TIME_REMINDERS: DocsReminder[] = ["at_time", "5m", "10m", "15m", "30m", "1h", "2h", "1d", "2d", "1w"];

export interface DateDraft {
  start: string;
  end: string;
  withEnd: boolean;
  withTime: boolean;
  startTime: string;
  endTime: string;
  remind: DocsReminder | "";
}

export function dateDraft(v: DocsDateValue | null | undefined): DateDraft {
  return {
    start: dayOf(v?.start),
    end: dayOf(v?.end),
    withEnd: !!v?.end,
    withTime: !!timeOf(v?.start),
    startTime: timeOf(v?.start) || "09:00",
    endTime: timeOf(v?.end) || timeOf(v?.start) || "10:00",
    remind: v?.remind ?? "",
  };
}

/** The value a draft saves, or an error to show. */
export function dateFromDraft(d: DateDraft): { value: DocsDateValue | null; error?: string } {
  if (!d.start) return { value: null };
  if (!YMD_RE.test(d.start)) return { value: null, error: "Choose a day." };
  let start = d.start;
  let end = d.withEnd && d.end ? d.end : "";
  if (d.withTime) {
    const st = normTime(d.startTime);
    if (!st) return { value: null, error: "Enter a start time like 09:30." };
    start = `${d.start}T${st}`;
    if (end) {
      const et = normTime(d.endTime);
      if (!et) return { value: null, error: "Enter an end time like 10:30." };
      end = `${end}T${et}`;
    }
  }
  if (end && end < start) return { value: null, error: "The end is before the start." };
  const out: DocsDateValue = { start };
  if (end) out.end = end;
  if (d.remind) out.remind = d.remind;
  if (d.withTime || d.remind) out.tz = docsTimeZone();
  return { value: out };
}

/** Day, optional end, optional times and a reminder. */
export function DateEditor({ prop, draft, onChange, error, allowReminder = true }: { prop: DocsProperty; draft: DateDraft; onChange: (d: DateDraft) => void; error?: string | null | undefined; allowReminder?: boolean }) {
  const theme = useTheme();
  const s = theme.space;
  const set = (patch: Partial<DateDraft>) => onChange({ ...draft, ...patch });
  const [picking, setPicking] = useState<"start" | "end">("start");
  const pressDay = (day: string) => {
    if (!draft.withEnd || picking === "start" || !draft.start) {
      set({ start: day, ...(draft.withEnd && draft.end && draft.end < day ? { end: day } : {}) });
      if (draft.withEnd) setPicking("end");
      return;
    }
    if (day < draft.start) set({ start: day });
    else {
      set({ end: day });
      setPicking("start");
    }
  };
  const preview = dateFromDraft(draft);
  const reminders = draft.withTime ? TIME_REMINDERS : DAY_REMINDERS;
  return (
    <View style={{ gap: s[4] }}>
      <View style={{ flexDirection: "row", gap: s[2], flexWrap: "wrap" }}>
        {(
          [
            ["Today", todayYmd()],
            ["Tomorrow", addDays(todayYmd(), 1)],
            ["Next week", addDays(todayYmd(), 7)],
          ] as const
        ).map(([label, d]) => (
          <Chip key={label} size="sm" selected={draft.start === d && !draft.withEnd} onPress={() => set({ start: d })}>
            {label}
          </Chip>
        ))}
      </View>
      <MonthGrid initial={draft.start || undefined} start={draft.start || undefined} end={draft.withEnd ? draft.end || undefined : undefined} onPressDay={pressDay} />
      <Text role="label" tone={error || preview.error ? "danger" : "muted"} accessibilityLiveRegion="polite">
        {error || preview.error || (preview.value ? docsFormatDateValue(preview.value, { ...prop, time_format: prop.time_format ?? "" }) : "No date")}
      </Text>
      <Switch
        label="End date"
        value={draft.withEnd}
        onValueChange={(on) => {
          set({ withEnd: on, end: on ? draft.end || draft.start : "" });
          setPicking(on ? "end" : "start");
        }}
      />
      <Switch label="Include time" value={draft.withTime} onValueChange={(on) => set({ withTime: on })} />
      {draft.withTime ? (
        <View style={{ flexDirection: "row", gap: s[3] }}>
          <Field label="Start time" style={{ flex: 1 }}>
            <Input value={draft.startTime} onChangeText={(t) => set({ startTime: t })} placeholder="09:00" keyboardType="numbers-and-punctuation" accessibilityLabel="Start time" />
          </Field>
          {draft.withEnd ? (
            <Field label="End time" style={{ flex: 1 }}>
              <Input value={draft.endTime} onChangeText={(t) => set({ endTime: t })} placeholder="10:00" keyboardType="numbers-and-punctuation" accessibilityLabel="End time" />
            </Field>
          ) : null}
        </View>
      ) : null}
      {allowReminder ? (
        <View style={{ gap: s[2] }}>
          <Text role="label" weight={600}>
            Remind
          </Text>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2] }}>
            <Chip size="sm" selected={!draft.remind} onPress={() => set({ remind: "" })}>
              None
            </Chip>
            {reminders.map((r) => (
              <Chip key={r} size="sm" selected={draft.remind === r} onPress={() => set({ remind: r })}>
                {r === "at_time" && !draft.withTime ? "On the day (9:00)" : REMINDER_LABEL[r]}
              </Chip>
            ))}
          </ScrollView>
        </View>
      ) : null}
    </View>
  );
}

// ---- people ----------------------------------------------------------------------------

/** People in the workspace: search, pick one or many. */
export function PersonPicker({ orgId, chosen, onChange, refs, single = false, enabled = true }: { orgId: string; chosen: string[]; onChange: (ids: string[]) => void; refs?: DocsRefs | undefined; single?: boolean; enabled?: boolean }) {
  const theme = useTheme();
  const [q, setQ] = useState("");
  const [names, setNames] = useState<Record<string, { name: string; avatar_url: string | null }>>({});
  const people = useAccessDirectory(orgId, q, { types: "user", limit: 20, enabled });
  const nameOf = (id: string) => names[id] ?? refs?.users[id] ?? null;
  const toggle = (id: string, name: string, avatar: string | null) => {
    setNames((m) => ({ ...m, [id]: { name, avatar_url: avatar } }));
    const on = chosen.includes(id);
    onChange(single ? (on ? [] : [id]) : on ? chosen.filter((x) => x !== id) : [...chosen, id]);
  };
  const results = (people.data ?? []).filter((p) => !chosen.includes(p.id));
  return (
    <View style={{ gap: theme.space[3], flex: 1 }}>
      <Input value={q} onChangeText={setQ} placeholder="Search people" accessibilityLabel="Search people" autoCorrect={false} autoCapitalize="none" />
      <ScrollView keyboardShouldPersistTaps="handled">
        {chosen.map((id) => {
          const u = nameOf(id);
          return (
            <ListRow
              key={id}
              title={u?.name ?? "Someone"}
              leading={<Avatar name={u?.name ?? "?"} src={u?.avatar_url ?? null} size="sm" />}
              trailing={<Check size={18} color={theme.colors.primaryStrong} />}
              onPress={() => toggle(id, u?.name ?? "", u?.avatar_url ?? null)}
              chevron={false}
              accessibilityHint="Removes this person"
            />
          );
        })}
        {people.isPending && enabled ? <Skeleton height={theme.tap.base} /> : null}
        {people.isError ? (
          <Text role="label" tone="danger">
            Couldn't search people.
          </Text>
        ) : null}
        {results.map((p) => (
          <ListRow key={p.id} title={p.name} leading={<Avatar name={p.name} src={p.avatar_url} size="sm" />} onPress={() => toggle(p.id, p.name, p.avatar_url ?? null)} chevron={false} />
        ))}
        {!people.isPending && !results.length && q.trim() ? (
          <Text role="label" tone="muted">
            No one matches.
          </Text>
        ) : null}
      </ScrollView>
    </View>
  );
}

// ---- relations -------------------------------------------------------------------------

/** Rows of the related database the viewer can open: search and pick (one when limited). */
export function RelationPicker({ prop, chosen, onChange, refs }: { prop: DocsProperty; chosen: string[]; onChange: (ids: string[]) => void; refs?: DocsRefs | undefined }) {
  const theme = useTheme();
  const [q, setQ] = useState("");
  const dq = useDebouncedValue(q.trim(), 250);
  const target = prop.relation?.database_id ?? null;
  const single = prop.relation?.limit === 1;
  const rows = useDocsRows(target, { limit: 30, tz: docsTimeZone(), ...(dq ? { q: dq } : {}) }, { enabled: !!target });
  const [titles, setTitles] = useState<Record<string, string>>({});
  const titleOf = (id: string): string | null => {
    if (titles[id] !== undefined) return titles[id]!;
    const ref = refs?.pages[id];
    if (ref) return ref.title === null ? null : docsTitle(ref.title);
    const r = rows.data?.rows.find((x) => x.id === id);
    return r ? docsTitle(r.title) : null;
  };
  const toggle = (id: string, title: string) => {
    setTitles((m) => ({ ...m, [id]: title }));
    const on = chosen.includes(id);
    onChange(single ? (on ? [] : [id]) : on ? chosen.filter((x) => x !== id) : [...chosen, id]);
  };
  if (!target) {
    return (
      <Text role="label" tone="muted">
        This relation isn't linked to a database.
      </Text>
    );
  }
  const visibleChosen = chosen.filter((id) => !refs || refs.pages[id] || titles[id] !== undefined || rows.data?.rows.some((r) => r.id === id));
  const results = (rows.data?.rows ?? []).filter((r) => !chosen.includes(r.id));
  return (
    <View style={{ gap: theme.space[3], flex: 1 }}>
      <Input value={q} onChangeText={setQ} placeholder="Search rows" accessibilityLabel="Search rows" autoCorrect={false} />
      <ScrollView keyboardShouldPersistTaps="handled">
        {visibleChosen.map((id) => {
          const t = titleOf(id);
          return t === null ? (
            <ListRow key={id} title="No access" leading={<Lock size={18} color={theme.colors.textMuted} />} info="A row you can't open. It stays linked." chevron={false} />
          ) : (
            <ListRow key={id} title={t} trailing={<Check size={18} color={theme.colors.primaryStrong} />} onPress={() => toggle(id, t)} chevron={false} accessibilityHint="Removes this link" />
          );
        })}
        {rows.isPending ? <Skeleton height={theme.tap.base} /> : null}
        {rows.isError ? (
          <Text role="label" tone="danger">
            Couldn't load rows of that database.
          </Text>
        ) : null}
        {results.map((r) => (
          <ListRow key={r.id} title={`${r.icon ? `${r.icon} ` : ""}${docsTitle(r.title)}`} onPress={() => toggle(r.id, docsTitle(r.title))} chevron={false} />
        ))}
        {!rows.isPending && !results.length ? (
          <Text role="label" tone="muted">
            {dq ? "No rows match." : "No other rows to link."}
          </Text>
        ) : null}
      </ScrollView>
    </View>
  );
}

// ---- files -----------------------------------------------------------------------------

function UploadedFile({ file, token, onRemove }: { file: DocsFileValue; token?: string | null; onRemove?: (() => void) | undefined }) {
  const theme = useTheme();
  const url = useDocsFileUrl(file.file_id ?? null, token);
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
      <View style={{ flex: 1 }}>
        {url.data?.url ? (
          <FilePreview url={url.data.url} name={file.name} mime={file.mime ?? url.data.content_type} size={url.data.size} compact />
        ) : url.isError ? (
          <ListRow title={file.name} leading={<FileIcon name={file.name} mime={file.mime ?? null} />} info="Couldn't load this file." />
        ) : (
          <Skeleton height={theme.tap.lg} />
        )}
      </View>
      {onRemove ? <IconButton label={`Remove ${file.name}`} icon={X} variant="ghost" onPress={onRemove} /> : null}
    </View>
  );
}

/** A files property: uploads (on the row's page) and links; changes save as they happen. */
export function FilesEditor({ rowId, files, onCommit, readOnly = false, token }: { rowId: string | null | undefined; files: DocsFileValue[]; onCommit: (next: DocsFileValue[]) => void; readOnly?: boolean; token?: string | null }) {
  const theme = useTheme();
  const s = theme.space;
  const upload = useDocsUploadFile(rowId ?? "");
  const [busy, setBusy] = useState(0);
  const [linking, setLinking] = useState(false);
  const [link, setLink] = useState("");
  const [linkName, setLinkName] = useState("");
  const [linkError, setLinkError] = useState<string | null>(null);
  const send = async (picked: { uri: string; name: string; mimeType?: string | null; size?: number | null }[]) => {
    if (!rowId || !picked.length) return;
    setBusy((n) => n + picked.length);
    const added: DocsFileValue[] = [];
    for (const f of picked) {
      try {
        const res = await upload.mutateAsync({ file: { uri: f.uri, name: f.name, mimeType: f.mimeType ?? null, size: f.size ?? null }, name: f.name, ...(f.mimeType ? { contentType: f.mimeType } : {}) });
        added.push({ file_id: res.id, name: res.name, mime: res.content_type });
      } catch (e) {
        toast.error(errorMessage(e, `Couldn't upload ${f.name}.`));
      } finally {
        setBusy((n) => n - 1);
      }
    }
    if (added.length) onCommit([...files, ...added]);
  };
  const addPhotos = async () => {
    const picked = await pickPhotos("library", 10, 0.8);
    await send(picked.map((p, i) => ({ uri: p.uri, name: p.name || `Photo ${i + 1}.jpg`, mimeType: p.mimeType ?? "image/jpeg", size: p.size ?? null })));
  };
  const addDocuments = async () => {
    try {
      const res = await DocumentPicker.getDocumentAsync({ multiple: true, copyToCacheDirectory: true });
      if (res.canceled) return;
      await send(res.assets.map((a) => ({ uri: a.uri, name: a.name, mimeType: a.mimeType ?? null, size: a.size ?? null })));
    } catch {
      toast.error("Couldn't open your files.");
    }
  };
  const addLink = () => {
    const raw = link.trim();
    const url = raw.includes("://") ? raw : `https://${raw}`;
    if (!raw || !/^https?:\/\/[^\s.]+\.[^\s]+$/i.test(url)) return setLinkError("Enter a web address.");
    onCommit([...files, { url, name: linkName.trim() || raw }]);
    setLink("");
    setLinkName("");
    setLinkError(null);
    setLinking(false);
  };
  const remove = (i: number) => onCommit(files.filter((_, j) => j !== i));
  return (
    <View style={{ gap: s[3] }}>
      {files.length === 0 ? (
        <Text role="label" tone="muted">
          No files yet.
        </Text>
      ) : null}
      {files.map((f, i) =>
        f.file_id ? (
          <UploadedFile key={`${f.file_id}${i}`} file={f} token={token ?? null} onRemove={readOnly ? undefined : () => remove(i)} />
        ) : (
          <View key={`${f.url}${i}`} style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
            <View style={{ flex: 1 }}>
              <ListRow title={f.name} leading={<Link2 size={18} color={theme.colors.textMuted} />} onPress={() => f.url && void WebBrowser.openBrowserAsync(f.url)} accessibilityHint="Opens the link" />
            </View>
            {readOnly ? null : <IconButton label={`Remove ${f.name}`} icon={X} variant="ghost" onPress={() => remove(i)} />}
          </View>
        ),
      )}
      {busy > 0 ? <Skeleton height={theme.tap.lg} /> : null}
      {readOnly ? null : linking ? (
        <View style={{ gap: s[2] }}>
          <Field label="Link" error={linkError}>
            <Input value={link} onChangeText={setLink} placeholder="https://" keyboardType="url" autoCapitalize="none" autoCorrect={false} autoFocus returnKeyType="next" />
          </Field>
          <Field label="Name">
            <Input value={linkName} onChangeText={setLinkName} returnKeyType="done" onSubmitEditing={addLink} />
          </Field>
          <View style={{ flexDirection: "row", gap: s[2] }}>
            <Button variant="secondary" style={{ flex: 1 }} onPress={() => setLinking(false)}>
              Cancel
            </Button>
            <Button style={{ flex: 1 }} onPress={addLink}>
              Add link
            </Button>
          </View>
        </View>
      ) : (
        <View style={{ flexDirection: "row", gap: s[2], flexWrap: "wrap" }}>
          {rowId ? (
            <>
              <Chip size="sm" icon={ImagePlus} onPress={() => void addPhotos()}>
                Photo
              </Chip>
              <Chip size="sm" icon={FileUp} onPress={() => void addDocuments()}>
                File
              </Chip>
            </>
          ) : null}
          <Chip size="sm" icon={Link2} onPress={() => setLinking(true)}>
            Link
          </Chip>
        </View>
      )}
    </View>
  );
}
