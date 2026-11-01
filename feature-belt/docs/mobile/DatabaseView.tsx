import {
  docsComparePosition,
  docsFormatValue,
  docsGroupRows,
  docsOption,
  docsTitle,
  errorMessage,
  useDocsCreateRow,
  useDocsRows,
  useDocsSetRowValues,
  type DocsDateValue,
  type DocsPage,
  type DocsProperty,
  type DocsRefs,
  type DocsRow,
  type DocsValue,
  type DocsView,
} from "@scaffoldapp/shared";
import { Badge, Button, ErrorState, ListRow, Sheet, SkeletonRow, Text, ToggleChip, toast, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { CalendarDays, Columns3, LayoutList, Plus, Table2 } from "lucide-react-native";
import { useMemo, useState } from "react";
import { Pressable, ScrollView, View } from "react-native";
import { PropertySheet } from "./PropertySheet";

const ICON = { table: Table2, board: Columns3, list: LayoutList, calendar: CalendarDays } as const;

/** A value as text or badges in a cell or card. */
export function ValueView({ prop, value, refs }: { prop: DocsProperty; value: DocsValue | undefined; refs: DocsRefs }) {
  const theme = useTheme();
  if (value === null || value === undefined || (Array.isArray(value) && value.length === 0)) {
    return prop.type === "checkbox" ? <Text role="label">No</Text> : <Text role="label" tone="subtle">Empty</Text>;
  }
  if (prop.type === "select" || prop.type === "status") {
    const o = docsOption(prop, value);
    return o ? <Badge tone={o.color}>{o.name}</Badge> : null;
  }
  if (prop.type === "multi_select") {
    return (
      <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[1] }}>
        {(value as string[]).map((id) => {
          const o = docsOption(prop, id);
          return o ? (
            <Badge key={id} tone={o.color} size="sm">
              {o.name}
            </Badge>
          ) : null;
        })}
      </View>
    );
  }
  return (
    <Text role="label" numberOfLines={2}>
      {docsFormatValue(prop, value, refs)}
    </Text>
  );
}

/**
 * A database on mobile: views as chips; table and board read with property edits
 * (tap a cell or card value); list and calendar views show as a list (calendar rows by
 * date).
 */
