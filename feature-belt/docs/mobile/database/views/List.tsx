import { docsTimeAgo, docsTitle, useDocsRows, type DocsRow } from "@scaffoldapp/shared";
import { Avatar, IconButton, Skeleton, Text, fadeIn, useTheme } from "@scaffoldapp/ui-native";
import { ChevronDown, ChevronRight, Plus } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { Pressable, View } from "react-native";
import Animated from "react-native-reanimated";
import type { ViewCtx } from "../context";
import { cardProps, groupSections } from "../model";
import { ValueView } from "../values";
import { GroupHeader } from "./Table";

function hasValue(v: unknown): boolean {
  return !(v === null || v === undefined || v === "" || (Array.isArray(v) && v.length === 0));
}

/** The card properties of a row as compact values. */
export function RowValues({ ctx, row, except }: { ctx: ViewCtx; row: DocsRow; except?: string | undefined }) {
  const s = useTheme().space;
  const shown = cardProps(ctx.schema, ctx.config, except).filter((p) => hasValue(row.props[p.id]) || ["button", "checkbox", "created_time", "last_edited_time", "created_by", "last_edited_by"].includes(p.type));
  if (!shown.length) return null;
  return (
    <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2], alignItems: "center" }}>
      {shown.map((p) => (
        <Pressable key={p.id} accessibilityRole="button" accessibilityLabel={p.name} onPress={() => ctx.editValue(row, p)} hitSlop={s[1]}>
          <ValueView prop={p} value={row.props[p.id]} refs={ctx.refs} row={row} compact pressing={ctx.pressing === `${row.id}:${p.id}`} {...(p.type === "button" && !ctx.token ? { onPressButton: () => ctx.pressButton(row, p) } : {})} />
        </Pressable>
      ))}
    </View>
  );
}

function Item({ ctx, row, depth }: { ctx: ViewCtx; row: DocsRow; depth: number }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [open, setOpen] = useState(false);
  const nested = !!ctx.config.sub_items && ctx.config.sub_items.display !== "flat";
  const subs = row.sub_count ?? 0;
  return (
    <>
      <View style={{ flexDirection: "row", alignItems: "flex-start", paddingLeft: depth * s[4], borderTopWidth: 1, borderTopColor: c.border }}>
        {nested ? (
          subs > 0 ? (
            <IconButton label={open ? `Hide sub-items of ${docsTitle(row.title)}` : `Show ${subs} sub-items of ${docsTitle(row.title)}`} icon={open ? ChevronDown : ChevronRight} variant="ghost" onPress={() => setOpen((o) => !o)} />
          ) : (
            <View style={{ width: s[3] }} />
          )
        ) : null}
        <Pressable
          accessibilityRole="link"
          accessibilityLabel={`Open ${docsTitle(row.title)}`}
          onPress={() => ctx.openRow(row.id)}
          style={({ pressed }) => ({ flex: 1, gap: s[1], paddingVertical: s[3], paddingHorizontal: s[3], minHeight: theme.tap.base, backgroundColor: pressed ? c.sunken : "transparent" })}
        >
          <Text role="body" weight={600} numberOfLines={2}>
            {row.icon ? `${row.icon} ` : ""}
            {docsTitle(row.title)}
          </Text>
          <RowValues ctx={ctx} row={row} />
        </Pressable>
      </View>
      {open ? <SubItems ctx={ctx} parent={row} depth={depth + 1} render={(r, sub) => <Item key={r.id} ctx={sub} row={r} depth={Math.min(depth + 1, 4)} />} /> : null}
    </>
  );
}

