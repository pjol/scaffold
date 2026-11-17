import { docsTitle, type DocsGroupInfo, type DocsProperty, type DocsRow, type DocsValue } from "@scaffoldapp/shared";
import { ListRow, Sheet, Text, useTheme } from "@scaffoldapp/ui-native";
import { Check, ChevronDown, ChevronRight, Plus } from "lucide-react-native";
import { useState } from "react";
import { Pressable, ScrollView, View } from "react-native";
import type { ViewCtx } from "../context";
import { cardProps, groupSections } from "../model";
import { OptionChip, OptionDot, ValueView } from "../values";

/** The value that puts a row in a group of p (null: no value), or undefined when cards can't move there. */
export function groupValue(p: DocsProperty, key: string): DocsValue | undefined {
  switch (p.type) {
    case "select":
    case "status":
      return key || null;
    case "checkbox":
      return key === "true";
    case "multi_select":
    case "person":
      return key ? [key] : [];
  }
  return undefined;
}

/** Whether a long press moves cards between groups of p (one value per row). */
export function movable(p: DocsProperty | undefined): boolean {
  return !!p && (p.type === "select" || p.type === "status" || p.type === "checkbox");
}

export function Card({ ctx, row, except, onLongPress }: { ctx: ViewCtx; row: DocsRow; except?: string | undefined; onLongPress?: (() => void) | undefined }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const shown = cardProps(ctx.schema, ctx.config, except);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={docsTitle(row.title)}
      accessibilityHint={onLongPress ? "Opens the row. Long press to move it." : "Opens the row."}
      onPress={() => ctx.openRow(row.id)}
      onLongPress={onLongPress}
      style={({ pressed }) => ({ gap: s[2], padding: s[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, backgroundColor: pressed ? c.sunken : c.surface })}
    >
      <Text role="label" weight={600} numberOfLines={3}>
        {row.icon ? `${row.icon} ` : ""}
        {docsTitle(row.title)}
      </Text>
      {shown.map((p) => {
        const v = row.props[p.id];
        const empty = v === null || v === undefined || (Array.isArray(v) && !v.length);
        if (empty && p.type !== "button" && p.type !== "created_time" && p.type !== "last_edited_time" && p.type !== "created_by" && p.type !== "last_edited_by" && p.type !== "checkbox") return null;
        return (
          <Pressable key={p.id} accessibilityRole="button" accessibilityLabel={`${p.name}`} onPress={() => ctx.editValue(row, p)} hitSlop={s[1]}>
            <ValueView prop={p} value={v} refs={ctx.refs} row={row} compact pressing={ctx.pressing === `${row.id}:${p.id}`} {...(p.type === "button" && !ctx.token ? { onPressButton: () => ctx.pressButton(row, p) } : {})} />
          </Pressable>
        );
      })}
    </Pressable>
  );
}

