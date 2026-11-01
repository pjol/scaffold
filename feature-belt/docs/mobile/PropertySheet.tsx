import { docsFormatDate, docsToday, useAccessDirectory, type DocsDateValue, type DocsProperty, type DocsValue } from "@scaffoldapp/shared";
import { Avatar, Badge, Button, Chip, Field, Input, ListRow, Sheet, Text, useTheme } from "@scaffoldapp/ui-native";
import { Check } from "lucide-react-native";
import { useEffect, useState } from "react";
import { ScrollView, View } from "react-native";

/** Edits one row value in a sheet (options, people, text, number, date). */
export function PropertySheet({ prop, value, orgId, open, onClose, onSave }: { prop: DocsProperty | null; value: DocsValue | undefined; orgId: string; open: boolean; onClose: () => void; onSave: (v: DocsValue) => void }) {
  const theme = useTheme();
  const [text, setText] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [chosen, setChosen] = useState<string[]>([]);
  const people = useAccessDirectory(orgId, q, { types: "user", limit: 15, enabled: open && prop?.type === "person" });
  useEffect(() => {
    if (!prop) return;
    setError(null);
    setQ("");
    if (prop.type === "date") setText((value as DocsDateValue | null)?.start?.slice(0, 10) ?? "");
    else if (prop.type === "multi_select" || prop.type === "person") setChosen(Array.isArray(value) ? (value as string[]) : []);
    else setText(value === null || value === undefined ? "" : String(value));
  }, [prop, value, open]);
  if (!prop) return null;
  const save = () => {
    const t = text.trim();
    if (prop.type === "number") {
      if (!t) return onSave(null);
      const n = Number(t.replace(/[,\s$€£%]/g, ""));
      if (!Number.isFinite(n)) return setError("Enter a number.");
      return onSave(n);
    }
    if (prop.type === "date") {
      if (!t) return onSave(null);
      if (!/^\d{4}-\d{2}-\d{2}$/.test(t)) return setError("Enter a date like 2026-11-20.");
      const end = (value as DocsDateValue | null)?.end;
      return onSave(end && end >= t ? { start: t, end } : { start: t });
    }
    if (prop.type === "email" && t && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(t)) return setError("Enter an email address.");
    if (prop.type === "url" && t && !/^https?:\/\/\S+$/i.test(t.includes("://") ? t : `https://${t}`)) return setError("Enter a web address.");
    onSave(t ? (prop.type === "url" && !t.includes("://") ? `https://${t}` : t) : null);
  };
  const single = prop.type === "select" || prop.type === "status";
  const multi = prop.type === "multi_select" || prop.type === "person";
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={prop.name}
      snapPoints={["70%"]}
      footer={
        single ? undefined : (
          <View style={{ flexDirection: "row", gap: theme.space[2] }}>
            <Button variant="secondary" style={{ flex: 1 }} onPress={() => onSave(multi ? [] : null)}>
              Clear
            </Button>
            <Button style={{ flex: 1 }} onPress={() => (multi ? onSave(chosen) : save())}>
              Save
            </Button>
          </View>
        )
      }
    >
      {single ? (
        <ScrollView>
          {(prop.options ?? []).map((o) => (
            <ListRow key={o.id} title={o.name} leading={<Badge tone={o.color}>{o.name}</Badge>} trailing={value === o.id ? <Check size={18} color={theme.colors.primaryStrong} /> : undefined} onPress={() => onSave(value === o.id ? null : o.id)} chevron={false} />
          ))}
          <ListRow title="Clear" onPress={() => onSave(null)} chevron={false} />
        </ScrollView>
      ) : prop.type === "multi_select" ? (
        <ScrollView>
          {(prop.options ?? []).map((o) => {
            const on = chosen.includes(o.id);
            return <ListRow key={o.id} title={o.name} leading={<Badge tone={o.color}>{o.name}</Badge>} trailing={on ? <Check size={18} color={theme.colors.primaryStrong} /> : undefined} onPress={() => setChosen((c) => (on ? c.filter((x) => x !== o.id) : [...c, o.id]))} chevron={false} />;
          })}
        </ScrollView>
      ) : prop.type === "person" ? (
        <View style={{ gap: theme.space[3], flex: 1 }}>
          <Input value={q} onChangeText={setQ} placeholder="Search people" accessibilityLabel="Search people" autoCorrect={false} />
          <ScrollView>
            {(people.data ?? []).map((p) => {
              const on = chosen.includes(p.id);
              return <ListRow key={p.id} title={p.name} leading={<Avatar name={p.name} src={p.avatar_url} size="sm" />} trailing={on ? <Check size={18} color={theme.colors.primaryStrong} /> : undefined} onPress={() => setChosen((c) => (on ? c.filter((x) => x !== p.id) : [...c, p.id]))} chevron={false} />;
            })}
          </ScrollView>
        </View>
      ) : (
        <View style={{ gap: theme.space[3] }}>
          <Field label={prop.name} error={error}>
            <Input
              value={text}
              onChangeText={setText}
              autoFocus
              placeholder={prop.type === "date" ? "YYYY-MM-DD" : undefined}
              keyboardType={prop.type === "number" ? "decimal-pad" : prop.type === "email" ? "email-address" : prop.type === "url" ? "url" : "default"}
              autoCapitalize={prop.type === "text" ? "sentences" : "none"}
              returnKeyType="done"
              onSubmitEditing={save}
            />
          </Field>
          {prop.type === "date" ? (
            <View style={{ flexDirection: "row", gap: theme.space[2], flexWrap: "wrap" }}>
              {[
                ["Today", docsToday()],
                ["Tomorrow", docsToday(1)],
                ["Next week", docsToday(7)],
              ].map(([label, d]) => (
                <Chip key={label} onPress={() => setText(d!)}>
                  {label}
                </Chip>
              ))}
              {text && /^\d{4}-\d{2}-\d{2}$/.test(text) ? (
                <Text role="label" tone="muted">
                  {docsFormatDate(text)}
                </Text>
              ) : null}
            </View>
          ) : null}
        </View>
      )}
    </Sheet>
  );
}
