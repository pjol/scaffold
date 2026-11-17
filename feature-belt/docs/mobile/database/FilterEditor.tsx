import {
  DOCS_FILTER_OP_LABEL,
  DOCS_RELATIVE_DATES,
  DOCS_WITHIN_RANGES,
  docsDefaultFilter,
  docsFilterOpsFor,
  docsIsFilterGroup,
  docsOpIsUnary,
  docsValueType,
  type DocsFilter,
  type DocsFilterCondition,
  type DocsFilterGroup,
  type DocsProperty,
  type DocsRefs,
} from "@scaffoldapp/shared";
import { Button, Chip, IconButton, Input, ListRow, SegmentedControl, Text, useTheme } from "@scaffoldapp/ui-native";
import { Plus, X } from "lucide-react-native";
import { useState } from "react";
import { ScrollView, View } from "react-native";
import { shortDayLabel, YMD_RE } from "./dates";
import { PersonPicker, RelationPicker } from "./editors";
import { MonthGrid } from "./MonthGrid";

const MAX_DEPTH = 3;

function propsWithOps(schema: DocsProperty[]): DocsProperty[] {
  return schema.filter((p) => docsFilterOpsFor(p).length > 0);
}

function Value({ prop, cond, onChange, orgId, refs }: { prop: DocsProperty; cond: DocsFilterCondition; onChange: (v: unknown) => void; orgId: string; refs?: DocsRefs | undefined }) {
  const theme = useTheme();
  const s = theme.space;
  const [picking, setPicking] = useState(false);
  if (docsOpIsUnary(cond.op)) return null;
  const vt = docsValueType(prop);
  const base = vt.startsWith("list:") ? vt.slice(5) : vt;
  const v = cond.value;
  if (prop.type === "checkbox" || vt === "boolean") {
    return (
      <SegmentedControl
        accessibilityLabel={`${prop.name} value`}
        options={[
          { value: "true", label: "Checked" },
          { value: "false", label: "Unchecked" },
        ]}
        value={v === false ? "false" : "true"}
        onValueChange={(x) => onChange(x === "true")}
      />
    );
  }
  if (prop.type === "select" || prop.type === "status" || prop.type === "multi_select") {
    return (
      <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
        {(prop.options ?? []).map((o) => (
          <Chip key={o.id} size="sm" selected={v === o.id} onPress={() => onChange(o.id)}>
            {o.name}
          </Chip>
        ))}
      </View>
    );
  }
  if (prop.type === "person" || prop.type === "created_by" || prop.type === "last_edited_by" || base === "person") {
    const id = typeof v === "string" ? v : "";
    const name = id === "me" ? "Me" : id ? (refs?.users[id]?.name ?? "Someone") : "";
    return (
      <View style={{ gap: s[2] }}>
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
          <Chip size="sm" selected={id === "me"} onPress={() => onChange("me")}>
            Me
          </Chip>
          {id && id !== "me" ? (
            <Chip size="sm" selected>
              {name}
            </Chip>
          ) : null}
          <Chip size="sm" selected={picking} onPress={() => setPicking((x) => !x)}>
            Someone else
          </Chip>
        </View>
        {picking ? (
          <View style={{ height: 280 }}>
            <PersonPicker
              orgId={orgId}
              chosen={id && id !== "me" ? [id] : []}
              single
              refs={refs}
              onChange={(ids) => {
                onChange(ids[0] ?? "me");
                setPicking(false);
              }}
            />
          </View>
        ) : null}
      </View>
    );
  }
  if (prop.type === "relation") {
    const id = typeof v === "string" ? v : "";
    return (
      <View style={{ height: 280 }}>
        <RelationPicker prop={{ ...prop, relation: { ...(prop.relation ?? { database_id: "" }), limit: 1 } }} chosen={id ? [id] : []} refs={refs} onChange={(ids) => onChange(ids[0] ?? "")} />
      </View>
    );
  }
  if (base === "date") {
    if (cond.op === "is_within") {
      return (
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
          {DOCS_WITHIN_RANGES.map((r) => (
            <Chip key={r.value} size="sm" selected={v === r.value} onPress={() => onChange(r.value)}>
              {r.label}
            </Chip>
          ))}
        </View>
      );
    }
    const exact = typeof v === "string" && YMD_RE.test(v) ? v : "";
    return (
      <View style={{ gap: s[2] }}>
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
          {DOCS_RELATIVE_DATES.map((r) => (
            <Chip key={r.value} size="sm" selected={v === r.value} onPress={() => onChange(r.value)}>
              {r.label}
            </Chip>
          ))}
          <Chip size="sm" selected={!!exact || picking} onPress={() => setPicking((x) => !x)}>
            {exact ? shortDayLabel(exact) : "Exact date"}
          </Chip>
        </View>
        {picking ? (
          <MonthGrid
            initial={exact || undefined}
            start={exact || undefined}
            onPressDay={(d) => {
              onChange(d);
              setPicking(false);
            }}
          />
        ) : null}
      </View>
    );
  }
  const numeric = base === "number";
  return (
    <Input
      value={v === undefined || v === null ? "" : String(v)}
      onChangeText={(t) => {
        if (!numeric) return onChange(t);
        const n = Number(t.replace(/[,\s]/g, ""));
        onChange(t.trim() === "" ? "" : Number.isFinite(n) && !/[.\-]$/.test(t) ? n : t);
      }}
      placeholder={numeric ? "Number" : "Text"}
      keyboardType={numeric ? "numbers-and-punctuation" : "default"}
      autoCapitalize="none"
      accessibilityLabel={`${prop.name} value`}
    />
  );
}