function Column({ ctx, group, rows, groupProp, onMove, lane }: { ctx: ViewCtx; group: DocsGroupInfo; rows: DocsRow[]; groupProp: DocsProperty; onMove: (r: DocsRow) => void; lane?: { prop: DocsProperty; key: string } | undefined }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [collapsed, setCollapsed] = useState(() => (ctx.config.group?.collapsed ?? []).includes(group.key));
  const canMove = movable(groupProp);
  const add = () => {
    const values: Record<string, DocsValue> = {};
    const v = groupValue(groupProp, group.key);
    if (v !== undefined && v !== null && !(Array.isArray(v) && !v.length)) values[groupProp.id] = v;
    if (lane) {
      const lv = groupValue(lane.prop, lane.key);
      if (lv !== undefined && lv !== null && !(Array.isArray(lv) && !lv.length)) values[lane.prop.id] = lv;
    }
    ctx.createRow(values);
  };
  const Icon = collapsed ? ChevronRight : ChevronDown;
  return (
    <View style={{ width: collapsed ? 148 : 264, gap: s[2], backgroundColor: c.sunken, borderRadius: theme.radius.md, padding: s[2] }}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${group.label}, ${group.count} ${group.count === 1 ? "card" : "cards"}`}
        accessibilityState={{ expanded: !collapsed }}
        onPress={() => setCollapsed((x) => !x)}
        style={{ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: theme.tap.sm }}
      >
        <Icon size={16} color={c.textMuted} />
        {group.key ? <OptionChip name={group.label} color={group.color} dot={groupProp.type === "status"} /> : <Text role="label" tone="muted">{group.label}</Text>}
        <Text role="caption" tone="muted" tabular>
          {group.count}
        </Text>
      </Pressable>
      {collapsed
        ? null
        : rows.map((r) => <Card key={r.id} ctx={ctx} row={r} except={groupProp.id} {...(canMove && ctx.canEditRow(r) ? { onLongPress: () => onMove(r) } : {})} />)}
      {!collapsed && ctx.canAdd && groupValue(groupProp, group.key) !== undefined ? (
        <Pressable accessibilityRole="button" accessibilityLabel={`New card in ${group.label}`} onPress={add} style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[1], minHeight: theme.tap.sm, paddingHorizontal: s[2], borderRadius: theme.radius.md, backgroundColor: pressed ? c.surface : "transparent" })}>
          <Plus size={16} color={c.textMuted} />
          <Text role="label" tone="muted">
            New
          </Text>
        </Pressable>
      ) : null}
    </View>
  );
}

/** Board: columns from the server's groups, optional sub-group lanes; long press moves a card. */
export function BoardView({ ctx }: { ctx: ViewCtx }) {
  const theme = useTheme();
  const s = theme.space;
  const groupProp = ctx.schema.find((p) => p.id === ctx.config.group_by);
  const subProp = ctx.schema.find((p) => p.id === ctx.config.sub_group_by);
  const [moving, setMoving] = useState<DocsRow | null>(null);
  if (!groupProp || !ctx.meta.groups) {
    return (
      <Text role="label" tone="muted">
        {groupProp ? "Loading the columns…" : "Choose a property to group the board by."}
      </Text>
    );
  }
  const columns = (rows: DocsRow[], lane?: { prop: DocsProperty; key: string }) => (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[3], alignItems: "flex-start" }}>
      {groupSections(rows, ctx.meta.groups).map(({ group, rows: rs }) => (
        <Column key={group.key || "none"} ctx={ctx} group={lane ? { ...group, count: rs.length } : group} rows={rs} groupProp={groupProp} onMove={setMoving} lane={lane} />
      ))}
    </ScrollView>
  );
  const move = (key: string) => {
    const r = moving;
    setMoving(null);
    const v = groupValue(groupProp, key);
    if (r && v !== undefined) ctx.setValues(r, { [groupProp.id]: v });
  };
  return (
    <View style={{ gap: s[4] }}>
      {subProp && ctx.meta.sub_groups
        ? groupSections(ctx.rows, ctx.meta.sub_groups, true).map(({ group, rows }) => (
            <View key={group.key || "none"} style={{ gap: s[2] }}>
              <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
                {group.key ? <OptionChip name={group.label} color={group.color} /> : <Text role="label" tone="muted">{group.label}</Text>}
                <Text role="caption" tone="muted" tabular>
                  {group.count}
                </Text>
              </View>
              {columns(rows, { prop: subProp, key: group.key })}
            </View>
          ))
        : columns(ctx.rows)}
      <Sheet open={!!moving} onClose={() => setMoving(null)} title="Move to" snapPoints={["50%"]}>
        <ScrollView>
          {(ctx.meta.groups ?? [])
            .filter((g) => !g.hidden)
            .map((g) => {
              const current = moving ? (moving.groups ?? [""]).includes(g.key) : false;
              return (
                <ListRow
                  key={g.key || "none"}
                  title={g.label}
                  leading={<OptionDot color={g.key ? g.color : undefined} />}
                  trailing={current ? <Check size={18} color={theme.colors.primaryStrong} accessibilityLabel="Current" /> : undefined}
                  disabled={current}
                  chevron={false}
                  onPress={() => move(g.key)}
                />
              );
            })}
        </ScrollView>
      </Sheet>
    </View>
  );
}
