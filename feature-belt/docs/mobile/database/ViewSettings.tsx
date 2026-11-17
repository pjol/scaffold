import {
  DOCS_ROLLUP_LABEL,
  DOCS_VIEW_LABEL,
  DOCS_VIEW_TYPES,
  docsAggregations,
  errorMessage,
  useDocsAutomations,
  useDocsPutSchema,
  type DocsAutomation,
  type DocsChartConfig,
  type DocsColumn,
  type DocsGroupConfig,
  type DocsProperty,
  type DocsRollupFunction,
  type DocsSort,
  type DocsViewConfig,
  type DocsViewType,
} from "@scaffoldapp/shared";
import { Badge, Button, Chip, Field, IconButton, Input, ListRow, SegmentedControl, Sheet, Skeleton, Switch, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { ArrowDown, ArrowUp, Check, Plus, X } from "lucide-react-native";
import { useEffect, useState, type ReactNode } from "react";
import { ScrollView, View } from "react-native";
import type { ViewCtx } from "./context";
import { FilterList } from "./FilterEditor";
import { calcValueType, freeName, groupable, isDateish, isNumeric, newPropId, orderedColumns, sortable, VIEW_ICON } from "./model";

export type SettingsPane = "filter" | "sort" | "group" | "properties" | "calculations" | "layout" | "subitems" | "rename" | "newview" | "automations";

const PANE_TITLE: Record<SettingsPane, string> = {
  filter: "Filter",
  sort: "Sort",
  group: "Group",
  properties: "Properties",
  calculations: "Calculations",
  layout: "Layout",
  subitems: "Sub-items",
  rename: "Rename view",
  newview: "New view",
  automations: "Automations",
};

const LOAD_LIMITS = [10, 25, 50, 100, 200] as const;

function Section({ title, children, info }: { title: string; children: ReactNode; info?: string }) {
  const s = useTheme().space;
  return (
    <View style={{ gap: s[2] }}>
      <Field label={title} {...(info ? { info } : {})}>
        <View style={{ gap: s[2] }}>{children}</View>
      </Field>
    </View>
  );
}

function Choices<T extends string>({ options, value, onChange }: { options: { value: T; label: string }[]; value: T | undefined; onChange: (v: T) => void }) {
  const s = useTheme().space;
  return (
    <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
      {options.map((o) => (
        <Chip key={o.value} size="sm" selected={value === o.value} onPress={() => onChange(o.value)}>
          {o.label}
        </Chip>
      ))}
    </View>
  );
}

function propChoices(props: DocsProperty[]): { value: string; label: string }[] {
  return props.map((p) => ({ value: p.id, label: p.name }));
}

// ---- panes -----------------------------------------------------------------------------

function SortPane({ ctx, draft, set }: { ctx: ViewCtx; draft: DocsViewConfig; set: (c: DocsViewConfig) => void }) {
  const theme = useTheme();
  const s = theme.space;
  const [adding, setAdding] = useState(false);
  const label = (id: string) => ctx.schema.find((p) => p.id === id)?.name ?? (id === "created_time" ? "Created time" : id === "last_edited_time" ? "Last edited time" : id);
  const options = [...ctx.schema.filter(sortable).map((p) => ({ id: p.id, name: p.name })), { id: "created_time", name: "Created time" }, { id: "last_edited_time", name: "Last edited time" }].filter(
    (o, i, all) => !draft.sorts.some((x) => x.property === o.id) && all.findIndex((y) => y.id === o.id) === i,
  );
  const setSorts = (sorts: DocsSort[]) => set({ ...draft, sorts });
  const move = (i: number, d: -1 | 1) => {
    const n = [...draft.sorts];
    const j = i + d;
    if (j < 0 || j >= n.length) return;
    [n[i], n[j]] = [n[j]!, n[i]!];
    setSorts(n);
  };
  return (
    <View style={{ gap: s[3] }}>
      {draft.sorts.length === 0 ? (
        <Text role="label" tone="muted">
          Rows keep their manual order.
        </Text>
      ) : null}
      {draft.sorts.map((srt, i) => (
        <View key={srt.property} style={{ gap: s[2], padding: s[3], borderRadius: theme.radius.md, backgroundColor: theme.colors.sunken }}>
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
            <Text role="label" weight={600} style={{ flex: 1 }} numberOfLines={1}>
              {label(srt.property)}
            </Text>
            <IconButton label="Sort earlier" icon={ArrowUp} variant="ghost" disabled={i === 0} onPress={() => move(i, -1)} />
            <IconButton label="Sort later" icon={ArrowDown} variant="ghost" disabled={i === draft.sorts.length - 1} onPress={() => move(i, 1)} />
            <IconButton label={`Remove sort by ${label(srt.property)}`} icon={X} variant="ghost" onPress={() => setSorts(draft.sorts.filter((_, j) => j !== i))} />
          </View>
          <SegmentedControl
            accessibilityLabel={`Direction for ${label(srt.property)}`}
            options={[
              { value: "asc", label: "Ascending" },
              { value: "desc", label: "Descending" },
            ]}
            value={srt.direction}
            onValueChange={(v) => setSorts(draft.sorts.map((x, j) => (j === i ? { ...x, direction: v as "asc" | "desc" } : x)))}
          />
        </View>
      ))}
      {adding ? (
        <View>
          {options.map((o) => (
            <ListRow
              key={o.id}
              title={o.name}
              chevron={false}
              onPress={() => {
                setSorts([...draft.sorts, { property: o.id, direction: "asc" }]);
                setAdding(false);
              }}
            />
          ))}
        </View>
      ) : draft.sorts.length < 5 ? (
        <Button variant="ghost" leftIcon={Plus} onPress={() => setAdding(true)}>
          Add a sort
        </Button>
      ) : null}
    </View>
  );
}

function GroupOptions({ prop, g, onChange }: { prop: DocsProperty; g: DocsGroupConfig; onChange: (g: DocsGroupConfig) => void }) {
  const s = useTheme().space;
  return (
    <View style={{ gap: s[3] }}>
      {isDateish(prop) ? (
        <Section title="Dates by">
          <Choices
            options={[
              { value: "relative", label: "Relative" },
              { value: "day", label: "Day" },
              { value: "week", label: "Week" },
              { value: "month", label: "Month" },
              { value: "year", label: "Year" },
            ]}
            value={(g.by as "relative" | "day" | "week" | "month" | "year" | undefined) ?? "relative"}
            onChange={(by) => onChange({ ...g, by })}
          />
        </Section>
      ) : null}
      {isNumeric(prop) ? (
        <Field label="Bucket size">
          <Input
            value={g.step ? String(g.step) : ""}
            onChangeText={(t) => {
              const n = Number(t);
              const next = { ...g };
              if (t.trim() && Number.isFinite(n) && n > 0) next.step = n;
              else delete next.step;
              onChange(next);
            }}
            keyboardType="decimal-pad"
            placeholder="10"
          />
        </Field>
      ) : null}
      <Section title="Order">
        <Choices
          options={[
            { value: "manual", label: prop.options ? "Option order" : "Manual" },
            { value: "asc", label: "Ascending" },
            { value: "desc", label: "Descending" },
          ]}
          value={g.sort ?? "manual"}
          onChange={(sort) => onChange({ ...g, sort })}
        />
      </Section>
      <Switch label="Hide empty groups" value={!!g.hide_empty} onValueChange={(v) => onChange({ ...g, hide_empty: v })} />
    </View>
  );
}

function GroupPane({ ctx, draft, set }: { ctx: ViewCtx; draft: DocsViewConfig; set: (c: DocsViewConfig) => void }) {
  const s = useTheme().space;
  const isBoard = ctx.view.type === "board";
  const options = ctx.schema.filter((p) => groupable(p) && !(isBoard && p.type === "title"));
  const prop = ctx.schema.find((p) => p.id === draft.group_by);
  const sub = ctx.schema.find((p) => p.id === draft.sub_group_by);
  const groups = ctx.meta.groups ?? [];
  const setGroupBy = (id: string | null) => {
    const next = { ...draft };
    if (id) {
      next.group_by = id;
      next.group = {};
    } else {
      delete next.group_by;
      delete next.group;
      delete next.sub_group_by;
      delete next.sub_group;
    }
    if (next.sub_group_by === id) {
      delete next.sub_group_by;
      delete next.sub_group;
    }
    set(next);
  };
  return (
    <View style={{ gap: s[4] }}>
      <Section title="Group by">
        <Choices options={[...(isBoard ? [] : [{ value: "", label: "None" }]), ...propChoices(options)]} value={draft.group_by ?? ""} onChange={(v) => setGroupBy(v || null)} />
      </Section>
      {prop ? <GroupOptions prop={prop} g={draft.group ?? {}} onChange={(group) => set({ ...draft, group })} /> : null}
      {prop && draft.group_by === ctx.config.group_by && groups.length ? (
        <Section title="Groups shown">
          {groups.map((g) => {
            const hidden = (draft.group?.hidden ?? []).includes(g.key);
            return (
              <Switch
                key={g.key || "none"}
                label={`${g.label} (${g.count})`}
                value={!hidden}
                onValueChange={(on) => {
                  const cur = draft.group?.hidden ?? [];
                  set({ ...draft, group: { ...(draft.group ?? {}), hidden: on ? cur.filter((k) => k !== g.key) : [...cur, g.key] } });
                }}
              />
            );
          })}
        </Section>
      ) : null}
      {isBoard && prop ? (
        <>
          <Section title="Sub-group by" info="Rows of the board in lanes.">
            <Choices
              options={[{ value: "", label: "None" }, ...propChoices(options.filter((p) => p.id !== draft.group_by && p.type !== "title"))]}
              value={draft.sub_group_by ?? ""}
              onChange={(v) => {
                const next = { ...draft };
                if (v) {
                  next.sub_group_by = v;
                  next.sub_group = {};
                } else {
                  delete next.sub_group_by;
                  delete next.sub_group;
                }
                set(next);
              }}
            />
          </Section>
          {sub ? <GroupOptions prop={sub} g={draft.sub_group ?? {}} onChange={(sub_group) => set({ ...draft, sub_group })} /> : null}
        </>
      ) : null}
    </View>
  );
}

function PropertiesPane({ ctx, draft, set }: { ctx: ViewCtx; draft: DocsViewConfig; set: (c: DocsViewConfig) => void }) {
  const theme = useTheme();
  const s = theme.space;
  if (ctx.view.type === "table") {
    const cols = orderedColumns(ctx.schema, draft);
    const setCols = (columns: DocsColumn[]) => set({ ...draft, columns });
    const move = (i: number, d: -1 | 1) => {
      const n = [...cols];
      const j = i + d;
      if (j < 1 || j >= n.length) return;
      [n[i], n[j]] = [n[j]!, n[i]!];
      setCols(n);
    };
    return (
      <View style={{ gap: s[1] }}>
        {cols.map((col, i) => {
          const p = ctx.schema.find((x) => x.id === col.property);
          if (!p) return null;
          return (
            <View key={col.property} style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
              <View style={{ flex: 1 }}>
                <Switch label={p.name} value={!col.hidden || p.type === "title"} disabled={p.type === "title"} onValueChange={(on) => setCols(cols.map((x) => (x.property === col.property ? { ...x, hidden: !on } : x)))} />
              </View>
              {p.type === "title" ? null : (
                <>
                  <IconButton label={`Move ${p.name} left`} icon={ArrowUp} variant="ghost" disabled={i <= 1} onPress={() => move(i, -1)} />
                  <IconButton label={`Move ${p.name} right`} icon={ArrowDown} variant="ghost" disabled={i === cols.length - 1} onPress={() => move(i, 1)} />
                </>
              )}
            </View>
          );
        })}
      </View>
    );
  }
  const shown = draft.card_properties.filter((id) => ctx.schema.some((p) => p.id === id && p.type !== "title"));
  const rest = ctx.schema.filter((p) => p.type !== "title" && !shown.includes(p.id));
  const move = (i: number, d: -1 | 1) => {
    const n = [...shown];
    const j = i + d;
    if (j < 0 || j >= n.length) return;
    [n[i], n[j]] = [n[j]!, n[i]!];
    set({ ...draft, card_properties: n });
  };
  return (
    <View style={{ gap: s[1] }}>
      {shown.map((id, i) => {
        const p = ctx.schema.find((x) => x.id === id)!;
        return (
          <View key={id} style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
            <View style={{ flex: 1 }}>
              <Switch label={p.name} value onValueChange={() => set({ ...draft, card_properties: shown.filter((x) => x !== id) })} />
            </View>
            <IconButton label={`Move ${p.name} up`} icon={ArrowUp} variant="ghost" disabled={i === 0} onPress={() => move(i, -1)} />
            <IconButton label={`Move ${p.name} down`} icon={ArrowDown} variant="ghost" disabled={i === shown.length - 1} onPress={() => move(i, 1)} />
          </View>
        );
      })}
      {rest.map((p) => (
        <Switch key={p.id} label={p.name} value={false} onValueChange={() => set({ ...draft, card_properties: [...shown, p.id] })} />
      ))}
    </View>
  );
}

function CalculationsPane({ ctx, draft, set }: { ctx: ViewCtx; draft: DocsViewConfig; set: (c: DocsViewConfig) => void }) {
  const s = useTheme().space;
  const calcs = draft.calculations ?? {};
  const props = ctx.schema.filter((p) => calcValueType(p) !== null);
  return (
    <View style={{ gap: s[4] }}>
      {props.map((p) => {
        const fns = docsAggregations(calcValueType(p)!, true);
        return (
          <Section key={p.id} title={p.name}>
            <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2] }}>
              <Chip
                size="sm"
                selected={!calcs[p.id]}
                onPress={() => {
                  const next = { ...calcs };
                  delete next[p.id];
                  set({ ...draft, calculations: next });
                }}
              >
                None
              </Chip>
              {fns.map((fn) => (
                <Chip key={fn} size="sm" selected={calcs[p.id] === fn} onPress={() => set({ ...draft, calculations: { ...calcs, [p.id]: fn as DocsRollupFunction } })}>
                  {DOCS_ROLLUP_LABEL[fn]}
                </Chip>
              ))}
            </ScrollView>
          </Section>
        );
      })}
    </View>
  );
}