export function DatabaseView({ db, views, editable, token = null }: { db: DocsPage; views: DocsView[]; editable: boolean; token?: string | null }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [viewId, setViewId] = useState<string | null>(null);
  const view = views.find((v) => v.id === viewId) ?? views[0];
  const schema = db.schema ?? [];
  const big = view?.type === "board" || view?.type === "calendar";
  const params = useMemo(() => {
    const p = { filters: view?.config.filters ?? [], sorts: view?.config.sorts ?? [], limit: big ? 1000 : 100 };
    if (view?.type === "calendar" && view.config.date_property) p.sorts = [{ property: view.config.date_property, direction: "asc" }];
    return p;
  }, [view, big]);
  const rows = useDocsRows(db.id, params, { token, enabled: !!view });
  const setValues = useDocsSetRowValues(db.id);
  const createRow = useDocsCreateRow(db.id);
  const [editing, setEditing] = useState<{ row: DocsRow; prop: DocsProperty } | null>(null);
  const [moving, setMoving] = useState<DocsRow | null>(null);
  const refs = rows.data?.refs ?? { users: {}, pages: {} };
  const list = rows.data?.rows ?? [];
  const openRow = (id: string) => router.push((token ? `/p/${token}?page=${id}` : `/docs/${id}`) as never);
  const canEdit = (r: DocsRow) => editable && !token && (r.my_level === "edit" || r.my_level === "manage");
  const edit = (row: DocsRow, prop: DocsProperty) => {
    if (!canEdit(row)) return;
    if (prop.type === "checkbox") setValues.mutate({ rowId: row.id, values: { [prop.id]: row.props[prop.id] !== true } }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save that value.")) });
    else if (prop.type === "title") openRow(row.id);
    else setEditing({ row, prop });
  };
  if (!view) return <Text tone="muted">This database has no views.</Text>;
  const columns = view.config.columns.length ? view.config.columns.filter((col) => !col.hidden).map((col) => schema.find((p) => p.id === col.property)).filter((p): p is DocsProperty => !!p) : schema;
  const cardProps = view.config.card_properties.map((id) => schema.find((p) => p.id === id)).filter((p): p is DocsProperty => !!p && p.type !== "title");
  const group = schema.find((p) => p.id === view.config.group_by);

  let body;
  if (rows.isPending) body = <SkeletonRow />;
  else if (rows.isError) body = <ErrorState title="Couldn't load the rows." onRetry={() => void rows.refetch()} compact />;
  else if (list.length === 0) body = <Text tone="muted">No rows match this view.</Text>;
  else if (view.type === "table") {
    body = (
      <ScrollView horizontal>
        <View style={{ borderWidth: 1, borderColor: c.border, borderRadius: theme.radius.md }}>
          <View style={{ flexDirection: "row", backgroundColor: c.sunken }}>
            {columns.map((p) => (
              <View key={p.id} style={{ width: p.type === "title" ? 200 : 150, padding: s[2] }}>
                <Text role="label" tone="muted" numberOfLines={1}>
                  {p.name}
                </Text>
              </View>
            ))}
          </View>
          {list.map((r) => (
            <View key={r.id} style={{ flexDirection: "row", borderTopWidth: 1, borderTopColor: c.border }}>
              {columns.map((p) => (
                <Pressable
                  key={p.id}
                  accessibilityRole="button"
                  accessibilityLabel={p.type === "title" ? `Open ${docsTitle(r.title)}` : `${p.name}: ${docsFormatValue(p, r.props[p.id], refs) || "empty"}`}
                  onPress={() => (p.type === "title" ? openRow(r.id) : edit(r, p))}
                  style={({ pressed }) => ({ width: p.type === "title" ? 200 : 150, minHeight: theme.tap.base, padding: s[2], justifyContent: "center", backgroundColor: pressed ? c.sunken : "transparent" })}
                >
                  {p.type === "title" ? (
                    <Text role="label" weight={600} numberOfLines={2}>
                      {r.icon ? `${r.icon} ` : ""}
                      {docsTitle(r.title)}
                    </Text>
                  ) : (
                    <ValueView prop={p} value={r.props[p.id]} refs={refs} />
                  )}
                </Pressable>
              ))}
            </View>
          ))}
        </View>
      </ScrollView>
    );
  } else if (view.type === "board" && group) {
    const cols = docsGroupRows([...list].sort(docsComparePosition), group);
    body = (
      <ScrollView horizontal contentContainerStyle={{ gap: s[3] }}>
        {cols.map((col) => (
          <View key={col.option?.id ?? "none"} style={{ width: 260, gap: s[2], backgroundColor: c.sunken, borderRadius: theme.radius.md, padding: s[2] }}>
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
              {col.option ? <Badge tone={col.option.color}>{col.option.name}</Badge> : <Text role="label" tone="muted">{`No ${group.name.toLowerCase()}`}</Text>}
              <Text role="caption" tone="muted">
                {col.rows.length}
              </Text>
            </View>
            {col.rows.map((r) => (
              <Pressable
                key={r.id}
                accessibilityRole="button"
                accessibilityHint={canEdit(r) ? "Long press to move to another column." : undefined}
                onPress={() => openRow(r.id)}
                onLongPress={() => canEdit(r) && setMoving(r)}
                style={({ pressed }) => ({ gap: s[2], padding: s[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, backgroundColor: pressed ? c.surfaceRaised : c.surface })}
              >
                <Text role="label" weight={600}>
                  {r.icon ? `${r.icon} ` : ""}
                  {docsTitle(r.title)}
                </Text>
                {cardProps.map((p) =>
                  r.props[p.id] !== undefined && r.props[p.id] !== null ? (
                    <Pressable key={p.id} onPress={() => edit(r, p)} disabled={!canEdit(r)}>
                      <ValueView prop={p} value={r.props[p.id]} refs={refs} />
                    </Pressable>
                  ) : null,
                )}
              </Pressable>
            ))}
          </View>
        ))}
      </ScrollView>
    );
  } else {
    const dateProp = view.type === "calendar" ? schema.find((p) => p.id === view.config.date_property) : undefined;
    body = (
      <View style={{ borderWidth: 1, borderColor: c.border, borderRadius: theme.radius.md, overflow: "hidden" }}>
        {list.map((r) => (
          <ListRow
            key={r.id}
            title={`${r.icon ? `${r.icon} ` : ""}${docsTitle(r.title)}`}
            trailing={dateProp ? <Text role="caption" tone="muted">{docsFormatValue(dateProp, r.props[dateProp.id] as DocsDateValue | undefined, refs)}</Text> : cardProps[0] ? <ValueView prop={cardProps[0]} value={r.props[cardProps[0].id]} refs={refs} /> : undefined}
            onPress={() => openRow(r.id)}
          />
        ))}
      </View>
    );
  }

  return (
    <View style={{ gap: s[3] }}>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2] }}>
        {views.map((v) => (
          <ToggleChip key={v.id} pressed={v.id === view.id} onPressedChange={() => setViewId(v.id)} icon={ICON[v.type]}>
            {v.name}
          </ToggleChip>
        ))}
      </ScrollView>
      {body}
      {rows.hasNextPage ? (
        <Button variant="ghost" size="md" onPress={() => void rows.fetchNextPage()}>
          Load more
        </Button>
      ) : null}
      {editable && !token ? (
        <Button variant="secondary" size="md" leftIcon={Plus} loading={createRow.isPending} onPress={() => createRow.mutate({}, { onSuccess: (p) => openRow(p.id), onError: (e) => toast.error(errorMessage(e, "Couldn't add a row.")) })}>
          New row
        </Button>
      ) : null}
      <PropertySheet
        open={!!editing}
        prop={editing?.prop ?? null}
        value={editing ? editing.row.props[editing.prop.id] : undefined}
        orgId={db.org_id}
        onClose={() => setEditing(null)}
        onSave={(v) => {
          const e = editing;
          setEditing(null);
          if (e) setValues.mutate({ rowId: e.row.id, values: { [e.prop.id]: v } }, { onError: (err) => toast.error(errorMessage(err, "Couldn't save that value.")) });
        }}
      />
      <Sheet open={!!moving} onClose={() => setMoving(null)} title="Move to">
        {group
          ? [...(group.options ?? []), null].map((o) => (
              <ListRow
                key={o?.id ?? "none"}
                title={o?.name ?? `No ${group.name.toLowerCase()}`}
                disabled={(moving?.props[group.id] ?? null) === (o?.id ?? null)}
                chevron={false}
                onPress={() => {
                  const r = moving;
                  setMoving(null);
                  if (r) setValues.mutate({ rowId: r.id, values: { [group.id]: o?.id ?? null } }, { onError: (e) => toast.error(errorMessage(e, "Couldn't move the card.")) });
                }}
              />
            ))
          : null}
      </Sheet>
    </View>
  );
}
