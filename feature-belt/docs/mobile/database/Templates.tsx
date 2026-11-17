import {
  docsTimeZone,
  docsTitle,
  errorMessage,
  useDocsCreateRowTemplate,
  useDocsRowTemplates,
  useDocsSetTemplateRepeat,
  type DocsRepeat,
  type DocsRowTemplate,
} from "@scaffoldapp/shared";
import { Badge, Button, Chip, Field, IconButton, Input, ListRow, SegmentedControl, Sheet, Skeleton, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { ChevronDown, ChevronUp, LayoutTemplate, Plus } from "lucide-react-native";
import { useState } from "react";
import { ScrollView, View } from "react-native";
import type { ViewCtx } from "./context";
import { normTime } from "./dates";

const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

function repeatLabel(r: DocsRepeat | null): string {
  if (!r) return "";
  const every = r.every === "day" ? "Daily" : r.every === "week" ? "Weekly" : "Monthly";
  return `${every} at ${r.time}`;
}

function RepeatEditor({ dbId, t, onDone }: { dbId: string; t: DocsRowTemplate; onDone: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const save = useDocsSetTemplateRepeat(dbId);
  const [every, setEvery] = useState<"off" | DocsRepeat["every"]>(t.repeat?.every ?? "off");
  const [time, setTime] = useState(t.repeat?.time ?? "09:00");
  const [days, setDays] = useState<number[]>(t.repeat?.weekdays ?? [0]);
  const [day, setDay] = useState(String(t.repeat?.day ?? 1));
  const [error, setError] = useState<string | null>(null);
  const apply = () => {
    if (every === "off") {
      save.mutate({ templateId: t.id, repeat: null }, { onSuccess: onDone, onError: (e) => toast.error(errorMessage(e, "Couldn't stop the repeat.")) });
      return;
    }
    const tm = normTime(time);
    if (!tm) return setError("Enter a time like 09:00.");
    const dom = Number(day);
    if (every === "month" && (!Number.isInteger(dom) || dom < 1 || dom > 31)) return setError("Enter a day from 1 to 31.");
    if (every === "week" && !days.length) return setError("Choose at least one day.");
    const repeat: DocsRepeat = { every, interval: t.repeat?.interval ?? 1, time: tm, tz: docsTimeZone(), ...(every === "week" ? { weekdays: days } : {}), ...(every === "month" ? { day: dom } : {}) };
    save.mutate({ templateId: t.id, repeat }, { onSuccess: onDone, onError: (e) => toast.error(errorMessage(e, "Couldn't save the repeat.")) });
  };
  return (
    <View style={{ gap: s[3], padding: s[3], borderRadius: theme.radius.md, backgroundColor: theme.colors.sunken }}>
      <SegmentedControl
        accessibilityLabel="Repeat"
        options={[
          { value: "off", label: "Off" },
          { value: "day", label: "Daily" },
          { value: "week", label: "Weekly" },
          { value: "month", label: "Monthly" },
        ]}
        value={every}
        onValueChange={(v) => {
          setEvery(v as typeof every);
          setError(null);
        }}
      />
      {every === "week" ? (
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
          {WEEKDAYS.map((w, i) => (
            <Chip key={w} size="sm" selected={days.includes(i)} onPress={() => setDays((d) => (d.includes(i) ? d.filter((x) => x !== i) : [...d, i].sort()))}>
              {w}
            </Chip>
          ))}
        </View>
      ) : null}
      {every !== "off" ? (
        <View style={{ flexDirection: "row", gap: s[3] }}>
          {every === "month" ? (
            <Field label="Day of the month" style={{ flex: 1 }}>
              <Input value={day} onChangeText={setDay} keyboardType="number-pad" maxLength={2} />
            </Field>
          ) : null}
          <Field label="Time" style={{ flex: 1 }} error={error}>
            <Input value={time} onChangeText={setTime} keyboardType="numbers-and-punctuation" placeholder="09:00" />
          </Field>
        </View>
      ) : null}
      {error && every === "off" ? (
        <Text role="caption" tone="danger">
          {error}
        </Text>
      ) : null}
      <Button size="md" loading={save.isPending} onPress={apply}>
        Save repeat
      </Button>
    </View>
  );
}

/** "New" for a view: the view's default template applies; the arrow picks another template or an empty row. */
export function NewRowButton({ ctx, creating }: { ctx: ViewCtx; creating: boolean }) {
  const theme = useTheme();
  const s = theme.space;
  const templates = useDocsRowTemplates(ctx.token ? null : ctx.db.id);
  const create = useDocsCreateRowTemplate(ctx.db.id);
  const [open, setOpen] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [repeating, setRepeating] = useState<string | null>(null);
  const items = templates.data?.items ?? [];
  const defaultId = ctx.config.template_id;
  const go = (path: string) => {
    setOpen(false);
    router.push(path as never);
  };
  return (
    <View style={{ flexDirection: "row", gap: s[2] }}>
      <Button variant="secondary" size="md" leftIcon={Plus} loading={creating} style={{ flex: 1 }} onPress={() => ctx.createRow()}>
        New
      </Button>
      {items.length || ctx.editable ? <IconButton label="Choose a template" icon={LayoutTemplate} variant="secondary" onPress={() => setOpen(true)} /> : null}
      <Sheet
        open={open}
        onClose={() => setOpen(false)}
        title="New row"
        snapPoints={["60%", "90%"]}
        {...(ctx.editable
          ? {
              footer: (
                <Button
                  variant="secondary"
                  leftIcon={Plus}
                  loading={create.isPending}
                  onPress={() =>
                    create.mutate(
                      { title: "" },
                      {
                        onSuccess: (p) => go(`/docs/${p.id}`),
                        onError: (e) => toast.error(errorMessage(e, "Couldn't add a template.")),
                      },
                    )
                  }
                >
                  New template
                </Button>
              ),
            }
          : {})}
      >
        <ScrollView>
          <ListRow
            title="Empty row"
            chevron={false}
            onPress={() => {
              setOpen(false);
              ctx.createRow(undefined, { blank: true });
            }}
          />
          {templates.isPending ? <Skeleton height={theme.tap.base} /> : null}
          {templates.isError ? (
            <Text role="label" tone="danger">
              Couldn't load the templates.
            </Text>
          ) : null}
          {items.map((t) => (
            <View key={t.id}>
              <ListRow
                title={`${t.icon ? `${t.icon} ` : ""}${docsTitle(t.title)}`}
                trailing={
                  <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
                    {t.id === defaultId ? <Badge size="sm">Default</Badge> : null}
                    {t.repeat ? <Badge size="sm" tone="info">{repeatLabel(t.repeat)}</Badge> : null}
                    {ctx.editable ? <IconButton label={`Options for ${docsTitle(t.title)}`} icon={expanded === t.id ? ChevronUp : ChevronDown} variant="ghost" onPress={() => setExpanded((x) => (x === t.id ? null : t.id))} /> : null}
                  </View>
                }
                chevron={false}
                onPress={() => {
                  setOpen(false);
                  ctx.createRow(undefined, { template_id: t.id });
                }}
              />
              {expanded === t.id ? (
                <View style={{ gap: s[2], paddingHorizontal: s[4], paddingBottom: s[3] }}>
                  <View style={{ flexDirection: "row", gap: s[2], flexWrap: "wrap" }}>
                    <Chip size="sm" onPress={() => go(`/docs/${t.id}`)}>
                      Edit template
                    </Chip>
                    <Chip size="sm" selected={t.id === defaultId} onPress={() => {
                        const next = { ...ctx.config };
                        if (t.id === defaultId) delete next.template_id;
                        else next.template_id = t.id;
                        ctx.saveConfig(next);
                      }}>
                      {t.id === defaultId ? "Default here" : "Use by default here"}
                    </Chip>
                    <Chip size="sm" selected={repeating === t.id} onPress={() => setRepeating((x) => (x === t.id ? null : t.id))}>
                      Repeat
                    </Chip>
                  </View>
                  {repeating === t.id ? <RepeatEditor dbId={ctx.db.id} t={t} onDone={() => setRepeating(null)} /> : null}
                </View>
              ) : null}
            </View>
          ))}
          {!templates.isPending && !items.length ? (
            <Text role="label" tone="muted" style={{ padding: s[4] }}>
              No templates yet.
            </Text>
          ) : null}
        </ScrollView>
      </Sheet>
    </View>
  );
}
