import { docsTitle, useDocsRows, type DocsGroupInfo, type DocsProperty, type DocsRollupFunction, type DocsRow } from "@scaffoldapp/shared";
import { IconButton, Skeleton, Text, fadeIn, useTheme } from "@scaffoldapp/ui-native";
import { ChevronDown, ChevronRight, Plus } from "lucide-react-native";
import { createContext, useContext, useState, type ReactNode } from "react";
import { Pressable, View } from "react-native";
import Animated, { useAnimatedScrollHandler, useAnimatedStyle, useSharedValue, type SharedValue } from "react-native-reanimated";
import type { ViewCtx } from "../context";
import { CALC_SHORT, formatCalc, groupSections, visibleColumns } from "../model";
import { OptionChip, ValueView, valueLabel } from "../values";

const Scroll = createContext<SharedValue<number> | null>(null);

/** A cell that stays put while the table scrolls sideways (the title column). */
function Sticky({ width, children, bg }: { width: number; children: ReactNode; bg: string }) {
  const x = useContext(Scroll);
  const st = useAnimatedStyle(() => ({ transform: [{ translateX: x ? x.value : 0 }] }));
  return <Animated.View style={[{ width, zIndex: 1, backgroundColor: bg }, st]}>{children}</Animated.View>;
}

function colWidth(p: DocsProperty, ctx: ViewCtx): number {
  const w = ctx.config.columns.find((c) => c.property === p.id)?.width;
  if (p.type === "title") return 184;
  return w ? Math.min(Math.max(Math.round(w * 0.8), 112), 280) : 148;
}