function ChartLayout({ ctx, draft, set }: { ctx: ViewCtx; draft: DocsViewConfig; set: (c: DocsViewConfig) => void }) {
  const s = useTheme().space;
  const ch: DocsChartConfig = draft.chart ?? { type: "bar" };
  const put = (patch: Partial<DocsChartConfig>) => set({ ...draft, chart: { ...ch, ...patch } });
  const xProp = ctx.schema.find((p) => p.id === ch.x);
  const groupables = ctx.schema.filter(groupable);
  return (
    <View style={{ gap: s[4] }}>
      <Section title="Chart">
        <Choices
          options={[
            { value: "bar", label: "Columns" },
            { value: "horizontal_bar", label: "Bars" },
            { value: "line", label: "Line" },
            { value: "donut", label: "Donut" },
          ]}
          value={ch.type}
          onChange={(type) => {
            const next: DocsChartConfig = { ...ch, type };
            if (type === "donut") delete next.series;
            set({ ...draft, chart: next });
          }}
        />
      </Section>
      <Section title="Group by">
        <Choices options={propChoices(groupables)} value={ch.x} onChange={(x) => put({ x })} />
      </Section>
      {xProp && isDateish(xProp) ? (
        <Section title="Dates by">
          <Choices
            options={[
              { value: "day", label: "Day" },
              { value: "week", label: "Week" },
              { value: "month", label: "Month" },
              { value: "year", label: "Year" },
            ]}
            value={ch.x_by ?? "month"}
            onChange={(x_by) => put({ x_by })}
          />
        </Section>
      ) : null}
      <Section title="Measure">
        <Choices
          options={[
            { value: "count", label: "Count" },
            { value: "sum", label: "Sum" },
            { value: "average", label: "Average" },
            { value: "median", label: "Median" },
            { value: "min", label: "Min" },
            { value: "max", label: "Max" },
          ]}
          value={ch.y ?? "count"}
          onChange={(y) => put({ y })}
        />
        {ch.y && ch.y !== "count" ? <Choices options={propChoices(ctx.schema.filter(isNumeric))} value={ch.y_property} onChange={(y_property) => put({ y_property })} /> : null}
      </Section>
      {ch.type !== "donut" ? (
        <Section title="Split by">
          <Choices
            options={[{ value: "", label: "None" }, ...propChoices(groupables.filter((p) => p.id !== ch.x))]}
            value={ch.series ?? ""}
            onChange={(v) => {
              const next = { ...ch };
              if (v) next.series = v;
              else delete next.series;
              set({ ...draft, chart: next });
            }}
          />
        </Section>
      ) : null}
      <Section title="Sort">
        <Choices
          options={[
            { value: "manual", label: "Group order" },
            { value: "x_asc", label: "Group A→Z" },
            { value: "x_desc", label: "Group Z→A" },
            { value: "y_asc", label: "Value ↑" },
            { value: "y_desc", label: "Value ↓" },
          ]}
          value={ch.sort ?? "manual"}
          onChange={(sort) => put({ sort })}
        />
      </Section>
      <Switch label="Hide empty groups" value={!!ch.hide_empty} onValueChange={(hide_empty) => put({ hide_empty })} />
      <Switch label="Running total" value={!!ch.cumulative} onValueChange={(cumulative) => put({ cumulative })} />
      <Switch label="Show values" value={!!ch.show_values} onValueChange={(show_values) => put({ show_values })} />
    </View>
  );
}

