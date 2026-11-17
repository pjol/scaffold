"use client";

import {
  DOCS_VIEW_LABEL,
  DOCS_VIEW_TYPES,
  docsValueType,
  errorMessage,
  useDocsPutSchema,
  type DocsChartConfig,
  type DocsGroupConfig,
  type DocsPage,
  type DocsProperty,
  type DocsView,
  type DocsViewConfig,
  type DocsViewType,
} from "@scaffoldapp/shared";
import { Button, Field, IconButton, InfoTip, Select, Sheet, Switch, cn, toast } from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowUp, Eye, EyeOff } from "lucide-react";
import { useState, type ReactNode } from "react";
import { newShortId, PROPERTY_ICON, VIEW_ICON } from "../format";
import { columnShown, orderedColumns, reorderColumns, setShown } from "./model";
import type { ViewSettingsSection } from "./types";

/** Properties a view can group by (mirrors the server's groupable). */
export function groupable(p: DocsProperty, type: DocsViewType): boolean {
  switch (p.type) {
    case "title":
      return type !== "board";
    case "select":
    case "status":
    case "multi_select":
    case "person":
    case "created_by":
    case "last_edited_by":
    case "checkbox":
    case "date":
    case "created_time":
    case "last_edited_time":
    case "text":
    case "url":
    case "email":
    case "phone":
    case "number":
    case "relation":
      return true;
    case "formula":
    case "rollup":
      return ["text", "number", "boolean", "date"].includes(docsValueType(p));
  }
  return false;
}

export function isDateish(p: DocsProperty): boolean {
  return p.type === "date" || p.type === "created_time" || p.type === "last_edited_time" || ((p.type === "formula" || p.type === "rollup") && docsValueType(p) === "date");
}

export function isNumeric(p: DocsProperty): boolean {
  return p.type === "number" || p.type === "unique_id" || ((p.type === "formula" || p.type === "rollup") && docsValueType(p) === "number");
}

export function selfRelations(schema: DocsProperty[], dbId: string): DocsProperty[] {
  return schema.filter((p) => p.type === "relation" && p.relation?.database_id === dbId);
}

function Section({ title, children, id }: { title: string; children: ReactNode; id: string }) {
  return (
    <section aria-labelledby={`vs-${id}`} className="flex flex-col gap-3 border-t border-border pt-4 first:border-t-0 first:pt-0">
      <h3 id={`vs-${id}`} className="type-title-sm text-fg">
        {title}
      </h3>
      {children}
    </section>
  );
}

function GroupFields({ label, schema, type, by, cfg, onBy, onCfg, exclude }: { label: string; schema: DocsProperty[]; type: DocsViewType; by: string | undefined; cfg: DocsGroupConfig | undefined; onBy: (id: string | undefined) => void; onCfg: (g: DocsGroupConfig) => void; exclude?: string | undefined }) {
  const p = schema.find((x) => x.id === by);
  const g = cfg ?? {};
  return (
    <div className="flex flex-col gap-3">
      <Field label={label}>
        <Select
          value={by ?? "none"}
          options={[...(type === "board" && label === "Group by" ? [] : [{ value: "none", label: "None" }]), ...schema.filter((x) => groupable(x, type) && x.id !== exclude).map((x) => ({ value: x.id, label: x.name }))]}
          onValueChange={(v) => onBy(v === "none" ? undefined : v)}
        />
      </Field>
      {p && isDateish(p) ? (
        <Field label="Date by">
          <Select
            value={g.by ?? "relative"}
            options={[
              { value: "relative", label: "Relative (today, last 7 days…)" },
              { value: "day", label: "Day" },
              { value: "week", label: "Week" },
              { value: "month", label: "Month" },
              { value: "year", label: "Year" },
            ]}
            onValueChange={(v) => onCfg({ ...g, by: v })}
          />
        </Field>
      ) : null}
      {p && isNumeric(p) ? (
        <Field label="Bucket size">
          <Select value={String(g.step ?? 10)} options={[1, 5, 10, 25, 50, 100, 1000].map((n) => ({ value: String(n), label: String(n) }))} onValueChange={(v) => onCfg({ ...g, step: Number(v) })} />
        </Field>
      ) : null}
      {p ? (
        <>
          <Field label="Order">
            <Select
              value={g.sort ?? "manual"}
              options={[
                { value: "manual", label: p.type === "select" || p.type === "status" || p.type === "multi_select" ? "Option order" : "Default" },
                { value: "asc", label: "Ascending" },
                { value: "desc", label: "Descending" },
              ]}
              onValueChange={(v) => onCfg({ ...g, sort: v as DocsGroupConfig["sort"], order: [] })}
            />
          </Field>
          <Switch checked={!!g.hide_empty} onCheckedChange={(on) => onCfg({ ...g, hide_empty: on })} label="Hide empty groups" />
          {g.hidden?.length ? (
            <Button size="sm" variant="ghost" onClick={() => onCfg({ ...g, hidden: [] })}>
              Show {g.hidden.length === 1 ? "the hidden group" : `${g.hidden.length} hidden groups`}
            </Button>
          ) : null}
        </>
      ) : null}
    </div>
  );
}