/** Nested sub-items of a row (fetched when opened) and an add button. */
export function SubItems({ ctx, parent, depth, render }: { ctx: ViewCtx; parent: DocsRow; depth: number; render: (r: DocsRow, ctx: ViewCtx) => ReactNode }) {
  const theme = useTheme();
  const s = theme.space;
  const q = useDocsRows(ctx.db.id, { ...ctx.params, parent: parent.id, cursor: undefined }, { token: ctx.token });
  const merged: ViewCtx = { ...ctx, refs: { users: { ...ctx.refs.users, ...q.data?.refs.users }, pages: { ...ctx.refs.pages, ...q.data?.refs.pages } } };
  return (
    <Animated.View entering={fadeIn(theme)}>
      {q.isPending ? <Skeleton height={theme.tap.base} /> : null}
      {q.isError ? (
        <Text role="label" tone="danger" style={{ padding: s[2] }}>
          Couldn't load the sub-items.
        </Text>
      ) : null}
      {(q.data?.rows ?? []).map((r) => render(r, merged))}
      {ctx.canAdd && ctx.canEditRow(parent) ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={`Add a sub-item to ${docsTitle(parent.title)}`}
          onPress={() => ctx.createRow(undefined, { parent_row: parent.id })}
          style={{ flexDirection: "row", alignItems: "center", gap: s[1], minHeight: theme.tap.sm, paddingLeft: s[4] + depth * s[4] }}
        >
          <Plus size={14} color={theme.colors.textMuted} />
          <Text role="label" tone="muted">
            Sub-item
          </Text>
        </Pressable>
      ) : null}
    </Animated.View>
  );
}

/** Sections by the server's groups, or one list. */
export function Grouped({ ctx, render }: { ctx: ViewCtx; render: (rows: DocsRow[]) => ReactNode }) {
  const s = useTheme().space;
  const groupProp = ctx.schema.find((p) => p.id === ctx.config.group_by);
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set(ctx.config.group?.collapsed ?? []));
  if (!groupProp || !ctx.meta.groups) return <>{render(ctx.rows)}</>;
  return (
    <View style={{ gap: s[2] }}>
      {groupSections(ctx.rows, ctx.meta.groups).map(({ group, rows }) => (
        <View key={group.key || "none"} style={{ gap: s[1] }}>
          <GroupHeader
            group={group}
            propName={groupProp.name}
            collapsed={collapsed.has(group.key)}
            onToggle={() =>
              setCollapsed((cur) => {
                const n = new Set(cur);
                if (n.has(group.key)) n.delete(group.key);
                else n.add(group.key);
                return n;
              })
            }
          />
          {collapsed.has(group.key) ? null : rows.length ? (
            render(rows)
          ) : (
            <Text role="label" tone="subtle" style={{ paddingHorizontal: s[3] }}>
              No rows
            </Text>
          )}
        </View>
      ))}
    </View>
  );
}

/** List: titles with their card properties, groups and nested sub-items. */
export function ListView({ ctx }: { ctx: ViewCtx }) {
  const theme = useTheme();
  return (
    <Grouped
      ctx={ctx}
      render={(rows) => (
        <View style={{ borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.md, overflow: "hidden", marginTop: -1 }}>
          {rows.map((r) => (
            <Item key={r.id} ctx={ctx} row={r} depth={0} />
          ))}
        </View>
      )}
    />
  );
}

/** Feed: rows as posts, newest first unless the view sorts them, with who added them. */
export function FeedView({ ctx }: { ctx: ViewCtx }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  return (
    <Grouped
      ctx={ctx}
      render={(rows) => (
        <View style={{ gap: s[3] }}>
          {rows.map((r) => {
            const author = r.created_by ? ctx.refs.users[r.created_by] : undefined;
            return (
              <Pressable
                key={r.id}
                accessibilityRole="link"
                accessibilityLabel={`${docsTitle(r.title)}${author ? `, by ${author.name}` : ""}`}
                onPress={() => ctx.openRow(r.id)}
                style={({ pressed }) => ({ gap: s[3], padding: s[4], borderRadius: theme.radius.lg, borderWidth: 1, borderColor: c.border, backgroundColor: pressed ? c.sunken : c.surface })}
              >
                <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
                  <Avatar name={author?.name ?? "?"} src={author?.avatar_url ?? null} size="sm" />
                  <Text role="label" numberOfLines={1} style={{ flex: 1 }}>
                    <Text role="label" weight={600}>
                      {author?.name ?? "Someone"}
                    </Text>
                    <Text role="label" tone="muted">{` · ${docsTimeAgo(r.created_at)}`}</Text>
                  </Text>
                </View>
                <Text role="titleSm" numberOfLines={3}>
                  {r.icon ? `${r.icon} ` : ""}
                  {docsTitle(r.title)}
                </Text>
                <RowValues ctx={ctx} row={r} />
              </Pressable>
            );
          })}
        </View>
      )}
    />
  );
}