function LayoutPane({ ctx, draft, set }: { ctx: ViewCtx; draft: DocsViewConfig; set: (c: DocsViewConfig) => void }) {
  const s = useTheme().space;
  const type = ctx.view.type;
  const dateProps = ctx.schema.filter(isDateish);
  const selfRelations = ctx.schema.filter((p) => p.type === "relation" && p.relation?.database_id === ctx.db.id);
  const loadLimit =
    type === "table" || type === "list" || type === "gallery" || type === "feed" || type === "board" ? (
      <Section title="Rows per page">
        <Choices options={LOAD_LIMITS.map((n) => ({ value: String(n), label: String(n) }))} value={String(draft.load_limit ?? 50)} onChange={(v) => set({ ...draft, load_limit: Number(v) as (typeof LOAD_LIMITS)[number] })} />
      </Section>
    ) : null;
  if (type === "chart") return <ChartLayout ctx={ctx} draft={draft} set={set} />;
  return (
    <View style={{ gap: s[4] }}>
      {type === "table" ? <Switch label="Wrap cells" value={!!draft.wrap} onValueChange={(wrap) => set({ ...draft, wrap })} /> : null}
      {type === "calendar" || type === "timeline" ? (
        <Section title={type === "timeline" ? "Starts" : "Show by"}>
          {dateProps.length ? (
            <Choices options={propChoices(dateProps)} value={draft.date_property} onChange={(date_property) => set({ ...draft, date_property })} />
          ) : (
            <Text role="label" tone="muted">
              Add a date property on the web first.
            </Text>
          )}
        </Section>
      ) : null}
      {type === "timeline" ? (
        <>
          <Section title="Ends">
            <Choices
              options={[{ value: "", label: "Its own end date" }, ...propChoices(dateProps.filter((p) => p.id !== draft.date_property))]}
              value={draft.end_property ?? ""}
              onChange={(v) => {
                const next = { ...draft };
                if (v) next.end_property = v;
                else delete next.end_property;
                set(next);
              }}
            />
          </Section>
          <Section title="Opens at">
            <Choices
              options={[
                { value: "day", label: "Day" },
                { value: "week", label: "Week" },
                { value: "month", label: "Month" },
                { value: "quarter", label: "Quarter" },
                { value: "year", label: "Year" },
              ]}
              value={draft.timeline?.zoom === "hours" ? "day" : (draft.timeline?.zoom ?? "month")}
              onChange={(zoom) => set({ ...draft, timeline: { ...(draft.timeline ?? {}), zoom } })}
            />
          </Section>
          {selfRelations.length ? (
            <Section title="Dependencies" info="A relation to rows of this database that must finish first.">
              <Choices
                options={[{ value: "", label: "None" }, ...propChoices(selfRelations)]}
                value={draft.timeline?.dependency ?? ""}
                onChange={(v) => {
                  const t = { ...(draft.timeline ?? {}) };
                  if (v) t.dependency = v;
                  else delete t.dependency;
                  set({ ...draft, timeline: t });
                }}
              />
            </Section>
          ) : null}
        </>
      ) : null}
      {type === "gallery" ? (
        <>
          <Section title="Card preview">
            <Choices
              options={[
                { value: "none", label: "None" },
                { value: "cover", label: "Page cover" },
                { value: "content", label: "Page content" },
                ...propChoices(ctx.schema.filter((p) => p.type === "files")),
              ]}
              value={draft.card?.preview ?? "cover"}
              onChange={(preview) => set({ ...draft, card: { ...(draft.card ?? {}), preview } })}
            />
          </Section>
          <Section title="Card size">
            <Choices
              options={[
                { value: "small", label: "Small" },
                { value: "medium", label: "Medium" },
                { value: "large", label: "Large" },
              ]}
              value={draft.card?.size ?? "medium"}
              onChange={(size) => set({ ...draft, card: { ...(draft.card ?? {}), size } })}
            />
          </Section>
          <Switch label="Fit image" value={!!draft.card?.fit} onValueChange={(fit) => set({ ...draft, card: { ...(draft.card ?? {}), fit } })} />
        </>
      ) : null}
      {loadLimit}
    </View>
  );
}