function ChartFields({ schema, chart, onChange }: { schema: DocsProperty[]; chart: DocsChartConfig; onChange: (c: DocsChartConfig) => void }) {
  const x = schema.find((p) => p.id === chart.x);
  const numeric = schema.filter(isNumeric);
  return (
    <div className="flex flex-col gap-3">
      <Field label="Chart">
        <Select
          value={chart.type}
          options={[
            { value: "bar", label: "Vertical bar" },
            { value: "horizontal_bar", label: "Horizontal bar" },
            { value: "line", label: "Line" },
            { value: "donut", label: "Donut" },
          ]}
          onValueChange={(v) => onChange({ ...chart, type: v as DocsChartConfig["type"], ...(v === "donut" ? { series: undefined } : {}) } as DocsChartConfig)}
        />
      </Field>
      <Field label={chart.type === "donut" ? "Slices" : "X axis"}>
        <Select value={chart.x ?? ""} placeholder="Choose a property" options={schema.filter((p) => groupable(p, "chart")).map((p) => ({ value: p.id, label: p.name }))} onValueChange={(v) => onChange({ ...chart, x: v })} />
      </Field>
      {x && isDateish(x) ? (
        <Field label="Dates by">
          <Select
            value={chart.x_by ?? "month"}
            options={[
              { value: "day", label: "Day" },
              { value: "week", label: "Week" },
              { value: "month", label: "Month" },
              { value: "year", label: "Year" },
            ]}
            onValueChange={(v) => onChange({ ...chart, x_by: v as DocsChartConfig["x_by"] })}
          />
        </Field>
      ) : null}
      <div className="flex flex-col gap-3 sm:flex-row">
        <Field label={chart.type === "donut" ? "Size" : "Y axis"} className="flex-1">
          <Select
            value={chart.y ?? "count"}
            options={[
              { value: "count", label: "Count of rows" },
              ...(numeric.length
                ? [
                    { value: "sum", label: "Sum" },
                    { value: "average", label: "Average" },
                    { value: "median", label: "Median" },
                    { value: "min", label: "Min" },
                    { value: "max", label: "Max" },
                  ]
                : []),
            ]}
            onValueChange={(v) => onChange({ ...chart, y: v as DocsChartConfig["y"], ...(v !== "count" && !chart.y_property ? { y_property: numeric[0]?.id } : {}) } as DocsChartConfig)}
          />
        </Field>
        {chart.y && chart.y !== "count" ? (
          <Field label="Of" className="flex-1">
            <Select value={chart.y_property ?? ""} options={numeric.map((p) => ({ value: p.id, label: p.name }))} onValueChange={(v) => onChange({ ...chart, y_property: v })} />
          </Field>
        ) : null}
      </div>
      {chart.type !== "donut" ? (
        <Field label="Series" info="Splits each bar or line by another property.">
          <Select
            value={chart.series ?? "none"}
            options={[{ value: "none", label: "None" }, ...schema.filter((p) => groupable(p, "chart") && p.id !== chart.x).map((p) => ({ value: p.id, label: p.name }))]}
            onValueChange={(v) => onChange({ ...chart, series: v === "none" ? undefined : v } as DocsChartConfig)}
          />
        </Field>
      ) : null}
      <Field label="Sort">
        <Select
          value={chart.sort ?? "manual"}
          options={[
            { value: "manual", label: "Default" },
            { value: "x_asc", label: "X ascending" },
            { value: "x_desc", label: "X descending" },
            { value: "y_asc", label: "Value ascending" },
            { value: "y_desc", label: "Value descending" },
          ]}
          onValueChange={(v) => onChange({ ...chart, sort: v as DocsChartConfig["sort"] })}
        />
      </Field>
      <Switch checked={!!chart.hide_empty} onCheckedChange={(on) => onChange({ ...chart, hide_empty: on })} label="Hide empty groups" />
      {chart.type === "line" ? <Switch checked={!!chart.cumulative} onCheckedChange={(on) => onChange({ ...chart, cumulative: on })} label="Cumulative" /> : null}
      <Switch checked={!!chart.show_values} onCheckedChange={(on) => onChange({ ...chart, show_values: on })} label="Show values" />
    </div>
  );
}

