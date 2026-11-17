import {
  DOCS_VIEW_LABEL,
  docsDatabaseKeys,
  docsFilterCount,
  docsTimeZone,
  errorMessage,
  useDebouncedValue,
  useDocsCreateLinkedView,
  useDocsCreateRow,
  useDocsCreateView,
  useDocsDeleteView,
  useDocsPressButton,
  useDocsRows,
  useDocsSetRowValues,
  useDocsUpdateView,
  type DocsPage,
  type DocsProperty,
  type DocsRollupFunction,
  type DocsRow,
  type DocsRowsParams,
  type DocsView,
  type DocsViewConfig,
  type DocsViewType,
} from "@scaffoldapp/shared";
import { Button, Chip, ConfirmDialog, EmptyState, ErrorState, IconButton, Input, SkeletonRow, Text, ToggleChip, fadeIn, toast, useTheme } from "@scaffoldapp/ui-native";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowDownUp, Calculator, Funnel, Group, ListTree, Pencil, Plus, Rows3, Search, SlidersHorizontal, Trash, Workflow, X } from "lucide-react-native";
import { useMemo, useState } from "react";
import { ScrollView, View } from "react-native";
import Animated from "react-native-reanimated";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import type { ViewCtx } from "./database/context";
import { FormView } from "./database/Forms";
import { CALC_SHORT, VIEW_ICON, formatCalc, rowsMeta } from "./database/model";
import { openButtonResult, openRow as goToRow } from "./database/open";
import { NewRowButton } from "./database/Templates";
import { ViewSettings, type SettingsPane } from "./database/ViewSettings";
import { BoardView } from "./database/views/Board";
import { CalendarView } from "./database/views/Calendar";
import { ChartView } from "./database/views/Chart";
import { GalleryView } from "./database/views/Gallery";
import { FeedView, ListView } from "./database/views/List";
import { TableView } from "./database/views/Table";
import { TimelineView } from "./database/views/Timeline";
import { PropertySheet } from "./PropertySheet";

export { ValueView } from "./database/values";

const EMPTY_CONFIG: DocsViewConfig = { filters: [], sorts: [], columns: [], card_properties: [] };

/** Rows a view asks for per page. */
function pageSize(type: DocsViewType, config: DocsViewConfig): number {
  if (type === "calendar" || type === "timeline") return 500;
  if (type === "board") return Math.max(config.load_limit ?? 100, 100);
  return config.load_limit ?? 50;
}

export interface DatabaseViewProps {
  db: DocsPage;
  views: DocsView[];
  /** The viewer may change views (filters, sorts, layout…) for everyone. */
  editable: boolean;
  /** Through a public link: read-only. */
  token?: string | null;
  /** Rows may be edited and added (defaults to `editable`; each row's own level still applies). */
  rowsEditable?: boolean;
  /** Shown in a database block: its views refresh through the block (linked views live there). */
  block?: { pageId: string; blockId: string; linked: boolean } | undefined;
}

/** Footer calculations for views without a table footer. */
function CalcStrip({ ctx }: { ctx: ViewCtx }) {
  const s = useTheme().space;
  const entries = Object.entries(ctx.config.calculations ?? {})
    .map(([id, fn]) => ({ p: ctx.schema.find((x) => x.id === id), fn: fn as DocsRollupFunction }))
    .filter((x): x is { p: DocsProperty; fn: DocsRollupFunction } => !!x.p);
  if (!entries.length) return null;
  return (
    <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[3] }}>
      {entries.map(({ p, fn }) => (
        <Text key={p.id} role="caption" tone="muted">
          {`${p.name} · ${CALC_SHORT[fn] ?? fn} `}
          <Text role="caption" weight={600} tabular>
            {formatCalc(fn, ctx.meta.calculations?.[p.id], p, ctx.refs) || "–"}
          </Text>
        </Text>
      ))}
    </View>
  );
}

/**
 * A database on mobile: view chips; table, board, list, calendar, gallery, timeline, chart, form and feed views;
 * filters (nested and/or), sorts, groups, properties, footer calculations, sub-items and layout in sheets; values
 * edited in PropertySheet; templates on New. Viewers' filters and sorts apply on their device only.
 */