function SubItemsPane({ ctx, draft, set }: { ctx: ViewCtx; draft: DocsViewConfig; set: (c: DocsViewConfig) => void }) {
  const s = useTheme().space;
  const putSchema = useDocsPutSchema(ctx.db.id);
  const self = ctx.schema.filter((p) => p.type === "relation" && p.relation?.database_id === ctx.db.id);
  const on = !!draft.sub_items;
  const enable = () => {
    const parent = self.find((p) => p.relation?.limit === 1) ?? self[0];
    if (parent) {
      set({ ...draft, sub_items: { property: parent.id, display: "nested" } });
      return;
    }
    // No self relation yet: add the pair (one parent each), then point the view at it.
    const id = newPropId();
    const prop: DocsProperty = { id, name: freeName(ctx.schema, "Parent item"), type: "relation", relation: { database_id: ctx.db.id, two_way: true, mirror_name: freeName(ctx.schema, "Sub-items"), limit: 1 } };
    putSchema.mutate([...ctx.schema, prop], {
      onSuccess: () => set({ ...draft, sub_items: { property: id, display: "nested" } }),
      onError: (e) => toast.error(errorMessage(e, "Couldn't add the sub-items relation.")),
    });
  };
  return (
    <View style={{ gap: s[4] }}>
      <Switch
        label="Sub-items"
        info="Rows inside other rows, through a relation of this database to itself."
        value={on}
        disabled={putSchema.isPending}
        onValueChange={(v) => {
          if (v) enable();
          else {
            const next = { ...draft };
            delete next.sub_items;
            set(next);
          }
        }}
      />
      {draft.sub_items ? (
        <>
          {self.length > 1 ? (
            <Section title="Parent relation">
              <Choices options={propChoices(self)} value={draft.sub_items.property} onChange={(property) => set({ ...draft, sub_items: { ...draft.sub_items!, property } })} />
            </Section>
          ) : null}
          <Section title="Show sub-items">
            <Choices
              options={[
                { value: "nested", label: "Inside their parent" },
                { value: "flat", label: "As rows" },
              ]}
              value={draft.sub_items.display}
              onChange={(display) => set({ ...draft, sub_items: { ...draft.sub_items!, display } })}
            />
          </Section>
        </>
      ) : null}
    </View>
  );
}