export interface ViewSettingsProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  db: DocsPage;
  view: DocsView;
  config: DocsViewConfig;
  section?: ViewSettingsSection | undefined;
  /** Saves (editors) or keeps for this viewer. */
  onConfig: (cfg: DocsViewConfig) => void;
  onType?: ((t: DocsViewType) => void) | undefined;
  /** Whether grouping, calculations and other server-side settings can change (editors). */
  canSave: boolean;
  schemaEditable: boolean;
}

/** One view's settings: layout and type options, properties shown and their order, groups, sub-items. */
export function ViewSettingsSheet({ open, onOpenChange, db, view, config, onConfig, onType, canSave, schemaEditable }: ViewSettingsProps) {
  const schema = db.schema ?? [];
  const type = view.type;
  const putSchema = useDocsPutSchema(db.id);
  const [busy, setBusy] = useState(false);
  const set = (patch: Partial<DocsViewConfig>) => onConfig({ ...config, ...patch });
  const cols = orderedColumns(schema, config);
  const byId = new Map(schema.map((p) => [p.id, p]));
  const dates = schema.filter(isDateish);
  const selfRels = selfRelations(schema, db.id);
  const files = schema.filter((p) => p.type === "files");
  const enableSubItems = () => {
    if (selfRels.length) return set({ sub_items: { property: selfRels[0]!.id, display: "nested" } });
    // A two-way self relation pair: each row's parent, and its sub-items on the other side.
    const id = newShortId();
    setBusy(true);
    putSchema.mutate([...schema, { id, name: "Parent item", type: "relation", relation: { database_id: db.id, two_way: true, mirror_name: "Sub-items", limit: 1 } }], {
      onSuccess: () => set({ sub_items: { property: id, display: "nested" } }),
      onError: (e) => toast.error(errorMessage(e, "Couldn't add sub-items.")),
      onSettled: () => setBusy(false),
    });
  };
  const cardSizes = (
    <Field label="Card size">
      <Select
        value={config.card?.size ?? "medium"}
        options={[
          { value: "small", label: "Small" },
          { value: "medium", label: "Medium" },
          { value: "large", label: "Large" },
        ]}
        onValueChange={(v) => set({ card: { ...(config.card ?? {}), size: v as "small" | "medium" | "large" } })}
      />
    </Field>
  );
  const preview = (
    <>
      <Field label="Card preview">
        <Select
          value={config.card?.preview ?? "none"}
          options={[
            { value: "none", label: "None" },
            { value: "cover", label: "Page cover" },
            { value: "content", label: "Page content" },
            ...files.map((p) => ({ value: p.id, label: p.name })),
          ]}
          onValueChange={(v) => set({ card: { ...(config.card ?? {}), preview: v } })}
        />
      </Field>
      {config.card?.preview && config.card.preview !== "none" && config.card.preview !== "content" ? (
        <Switch checked={!!config.card?.fit} onCheckedChange={(on) => set({ card: { ...(config.card ?? {}), fit: on } })} label="Fit image" />
      ) : null}
    </>
  );
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title={`${view.name} settings`} size="md" info={canSave ? "Changes save for everyone who sees this view." : "Changes stay with you until you reload."}>
      <div className="flex flex-col gap-6">
        <Section title="Layout" id="layout">
          {onType ? (
            <div role="radiogroup" aria-label="View type" className="grid grid-cols-3 gap-2">
              {DOCS_VIEW_TYPES.map((t) => {
                const Icon = VIEW_ICON[t];
                return (
                  <button
                    key={t}
                    type="button"
                    role="radio"
                    aria-checked={type === t}
                    onClick={() => type !== t && onType(t)}
                    className={cn(
                      "flex min-h-16 flex-col items-center justify-center gap-1 rounded-md border px-2 type-caption focus-visible:outline-2 focus-visible:outline-ring",
                      type === t ? "border-primary bg-primary-soft text-fg" : "border-border text-fg-muted hover:bg-sunken",
                    )}
                  >
                    <Icon aria-hidden className="size-5" />
                    {DOCS_VIEW_LABEL[t]}
                  </button>
                );
              })}
            </div>
          ) : null}
          {type === "table" || type === "list" ? (
            <>
              <Switch checked={!!config.wrap} onCheckedChange={(on) => set({ wrap: on })} label="Wrap cell content" />
              <Field label="Row height">
                <Select
                  value={config.density ?? "comfortable"}
                  options={[
                    { value: "comfortable", label: "Comfortable" },
                    { value: "compact", label: "Compact" },
                  ]}
                  onValueChange={(v) => set({ density: v as "compact" | "comfortable" })}
                />
              </Field>
            </>
          ) : null}
          {type === "table" ? (
            <Field label="Freeze columns" info="Keeps the first columns in place while the table scrolls sideways.">
              <Select value={String(config.freeze_columns ?? 0)} options={[0, 1, 2, 3].map((n) => ({ value: String(n), label: n === 0 ? "None" : n === 1 ? "First column" : `First ${n} columns` }))} onValueChange={(v) => set({ freeze_columns: Number(v) })} />
            </Field>
          ) : null}
          {type === "table" || type === "list" || type === "gallery" || type === "feed" ? (
            <Field label="Load" info="How many rows show before Load more.">
              <Select value={String(config.load_limit ?? 50)} options={[10, 25, 50, 100, 200].map((n) => ({ value: String(n), label: `${n} rows` }))} onValueChange={(v) => set({ load_limit: Number(v) as DocsViewConfig["load_limit"] })} />
            </Field>
          ) : null}
          {type === "gallery" ? (
            <>
              {preview}
              {cardSizes}
            </>
          ) : null}
          {type === "board" ? preview : null}
          {type === "calendar" || type === "timeline" ? (
            <Field label={type === "timeline" ? "Start" : "Show by"}>
              <Select value={config.date_property ?? ""} placeholder="Choose a date property" options={dates.map((p) => ({ value: p.id, label: p.name }))} onValueChange={(v) => set({ date_property: v })} />
            </Field>
          ) : null}
          {type === "timeline" ? (
            <>
              <Field label="End" info="Without one, a date's own end date is used.">
                <Select
                  value={config.end_property ?? "none"}
                  options={[{ value: "none", label: "The start date's end" }, ...dates.filter((p) => p.id !== config.date_property).map((p) => ({ value: p.id, label: p.name }))]}
                  onValueChange={(v) => {
                    const next = { ...config };
                    if (v === "none") delete next.end_property;
                    else next.end_property = v;
                    onConfig(next);
                  }}
                />
              </Field>
              <Field label="Dependencies" info="A relation from this database to itself: each row's “blocked by” rows. Arrows link them on the timeline.">
                <Select
                  value={config.timeline?.dependency ?? "none"}
                  options={[{ value: "none", label: "None" }, ...selfRels.map((p) => ({ value: p.id, label: p.name }))]}
                  onValueChange={(v) => set({ timeline: { ...(config.timeline ?? {}), ...(v === "none" ? { dependency: "" } : { dependency: v }) } })}
                />
              </Field>
              <Switch checked={!!config.timeline?.show_table} onCheckedChange={(on) => set({ timeline: { ...(config.timeline ?? {}), show_table: on } })} label="Show table beside it" />
            </>
          ) : null}
          {type === "chart" ? <ChartFields schema={schema} chart={config.chart ?? { type: "bar", y: "count" }} onChange={(chart) => set({ chart })} /> : null}
        </Section>

        {type !== "chart" && type !== "form" ? (
          <Section title="Properties" id="properties">
            <ul className="flex flex-col gap-0.5">
              {cols.map((c, i) => {
                const p = byId.get(c.property);
                if (!p) return null;
                const Icon = PROPERTY_ICON[p.type];
                const shown = columnShown(config, type, p.id);
                const timelineTable = type === "timeline";
                const inTable = timelineTable ? (config.timeline?.table_properties ?? []).includes(p.id) : shown;
                return (
                  <li key={p.id} className="flex min-h-control items-center gap-2">
                    <Icon aria-hidden className="size-4 shrink-0 text-fg-muted" />
                    <span className={cn("min-w-0 flex-1 truncate type-label", inTable ? "text-fg" : "text-fg-muted")}>{p.name}</span>
                    {p.type !== "title" ? (
                      <>
                        <IconButton label={`Move ${p.name} up`} icon={<ArrowUp />} size="sm" variant="ghost" disabled={i <= 1} onClick={() => set({ columns: reorderColumns(schema, config, type, i, i - 1) })} />
                        <IconButton label={`Move ${p.name} down`} icon={<ArrowDown />} size="sm" variant="ghost" disabled={i >= cols.length - 1} onClick={() => set({ columns: reorderColumns(schema, config, type, i, i + 1) })} />
                        <IconButton
                          label={inTable ? `Hide ${p.name}` : `Show ${p.name}`}
                          icon={inTable ? <Eye /> : <EyeOff />}
                          size="sm"
                          variant="ghost"
                          aria-pressed={inTable}
                          onClick={() =>
                            timelineTable
                              ? set({ timeline: { ...(config.timeline ?? {}), table_properties: inTable ? (config.timeline?.table_properties ?? []).filter((x) => x !== p.id) : [...(config.timeline?.table_properties ?? []), p.id] } })
                              : set({ columns: setShown(schema, config, type, p.id, !shown) })
                          }
                        />
                      </>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          </Section>
        ) : null}

        {canSave && type !== "chart" && type !== "form" && type !== "calendar" && type !== "timeline" ? (
          <Section title="Group" id="group">
            <GroupFields
              label="Group by"
              schema={schema}
              type={type}
              by={config.group_by}
              cfg={config.group}
              onBy={(id) => {
                const next = { ...config };
                if (id) next.group_by = id;
                else {
                  delete next.group_by;
                  delete next.group;
                  delete next.sub_group_by;
                  delete next.sub_group;
                }
                if (id !== config.group_by) delete next.group;
                onConfig(next);
              }}
              onCfg={(group) => set({ group })}
            />
            {type === "board" && config.group_by ? (
              <GroupFields
                label="Sub-group by"
                schema={schema}
                type={type}
                by={config.sub_group_by}
                cfg={config.sub_group}
                exclude={config.group_by}
                onBy={(id) => {
                  const next = { ...config };
                  if (id) next.sub_group_by = id;
                  else delete next.sub_group_by;
                  delete next.sub_group;
                  onConfig(next);
                }}
                onCfg={(sub_group) => set({ sub_group })}
              />
            ) : null}
          </Section>
        ) : null}

        {canSave && (type === "table" || type === "list") ? (
          <Section title="Sub-items" id="sub_items">
            {config.sub_items ? (
              <>
                <Field label="Parent property" info="A relation from this database to itself; each row's parent.">
                  <Select value={config.sub_items.property} options={selfRels.map((p) => ({ value: p.id, label: p.name }))} onValueChange={(v) => set({ sub_items: { ...config.sub_items!, property: v } })} />
                </Field>
                <Field label="Show sub-items">
                  <Select
                    value={config.sub_items.display}
                    options={[
                      { value: "nested", label: "Nested under their parent" },
                      { value: "flat", label: "In the list with everything" },
                    ]}
                    onValueChange={(v) => set({ sub_items: { ...config.sub_items!, display: v as "nested" | "flat" } })}
                  />
                </Field>
                <div>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      const next = { ...config };
                      delete next.sub_items;
                      onConfig(next);
                    }}
                  >
                    Turn off sub-items
                  </Button>
                </div>
              </>
            ) : (
              <div className="flex items-center gap-2">
                <Button size="sm" variant="secondary" loading={busy} disabled={!selfRels.length && !schemaEditable} onClick={enableSubItems}>
                  Turn on sub-items
                </Button>
                {!selfRels.length ? <InfoTip content="Adds a “Parent item” relation to this database, with “Sub-items” on the other side." /> : null}
              </div>
            )}
          </Section>
        ) : null}
      </div>
    </Sheet>
  );
}