export function GroupHeader({ group, collapsed, onToggle, propName }: { group: DocsGroupInfo; collapsed: boolean; onToggle: () => void; propName?: string | undefined }) {
  const theme = useTheme();
  const s = theme.space;
  const Icon = collapsed ? ChevronRight : ChevronDown;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${propName ? `${propName} ` : ""}${group.label}, ${group.count} ${group.count === 1 ? "row" : "rows"}`}
      accessibilityState={{ expanded: !collapsed }}
      onPress={onToggle}
      style={{ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: theme.tap.base, paddingHorizontal: s[1] }}
    >
      <Icon size={16} color={theme.colors.textMuted} />
      {group.key ? <OptionChip name={group.label} color={group.color} /> : <Text role="label" tone="muted">{group.label}</Text>}
      <Text role="caption" tone="muted" tabular>
        {group.count}
      </Text>
    </Pressable>
  );
}

interface LineProps {
  ctx: ViewCtx;
  row: DocsRow;
  cols: DocsProperty[];
  depth: number;
}

function Line({ ctx, row, cols, depth }: LineProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [open, setOpen] = useState(false);
  const nested = ctx.config.sub_items?.display !== "flat" && !!ctx.config.sub_items;
  const subs = row.sub_count ?? 0;
  const wrap = !!ctx.config.wrap;
  const title = schemaTitle(ctx);
  return (
    <>
      <View style={{ flexDirection: "row", borderTopWidth: 1, borderTopColor: c.border, minHeight: theme.tap.base }}>
        <Sticky width={title ? colWidth(title, ctx) : 184} bg={c.surface}>
          <View style={{ flexDirection: "row", alignItems: "center", minHeight: theme.tap.base, paddingLeft: s[1] + depth * s[4], borderRightWidth: 1, borderRightColor: c.border, flex: 1 }}>
            {nested && subs > 0 ? (
              <IconButton label={open ? `Hide sub-items of ${docsTitle(row.title)}` : `Show ${subs} sub-items of ${docsTitle(row.title)}`} icon={open ? ChevronDown : ChevronRight} variant="ghost" onPress={() => setOpen((o) => !o)} />
            ) : nested ? (
              <View style={{ width: s[2] }} />
            ) : null}
            <Pressable
              accessibilityRole="link"
              accessibilityLabel={`Open ${docsTitle(row.title)}`}
              onPress={() => ctx.openRow(row.id)}
              style={({ pressed }) => ({ flex: 1, alignSelf: "stretch", justifyContent: "center", paddingHorizontal: s[2], paddingVertical: s[1], backgroundColor: pressed ? c.sunken : "transparent" })}
            >
              <Text role="label" weight={600} numberOfLines={wrap ? 3 : 1}>
                {row.icon ? `${row.icon} ` : ""}
                {docsTitle(row.title)}
              </Text>
            </Pressable>
          </View>
        </Sticky>
        {cols.map((p) => {
          return (
            <Pressable
              key={p.id}
              accessibilityRole="button"
              accessibilityLabel={valueLabel(p, row.props[p.id], ctx.refs, row)}
              onPress={() => ctx.editValue(row, p)}
              style={({ pressed }) => ({ width: colWidth(p, ctx), minHeight: theme.tap.base, paddingHorizontal: s[2], paddingVertical: s[1], justifyContent: "center", overflow: "hidden", backgroundColor: pressed ? c.sunken : "transparent" })}
            >
              <ValueView
                prop={p}
                value={row.props[p.id]}
                refs={ctx.refs}
                row={row}
                compact={!wrap}
                pressing={ctx.pressing === `${row.id}:${p.id}`}
                {...(p.type === "button" && !ctx.token ? { onPressButton: () => ctx.pressButton(row, p) } : {})}
              />
            </Pressable>
          );
        })}
      </View>
      {open ? <SubLines ctx={ctx} parent={row} cols={cols} depth={depth + 1} /> : null}
    </>
  );
}

function schemaTitle(ctx: ViewCtx): DocsProperty | undefined {
  return ctx.schema.find((p) => p.type === "title");
}

/** Nested sub-items of a row, fetched when opened. */
function SubLines({ ctx, parent, cols, depth }: { ctx: ViewCtx; parent: DocsRow; cols: DocsProperty[]; depth: number }) {
  const theme = useTheme();
  const s = theme.space;
  const q = useDocsRows(ctx.db.id, { ...ctx.params, parent: parent.id, cursor: undefined }, { token: ctx.token });
  const kids = q.data?.rows ?? [];
  const merged = { ...ctx, refs: { users: { ...ctx.refs.users, ...q.data?.refs.users }, pages: { ...ctx.refs.pages, ...q.data?.refs.pages } } };
  return (
    <Animated.View entering={fadeIn(theme)}>
      {q.isPending ? <Skeleton height={theme.tap.base} /> : null}
      {q.isError ? (
        <Text role="label" tone="danger" style={{ padding: s[2] }}>
          Couldn't load the sub-items.
        </Text>
      ) : null}
      {kids.map((r) => (
        <Line key={r.id} ctx={merged} row={r} cols={cols} depth={Math.min(depth, 4)} />
      ))}
      {ctx.canAdd && ctx.canEditRow(parent) ? (
        <Sticky width={240} bg="transparent">
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={`Add a sub-item to ${docsTitle(parent.title)}`}
            onPress={() => ctx.createRow(undefined, { parent_row: parent.id })}
            style={{ flexDirection: "row", alignItems: "center", gap: s[1], minHeight: theme.tap.sm, paddingLeft: s[3] + depth * s[4] }}
          >
            <Plus size={14} color={theme.colors.textMuted} />
            <Text role="label" tone="muted">
              Sub-item
            </Text>
          </Pressable>
        </Sticky>
      ) : null}
    </Animated.View>
  );
}

/** Footer calculations under their columns. */
function CalcLine({ ctx, cols }: { ctx: ViewCtx; cols: DocsProperty[] }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const calcs = ctx.config.calculations ?? {};
  const title = schemaTitle(ctx);
  if (!Object.keys(calcs).length) return null;
  const cell = (p: DocsProperty | undefined, width: number) => {
    const fn = p ? (calcs[p.id] as DocsRollupFunction | undefined) : undefined;
    const v = p && fn ? formatCalc(fn, ctx.meta.calculations?.[p.id], p, ctx.refs) : "";
    return (
      <View style={{ width, paddingHorizontal: s[2], paddingVertical: s[2], flexDirection: "row", gap: s[1], alignItems: "baseline", justifyContent: "flex-end" }} accessible={!!fn} accessibilityLabel={fn && p ? `${p.name} ${CALC_SHORT[fn] ?? fn}: ${v || "none"}` : undefined}>
        {fn ? (
          <>
            <Text role="caption" tone="muted">
              {CALC_SHORT[fn] ?? fn}
            </Text>
            <Text role="label" weight={600} tabular numberOfLines={1} style={{ flexShrink: 1 }}>
              {v || "–"}
            </Text>
          </>
        ) : null}
      </View>
    );
  };
  return (
    <View style={{ flexDirection: "row", borderTopWidth: 1, borderTopColor: c.border, backgroundColor: c.sunken }}>
      <Sticky width={title ? colWidth(title, ctx) : 184} bg={c.sunken}>
        {cell(title, title ? colWidth(title, ctx) : 184)}
      </Sticky>
      {cols.map((p) => (
        <View key={p.id}>{cell(p, colWidth(p, ctx))}</View>
      ))}
    </View>
  );
}

/** Table: a frozen title column, every visible property, groups, nested sub-items and footer calculations. */
export function TableView({ ctx }: { ctx: ViewCtx }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const x = useSharedValue(0);
  const onScroll = useAnimatedScrollHandler((e) => {
    x.value = e.contentOffset.x;
  });
  const all = visibleColumns(ctx.schema, ctx.config);
  const title = all.find((p) => p.type === "title");
  const cols = all.filter((p) => p.type !== "title");
  const groupProp = ctx.schema.find((p) => p.id === ctx.config.group_by);
  const sections = groupProp && ctx.meta.groups ? groupSections(ctx.rows, ctx.meta.groups) : null;
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set(ctx.config.group?.collapsed ?? []));
  const toggle = (k: string) =>
    setCollapsed((cur) => {
      const n = new Set(cur);
      if (n.has(k)) n.delete(k);
      else n.add(k);
      return n;
    });
  return (
    <Scroll.Provider value={x}>
      <Animated.ScrollView horizontal onScroll={onScroll} scrollEventThrottle={16} showsHorizontalScrollIndicator={false} style={{ borderWidth: 1, borderColor: c.border, borderRadius: theme.radius.md }}>
        <View>
          <View style={{ flexDirection: "row", backgroundColor: c.sunken }}>
            <Sticky width={title ? colWidth(title, ctx) : 184} bg={c.sunken}>
              <Text role="label" tone="muted" weight={600} numberOfLines={1} style={{ padding: s[2], borderRightWidth: 1, borderRightColor: c.border }}>
                {title?.name ?? "Name"}
              </Text>
            </Sticky>
            {cols.map((p) => (
              <View key={p.id} style={{ width: colWidth(p, ctx), padding: s[2] }}>
                <Text role="label" tone="muted" weight={600} numberOfLines={1}>
                  {p.name}
                </Text>
              </View>
            ))}
          </View>
          {sections
            ? sections.map(({ group, rows }) => (
                <View key={group.key || "none"}>
                  <View style={{ flexDirection: "row", borderTopWidth: 1, borderTopColor: c.border }}>
                    <Sticky width={280} bg={c.surface}>
                      <GroupHeader group={group} collapsed={collapsed.has(group.key)} onToggle={() => toggle(group.key)} propName={groupProp?.name} />
                    </Sticky>
                  </View>
                  {collapsed.has(group.key) ? null : rows.map((r) => <Line key={r.id} ctx={ctx} row={r} cols={cols} depth={0} />)}
                </View>
              ))
            : ctx.rows.map((r) => <Line key={r.id} ctx={ctx} row={r} cols={cols} depth={0} />)}
          <CalcLine ctx={ctx} cols={cols} />
        </View>
      </Animated.ScrollView>
    </Scroll.Provider>
  );
}