function triggerLabel(a: DocsAutomation): string {
  const t = a.trigger;
  if (t.type === "row_added") return "When a row is added";
  if (t.type === "property_edited") return "When a property changes";
  return t.every === "week" ? `Every week at ${t.time ?? ""}` : `Every day at ${t.time ?? ""}`;
}

function AutomationsPane({ ctx }: { ctx: ViewCtx }) {
  const theme = useTheme();
  const s = theme.space;
  const q = useDocsAutomations(ctx.db.id, ctx.db.my_level === "edit" || ctx.db.my_level === "manage");
  if (q.isPending) return <Skeleton height={theme.tap.base} />;
  if (q.isError) {
    return (
      <Text role="label" tone="danger">
        {q.error.status === 403 ? "Editors of this database see its automations." : "Couldn't load the automations."}
      </Text>
    );
  }
  const items = q.data.items;
  if (!items.length) {
    return (
      <Text role="label" tone="muted">
        No automations. Add them on the web.
      </Text>
    );
  }
  return (
    <View style={{ gap: s[2] }}>
      {items.map((a) => (
        <View key={a.id} style={{ gap: s[1], padding: s[3], borderRadius: theme.radius.md, backgroundColor: theme.colors.sunken }}>
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
            <Text role="label" weight={600} style={{ flex: 1 }} numberOfLines={2}>
              {a.name}
            </Text>
            {a.paused_reason ? (
              <Badge tone="warning" size="sm">
                Paused
              </Badge>
            ) : a.enabled ? (
              <Badge tone="success" size="sm">
                On
              </Badge>
            ) : (
              <Badge size="sm">Off</Badge>
            )}
          </View>
          <Text role="caption" tone="muted">{`${triggerLabel(a)} · ${a.actions.length} ${a.actions.length === 1 ? "action" : "actions"}`}</Text>
          {a.paused_reason ? (
            <Text role="caption" tone="warning">
              {a.paused_reason}
            </Text>
          ) : null}
        </View>
      ))}
    </View>
  );
}