export function DatabaseView({ db, views, editable, token = null, rowsEditable, block }: DatabaseViewProps) {
  const theme = useTheme();
  const s = theme.space;
  const qc = useQueryClient();
  const [viewId, setViewId] = useState<string | null>(null);
  const view = views.find((v) => v.id === viewId) ?? views[0];
  const [local, setLocal] = useState<Record<string, DocsViewConfig>>({});
  const config = view ? (local[view.id] ?? view.config) : EMPTY_CONFIG;
  const schema = db.schema ?? [];
  const [searching, setSearching] = useState(false);
  const [q, setQ] = useState("");
  const dq = useDebouncedValue(q.trim(), 300);
  const [pane, setPane] = useState<SettingsPane | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [editing, setEditing] = useState<{ row: DocsRow; prop: DocsProperty; readOnly: boolean } | null>(null);
  const viewType = view?.type ?? "table";
  const needsRows = !!view && viewType !== "chart" && viewType !== "form";
  const params = useMemo<DocsRowsParams>(
    () => ({
      ...(view ? { view: view.id } : {}),
      tz: docsTimeZone(),
      limit: pageSize(viewType, config),
      filters: config.filters,
      ...(config.filter_conj ? { filter_conj: config.filter_conj } : {}),
      sorts: config.sorts,
      ...(dq ? { q: dq } : {}),
    }),
    [view, viewType, config, dq],
  );
  const rows = useDocsRows(db.id, params, { token, enabled: needsRows });
  const setValues = useDocsSetRowValues(db.id);
  const createRow = useDocsCreateRow(db.id);
  const updateView = useDocsUpdateView(db.id);
  const createView = useDocsCreateView(db.id);
  const createLinked = useDocsCreateLinkedView(block?.pageId ?? "", block?.blockId ?? "");
  const deleteView = useDocsDeleteView(db.id);
  const press = useDocsPressButton(db.id);

  const canChangeViews = editable && !token;
  const rowsOk = !token && (rowsEditable ?? editable) && !db.deleted_at;
  const canEditRow = (r: DocsRow) => rowsOk && (r.my_level === "edit" || r.my_level === "manage");
  const canAdd = rowsOk && (db.my_level === "edit" || db.my_level === "manage");
  const refreshBlock = () => (block ? qc.invalidateQueries({ queryKey: docsDatabaseKeys.blockDatabase(block.pageId, block.blockId) }) : Promise.resolve());

  const saveConfig = (next: DocsViewConfig) => {
    if (!view) return;
    const id = view.id;
    setLocal((m) => ({ ...m, [id]: next }));
    if (!canChangeViews) return;
    updateView.mutate(
      { viewId: id, config: next },
      {
        onSuccess: async () => {
          await refreshBlock();
          setLocal(({ [id]: _saved, ...rest }) => rest);
        },
        onError: (e) => {
          setLocal(({ [id]: _failed, ...rest }) => rest);
          toast.error(errorMessage(e, "Couldn't save the view."));
        },
      },
    );
  };

  function runButton(r: DocsRow, p: DocsProperty) {
    if (token || !canEditRow(r)) return;
    press.mutate({ rowId: r.id, propId: p.id }, { onSuccess: openButtonResult, onError: (e) => toast.error(errorMessage(e, "Couldn't run that button.")) });
  }

  const ctx: ViewCtx | null = view
    ? {
        db,
        schema,
        view,
        config,
        rows: rows.data?.rows ?? [],
        refs: rows.data?.refs ?? { users: {}, pages: {} },
        meta: rowsMeta(rows.data),
        params,
        token,
        editable: canChangeViews,
        canAdd,
        canEditRow,
        openRow: (id) => goToRow(id, token),
        editValue: (r, p) => {
          if (p.type === "title") return goToRow(r.id, token);
          if (p.type === "button") return runButton(r, p);
          if (p.type === "checkbox" && canEditRow(r) && !p.read_only) {
            setValues.mutate({ rowId: r.id, values: { [p.id]: r.props[p.id] !== true } }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save that value.")) });
            return;
          }
          setEditing({ row: r, prop: p, readOnly: !canEditRow(r) });
        },
        setValues: (r, values) => setValues.mutate({ rowId: r.id, values }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save that change.")) }),
        pressButton: runButton,
        pressing: press.isPending && press.variables ? `${press.variables.rowId}:${press.variables.propId}` : null,
        createRow: (values, opts) =>
          createRow.mutate(
            {
              ...(values ? { values } : {}),
              ...(opts?.blank ? {} : { view_id: view.id }),
              ...(opts?.template_id ? { template_id: opts.template_id } : {}),
              ...(opts?.parent_row ? { parent_row: opts.parent_row } : {}),
            },
            { onSuccess: (p) => goToRow(p.id, token), onError: (e) => toast.error(errorMessage(e, "Couldn't add a row.")) },
          ),
        saveConfig,
      }
    : null;

  const addView = (type: DocsViewType) => {
    const name = DOCS_VIEW_LABEL[type];
    const done = (v: DocsView) => {
      setPane(null);
      setViewId(v.id);
    };
    const fail = (e: unknown) => toast.error(errorMessage(e, "Couldn't add the view."));
    if (block?.linked) createLinked.mutate({ database_id: db.id, type, name }, { onSuccess: (v) => void refreshBlock().then(() => done(v)), onError: fail });
    else createView.mutate({ type, name }, { onSuccess: (v) => void refreshBlock().then(() => done(v)), onError: fail });
  };

  if (!view || !ctx) {
    return (
      <View style={{ gap: s[3] }}>
        <Text role="label" tone="muted">
          This database has no views.
        </Text>
        {canChangeViews ? (
          <Button variant="secondary" size="md" leftIcon={Plus} loading={createView.isPending || createLinked.isPending} onPress={() => addView("table")}>
            Add a table view
          </Button>
        ) : null}
      </View>
    );
  }

  const filterCount = docsFilterCount(config.filters);
  const type = view.type;
  const actions: ResourceMenuAction[] = canChangeViews
    ? [
        ...(needsRows ? [{ id: "group", label: "Group", icon: Group, onSelect: () => setPane("group") }] : []),
        ...(needsRows ? [{ id: "properties", label: "Properties", icon: Rows3, onSelect: () => setPane("properties") }] : []),
        ...(needsRows ? [{ id: "calculations", label: "Calculations", icon: Calculator, onSelect: () => setPane("calculations") }] : []),
        ...(type !== "form" ? [{ id: "layout", label: "Layout", icon: SlidersHorizontal, onSelect: () => setPane("layout") }] : []),
        ...(type === "table" || type === "list" ? [{ id: "subitems", label: "Sub-items", icon: ListTree, onSelect: () => setPane("subitems") }] : []),
        { id: "rename", label: "Rename view", icon: Pencil, onSelect: () => setPane("rename") },
        { id: "newview", label: "New view", icon: Plus, onSelect: () => setPane("newview") },
        ...(db.my_level === "edit" || db.my_level === "manage" ? [{ id: "automations", label: "Automations", icon: Workflow, onSelect: () => setPane("automations") }] : []),
        ...(views.length > 1 || block?.linked ? [{ id: "delete", label: "Delete view", icon: Trash, destructive: true, onSelect: () => setDeleting(true) }] : []),
      ]
    : [];

  let body;
  if (type === "chart") body = <ChartView ctx={ctx} />;
  else if (type === "form") body = <FormView ctx={ctx} />;
  else if (rows.isPending) {
    body = (
      <View style={{ gap: s[2] }}>
        <SkeletonRow />
        <SkeletonRow />
        <SkeletonRow />
      </View>
    );
  } else if (rows.isError) {
    body = <ErrorState title="Couldn't load the rows." info={errorMessage(rows.error)} onRetry={() => void rows.refetch()} compact />;
  } else if (ctx.rows.length === 0 && !(type === "board" && ctx.meta.groups?.length) && type !== "calendar" && type !== "timeline") {
    body =
      filterCount || dq ? (
        <EmptyState
          title="No rows match"
          compact
          action={
            <Button
              variant="secondary"
              size="md"
              onPress={() => {
                setQ("");
                setSearching(false);
                if (filterCount) saveConfig({ ...config, filters: [] });
              }}
            >
              Clear filters
            </Button>
          }
        />
      ) : (
        <EmptyState title="No rows yet" compact />
      );
  } else if (type === "table") body = <TableView ctx={ctx} />;
  else if (type === "board") body = <BoardView ctx={ctx} />;
  else if (type === "calendar") body = <CalendarView ctx={ctx} />;
  else if (type === "gallery") body = <GalleryView ctx={ctx} />;
  else if (type === "timeline") body = <TimelineView ctx={ctx} />;
  else if (type === "feed") body = <FeedView ctx={ctx} />;
  else body = <ListView ctx={ctx} />;

  return (
    <View style={{ gap: s[3] }}>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2], alignItems: "center" }}>
        {views.map((v) => (
          <ToggleChip key={v.id} size="sm" pressed={v.id === view.id} onPressedChange={() => setViewId(v.id)} icon={VIEW_ICON[v.type] ?? Rows3}>
            {v.name}
          </ToggleChip>
        ))}
        {canChangeViews ? <IconButton label="New view" icon={Plus} variant="ghost" onPress={() => setPane("newview")} /> : null}
      </ScrollView>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        {needsRows ? (
          <>
            <Chip size="sm" icon={Funnel} selected={filterCount > 0} {...(filterCount ? { count: filterCount } : {})} onPress={() => setPane("filter")} accessibilityLabel={filterCount ? `Filter, ${filterCount} on` : "Filter"}>
              Filter
            </Chip>
            <Chip size="sm" icon={ArrowDownUp} selected={config.sorts.length > 0} {...(config.sorts.length ? { count: config.sorts.length } : {})} onPress={() => setPane("sort")} accessibilityLabel={config.sorts.length ? `Sort, ${config.sorts.length} on` : "Sort"}>
              Sort
            </Chip>
            <IconButton
              label={searching ? "Close search" : "Search rows"}
              icon={searching ? X : Search}
              variant="ghost"
              onPress={() => {
                if (searching) setQ("");
                setSearching((x) => !x);
              }}
            />
          </>
        ) : null}
        <View style={{ flex: 1 }} />
        {needsRows && ctx.meta.total !== undefined ? (
          <Text role="caption" tone="muted" tabular>
            {`${ctx.meta.total} ${ctx.meta.total === 1 ? "row" : "rows"}`}
          </Text>
        ) : null}
        {!token ? <ResourceMenu feature="docs" {...(type === "form" ? { kind: "docs.form", id: view.id } : {})} title={view.name} orgId={db.org_id} label="View options" actions={actions} /> : null}
      </View>
      {searching ? <Input value={q} onChangeText={setQ} placeholder="Search rows" accessibilityLabel="Search rows" autoFocus autoCorrect={false} returnKeyType="search" leadingIcon={Search} /> : null}
      <Animated.View key={view.id} entering={fadeIn(theme)} style={{ gap: s[3] }}>
        {body}
        {needsRows && type !== "table" && !rows.isPending ? <CalcStrip ctx={ctx} /> : null}
      </Animated.View>
      {needsRows && rows.hasNextPage ? (
        <Button variant="ghost" size="md" loading={rows.isFetchingNextPage} onPress={() => void rows.fetchNextPage()}>
          Load more
        </Button>
      ) : null}
      {needsRows && canAdd && type !== "calendar" ? <NewRowButton ctx={ctx} creating={createRow.isPending} /> : null}
      <PropertySheet
        open={!!editing}
        prop={editing?.prop ?? null}
        value={editing ? editing.row.props[editing.prop.id] : undefined}
        orgId={db.org_id}
        refs={ctx.refs}
        rowId={editing?.row.id}
        dbId={db.id}
        row={editing?.row}
        readOnly={editing?.readOnly ?? false}
        token={token}
        onOpenPage={(id) => {
          setEditing(null);
          goToRow(id, token);
        }}
        onClose={() => setEditing(null)}
        onSave={(v) => {
          const e = editing;
          setEditing(null);
          if (e && !e.readOnly) setValues.mutate({ rowId: e.row.id, values: { [e.prop.id]: v } }, { onError: (err) => toast.error(errorMessage(err, "Couldn't save that value.")) });
        }}
      />
      <ViewSettings
        ctx={ctx}
        pane={pane}
        onClose={() => setPane(null)}
        onRename={(name) => updateView.mutate({ viewId: view.id, name }, { onSuccess: () => void refreshBlock(), onError: (e) => toast.error(errorMessage(e, "Couldn't rename the view.")) })}
        onCreateView={addView}
        creatingView={createView.isPending || createLinked.isPending}
      />
      <ConfirmDialog
        open={deleting}
        onClose={() => setDeleting(false)}
        title={`Delete the view "${view.name}"? The rows stay in the database.`}
        confirmLabel="Delete view"
        tone="danger"
        onConfirm={() =>
          deleteView.mutateAsync(view.id).then(() => {
            setViewId(null);
            void refreshBlock();
          })
        }
        errorText={(e) => errorMessage(e, "Couldn't delete the view.")}
      />
    </View>
  );
}