function Condition({ schema, cond, onChange, onRemove, orgId, refs }: { schema: DocsProperty[]; cond: DocsFilterCondition; onChange: (c: DocsFilterCondition) => void; onRemove: () => void; orgId: string; refs?: DocsRefs | undefined }) {
  const theme = useTheme();
  const s = theme.space;
  const [choosing, setChoosing] = useState(false);
  const prop = schema.find((p) => p.id === cond.property);
  const ops = prop ? docsFilterOpsFor(prop) : [];
  const setOp = (op: string) => {
    if (!prop) return;
    const base = docsValueType(prop).replace(/^list:/, "");
    let value = cond.value;
    if (docsOpIsUnary(op)) value = undefined;
    else if (base === "date" && op === "is_within" && cond.op !== "is_within") value = "this_week";
    else if (base === "date" && op !== "is_within" && cond.op === "is_within") value = "today";
    onChange({ property: cond.property, op, ...(value !== undefined ? { value } : {}) });
  };
  return (
    <View style={{ gap: s[2] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <Chip size="sm" selected={choosing} onPress={() => setChoosing((x) => !x)} style={{ flexShrink: 1 }}>
          {prop?.name ?? "Choose a property"}
        </Chip>
        <View style={{ flex: 1 }} />
        <IconButton label="Remove filter" icon={X} variant="ghost" onPress={onRemove} />
      </View>
      {choosing ? (
        <View style={{ maxHeight: 240 }}>
          <ScrollView nestedScrollEnabled>
            {propsWithOps(schema).map((p) => (
              <ListRow
                key={p.id}
                title={p.name}
                chevron={false}
                onPress={() => {
                  onChange(docsDefaultFilter(p));
                  setChoosing(false);
                }}
              />
            ))}
          </ScrollView>
        </View>
      ) : null}
      {prop ? (
        <>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2] }}>
            {ops.map((op) => (
              <Chip key={op} size="sm" selected={cond.op === op} onPress={() => setOp(op)}>
                {DOCS_FILTER_OP_LABEL[op] ?? op}
              </Chip>
            ))}
          </ScrollView>
          <Value prop={prop} cond={cond} onChange={(value) => onChange({ ...cond, value })} orgId={orgId} refs={refs} />
        </>
      ) : null}
    </View>
  );
}

/** A list of conditions and groups joined by one conjunction; groups nest up to three deep. */
export function FilterList({
  schema,
  filters,
  conj,
  onChange,
  depth = 1,
  orgId,
  refs,
}: {
  schema: DocsProperty[];
  filters: DocsFilter[];
  conj: "and" | "or";
  onChange: (filters: DocsFilter[], conj: "and" | "or") => void;
  depth?: number;
  orgId: string;
  refs?: DocsRefs | undefined;
}) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const first = propsWithOps(schema)[0];
  const set = (i: number, f: DocsFilter | null) => onChange(f ? filters.map((x, j) => (j === i ? f : x)) : filters.filter((_, j) => j !== i), conj);
  return (
    <View style={{ gap: s[3] }}>
      {filters.map((f, i) => (
        <View key={i} style={{ gap: s[2] }}>
          {i === 0 ? (
            <Text role="caption" tone="muted" weight={600}>
              Where
            </Text>
          ) : (
            <View style={{ alignSelf: "flex-start" }}>
              <SegmentedControl
                accessibilityLabel="Join filters with"
                options={[
                  { value: "and", label: "And" },
                  { value: "or", label: "Or" },
                ]}
                value={conj}
                onValueChange={(v) => onChange(filters, v as "and" | "or")}
              />
            </View>
          )}
          {docsIsFilterGroup(f) ? (
            <View style={{ gap: s[2], padding: s[3], borderRadius: theme.radius.md, backgroundColor: depth % 2 ? c.sunken : c.surface, borderWidth: 1, borderColor: c.border }}>
              <View style={{ flexDirection: "row", alignItems: "center" }}>
                <Text role="label" weight={600} style={{ flex: 1 }}>
                  Group
                </Text>
                <IconButton label="Remove group" icon={X} variant="ghost" onPress={() => set(i, null)} />
              </View>
              <FilterList schema={schema} filters={f.filters} conj={f.conj} depth={depth + 1} orgId={orgId} refs={refs} onChange={(fs, cj) => set(i, fs.length ? ({ conj: cj, filters: fs } satisfies DocsFilterGroup) : null)} />
            </View>
          ) : (
            <Condition schema={schema} cond={f} onChange={(x) => set(i, x)} onRemove={() => set(i, null)} orgId={orgId} refs={refs} />
          )}
        </View>
      ))}
      <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
        {first ? (
          <Button variant="ghost" size="md" leftIcon={Plus} onPress={() => onChange([...filters, docsDefaultFilter(first)], conj)}>
            Add a filter
          </Button>
        ) : null}
        {first && depth < MAX_DEPTH ? (
          <Button variant="ghost" size="md" leftIcon={Plus} onPress={() => onChange([...filters, { conj: "and", filters: [docsDefaultFilter(first)] }], conj)}>
            Add a group
          </Button>
        ) : null}
      </View>
    </View>
  );
}