// ---- the sheet -------------------------------------------------------------------------

export interface ViewSettingsProps {
  ctx: ViewCtx;
  pane: SettingsPane | null;
  onClose: () => void;
  onRename: (name: string) => void;
  onCreateView: (type: DocsViewType) => void;
  creatingView?: boolean;
}

/** One sheet for a view's settings, a pane at a time (filter, sort, group, properties, calculations, layout, sub-items…). */
export function ViewSettings({ ctx, pane, onClose, onRename, onCreateView, creatingView }: ViewSettingsProps) {
  const theme = useTheme();
  const s = theme.space;
  const [draft, setDraft] = useState<DocsViewConfig>(ctx.config);
  const [name, setName] = useState(ctx.view.name);
  useEffect(() => {
    if (!pane) return;
    setDraft(ctx.config);
    setName(ctx.view.name);
    // Fresh draft each time a pane opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pane]);
  const apply = () => {
    ctx.saveConfig(draft);
    onClose();
  };
  const configPane = pane === "filter" || pane === "sort" || pane === "group" || pane === "properties" || pane === "calculations" || pane === "layout" || pane === "subitems";
  let body: ReactNode = null;
  switch (pane) {
    case "filter":
      body = (
        <FilterList
          schema={ctx.schema}
          filters={draft.filters}
          conj={draft.filter_conj ?? "and"}
          orgId={ctx.db.org_id}
          refs={ctx.refs}
          onChange={(filters, filter_conj) => setDraft({ ...draft, filters, filter_conj })}
        />
      );
      break;
    case "sort":
      body = <SortPane ctx={ctx} draft={draft} set={setDraft} />;
      break;
    case "group":
      body = <GroupPane ctx={ctx} draft={draft} set={setDraft} />;
      break;
    case "properties":
      body = <PropertiesPane ctx={ctx} draft={draft} set={setDraft} />;
      break;
    case "calculations":
      body = <CalculationsPane ctx={ctx} draft={draft} set={setDraft} />;
      break;
    case "layout":
      body = <LayoutPane ctx={ctx} draft={draft} set={setDraft} />;
      break;
    case "subitems":
      body = <SubItemsPane ctx={ctx} draft={draft} set={setDraft} />;
      break;
    case "automations":
      body = <AutomationsPane ctx={ctx} />;
      break;
    case "rename":
      body = (
        <Field label="Name">
          <Input value={name} onChangeText={setName} autoFocus maxLength={100} returnKeyType="done" onSubmitEditing={() => name.trim() && (onRename(name.trim()), onClose())} />
        </Field>
      );
      break;
    case "newview":
      body = (
        <View>
          {DOCS_VIEW_TYPES.map((t) => {
            const Icon = VIEW_ICON[t];
            return <ListRow key={t} title={DOCS_VIEW_LABEL[t]} leading={<Icon size={18} color={theme.colors.textMuted} />} chevron={false} disabled={creatingView} onPress={() => onCreateView(t)} />;
          })}
        </View>
      );
      break;
  }
  const footer = configPane ? (
    <View style={{ flexDirection: "row", gap: s[2] }}>
      {pane === "filter" && draft.filters.length ? (
        <Button variant="secondary" style={{ flex: 1 }} onPress={() => setDraft({ ...draft, filters: [] })}>
          Clear
        </Button>
      ) : null}
      <Button style={{ flex: 1 }} leftIcon={Check} onPress={apply}>
        {ctx.editable ? "Save" : "Apply"}
      </Button>
    </View>
  ) : pane === "rename" ? (
    <Button disabled={!name.trim()} onPress={() => (onRename(name.trim()), onClose())}>
      Rename
    </Button>
  ) : undefined;
  return (
    <Sheet
      open={!!pane}
      onClose={onClose}
      title={pane ? PANE_TITLE[pane] : ""}
      {...(pane === "filter" && !ctx.editable ? { info: "Only for you, until you leave." } : {})}
      snapPoints={pane === "rename" ? ["40%"] : ["70%", "92%"]}
      {...(footer ? { footer } : {})}
    >
      <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={{ paddingBottom: s[4] }}>
        {body}
      </ScrollView>
    </Sheet>
  );
}
