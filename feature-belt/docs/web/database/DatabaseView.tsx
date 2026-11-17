"use client";

import {
  DOCS_VIEW_LABEL,
  DOCS_VIEW_TYPES,
  docsDatabaseKeys,
  docsDefaultFilter,
  docsFilterCount,
  docsTimeZone,
  docsTitle,
  errorMessage,
  useDocsBlockDatabase,
  useDocsCreateLinkedView,
  useDocsCreateRow,
  useDocsCreateView,
  useDocsDeleteView,
  useDocsDuplicatePage,
  useDocsPage,
  useDocsPressButton,
  useDocsPutSchema,
  useDocsRows,
  useDocsSetRowValues,
  useDocsTrashPage,
  useDocsUpdateView,
  type DocsPage,
  type DocsProperty,
  type DocsRow,
  type DocsRowsParams,
  type DocsValue,
  type DocsView,
  type DocsViewConfig,
  type DocsViewType,
} from "@scaffoldapp/shared";
import { Badge, Button, ConfirmDialog, ErrorState, IconButton, Input, Skeleton, cn, toast } from "@scaffoldapp/ui-web";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowUpDown, ArrowUpRight, Copy, Download, Filter, Import, Pencil, Plus, Search, Settings2, SlidersHorizontal, Trash2, Workflow, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState, type ComponentType } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { Floating, FloatingMenu } from "../Floating";
import { VIEW_ICON } from "../format";
import { AutomationsSheet } from "./automations";
import { ChartView } from "./chart";
import { CsvExportDialog, CsvImportDialog } from "./csv";
import { FeedView } from "./feed";
import { FormBuilder } from "./form";
import { GalleryView } from "./gallery";
import { EMPTY_CONFIG } from "./model";
import { FilterPanel, SortPanel } from "./panels";
import { PropertySheet } from "./propertySettings";
import { NewWithTemplates } from "./templates";
import { TimelineView } from "./timeline";
import type { ViewProps, ViewSettingsSection } from "./types";
import { ViewSettingsSheet } from "./viewSettings";
import { BoardView, CalendarView, ListView, TableView } from "./views";

const BODY: Record<DocsViewType, ComponentType<ViewProps>> = {
  table: TableView,
  board: BoardView,
  list: ListView,
  calendar: CalendarView,
  gallery: GalleryView,
  timeline: TimelineView,
  chart: ChartView,
  form: FormBuilder,
  feed: FeedView,
};

/** Rows a view reads per page. */
function pageSize(type: DocsViewType, cfg: DocsViewConfig): number {
  switch (type) {
    case "board":
      return 200;
    case "calendar":
    case "timeline":
      return 500;
    default:
      return cfg.load_limit ?? 50;
  }
}

function rememberView(dbId: string, viewId: string) {
  try {
    window.localStorage.setItem(`scaffoldapp.docs.view.${dbId}`, viewId);
  } catch {
    // Storage blocked: the first view opens.
  }
}

function recallView(dbId: string): string | null {
  try {
    return window.localStorage.getItem(`scaffoldapp.docs.view.${dbId}`);
  } catch {
    return null;
  }
}

/**
 * A database block. With `blockId` and `pageId` (the editor passes both) a block showing a
 * database that lives elsewhere is a linked view: it keeps its own views on the host page.
 * Its title links to the source database.
 */
export function InlineDatabase({ databaseId, blockId, pageId }: { databaseId: string; blockId?: string; pageId?: string }) {
  const both = !!(pageId && blockId);
  const block = useDocsBlockDatabase(both ? pageId : null, both ? blockId : null);
  const detail = useDocsPage(databaseId);
  const linked = !!block.data?.linked;
  const createLinked = useDocsCreateLinkedView(pageId ?? "", blockId ?? "");
  const started = useRef(false);
  const canEditViews = !!block.data?.can_edit;
  const blockViews = block.data?.views;
  const mutateLinked = createLinked.mutate;
  useEffect(() => {
    // A new linked view starts with a table, once.
    if (!linked || !canEditViews || started.current || !blockViews || blockViews.length > 0) return;
    started.current = true;
    mutateLinked({ database_id: databaseId, type: "table", name: "Table" }, { onError: (e) => toast.error(errorMessage(e, "Couldn't add a view.")) });
  }, [linked, canEditViews, blockViews, databaseId, mutateLinked]);
  if (detail.isPending || (both && block.isPending)) return <Skeleton className="h-40 w-full rounded-md" />;
  if (block.data && block.data.database === null) return <NoAccess />;
  if (detail.isError) {
    return detail.error.status === 404 || detail.error.status === 403 ? <NoAccess /> : <ErrorState title="Couldn't load this database." onRetry={() => void detail.refetch()} />;
  }
  const d = detail.data;
  const editable = (d.page.my_level === "edit" || d.page.my_level === "manage") && !d.page.locked && !d.page.deleted_at;
  const views = linked ? (blockViews ?? []) : d.views;
  return (
    <div data-database className="flex flex-col gap-2">
      <Link href={`/docs/${d.page.id}`} className="flex w-fit items-center gap-2 rounded-sm type-title-sm text-fg hover:underline focus-visible:outline-2 focus-visible:outline-ring">
        {linked ? <ArrowUpRight aria-label="Linked database" className="size-4 text-fg-muted" /> : null}
        {d.page.icon ? <span aria-hidden>{d.page.icon}</span> : null}
        {docsTitle(d.page.title)}
      </Link>
      {linked && views.length === 0 ? (
        canEditViews ? (
          <Skeleton className="h-32 w-full rounded-md" />
        ) : (
          <p className="type-label text-fg-muted">This linked database has no views.</p>
        )
      ) : (
        <DatabaseView db={d.page} views={views} orgId={d.page.org_id} editable={editable} inline {...(linked && pageId && blockId ? { linked: { pageId, blockId, canEdit: canEditViews } } : {})} />
      )}
    </div>
  );
}

function NoAccess() {
  return <p className="rounded-md border border-dashed border-border px-3 py-4 type-label text-fg-muted">You can't open this database.</p>;
}

export interface DatabaseViewProps {
  db: DocsPage;
  views: DocsView[];
  orgId: string;
  editable: boolean;
  inline?: boolean;
  /** Public link token: read-only rows through the link. */
  token?: string | null;
  onOpenRow?: (id: string) => void;
  /** A linked view: its views belong to the block on the host page (edit there to change them). */
  linked?: { pageId: string; blockId: string; canEdit: boolean };
}

type Panel = { kind: "filter" | "sort" | "new-view" | "view-menu"; anchor: HTMLElement };

/** Views (tabs) with their filters, sorts, groups and settings, per-view search, and the view's body. */
export function DatabaseView({ db, views, orgId, editable, inline, token, onOpenRow, linked }: DatabaseViewProps) {
  const router = useRouter();
  const qc = useQueryClient();
  const [viewId, setViewId] = useState<string | null>(null);
  useEffect(() => {
    const saved = recallView(db.id);
    if (saved && views.some((v) => v.id === saved)) setViewId(saved);
  }, [db.id, views]);
  const view = views.find((v) => v.id === viewId) ?? views[0];
  const viewsEditable = !token && (linked ? linked.canEdit : editable);
  const [draft, setDraft] = useState<DocsViewConfig | null>(null);
  const [pending, setPending] = useState<{ id: string; cfg: DocsViewConfig } | null>(null);
  useEffect(() => setDraft(null), [view?.id]);
  useEffect(() => setPending(null), [view?.config]);
  const config: DocsViewConfig = draft ?? (pending && pending.id === view?.id ? pending.cfg : null) ?? view?.config ?? EMPTY_CONFIG;
  const [searches, setSearches] = useState<Record<string, string>>({});
  const search = view ? (searches[view.id] ?? "") : "";
  const [searchOpen, setSearchOpen] = useState(false);
  const [panel, setPanel] = useState<Panel | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [settings, setSettings] = useState<{ section?: ViewSettingsSection } | null>(null);
  const [propSheet, setPropSheet] = useState<{ prop: DocsProperty; isNew: boolean } | null>(null);
  const [deleting, setDeleting] = useState<DocsProperty | null>(null);
  const [deletingView, setDeletingView] = useState<DocsView | null>(null);
  const [sheet, setSheet] = useState<"automations" | "import" | "export" | null>(null);
  const [autoEdit, setAutoEdit] = useState<string | null>(null);
  const filterBtn = useRef<HTMLButtonElement>(null);
  const schema = db.schema ?? [];
  const type = view?.type ?? "table";
  const noRows = type === "chart" || type === "form";
  const viewKey = view?.id;
  const limit = pageSize(type, config);
  const params = useMemo<DocsRowsParams>(
    () => ({
      ...(viewKey ? { view: viewKey } : {}),
      tz: docsTimeZone(),
      limit,
      ...(search.trim() ? { q: search.trim() } : {}),
      ...(draft ? { filters: draft.filters, ...(draft.filter_conj ? { filter_conj: draft.filter_conj } : {}), sorts: draft.sorts } : {}),
    }),
    [viewKey, limit, search, draft],
  );
  const rows = useDocsRows(db.id, params, { token: token ?? null, enabled: !!view && !noRows });
  const setValues = useDocsSetRowValues(db.id);
  const createRow = useDocsCreateRow(db.id);
  const putSchema = useDocsPutSchema(db.id);
  const updateView = useDocsUpdateView(db.id);
  const createView = useDocsCreateView(db.id);
  const createLinked = useDocsCreateLinkedView(linked?.pageId ?? "", linked?.blockId ?? "");
  const deleteView = useDocsDeleteView(db.id);
  const trash = useDocsTrashPage(orgId);
  const duplicate = useDocsDuplicatePage(orgId);
  const press = useDocsPressButton(db.id);

  const onErr = (fallback: string) => (e: unknown) => toast.error(errorMessage(e, fallback));
  const afterLinked = () => {
    if (linked) void qc.invalidateQueries({ queryKey: docsDatabaseKeys.blockDatabase(linked.pageId, linked.blockId) });
  };
  const addView = (t: DocsViewType, name: string = DOCS_VIEW_LABEL[t], config?: DocsViewConfig) => {
    const done = {
      onSuccess: (v: DocsView) => {
        setViewId(v.id);
        rememberView(db.id, v.id);
      },
      onError: onErr("Couldn't add the view."),
    };
    if (linked) createLinked.mutate({ database_id: db.id, type: t, name, ...(config ? { config } : {}) }, done);
    else createView.mutate({ type: t, name, ...(config ? { config } : {}) }, done);
  };

  if (!view) {
    return (
      <div className="flex items-center gap-2 type-label text-fg-muted">
        This database has no views.
        {viewsEditable ? (
          <Button size="sm" variant="ghost" leftIcon={<Plus />} onClick={() => addView("table")}>
            Add a table
          </Button>
        ) : null}
      </div>
    );
  }

  const saveView = (patch: { name?: string; type?: DocsViewType; config?: DocsViewConfig }, fallback = "Couldn't save the view.") => {
    if (patch.config && linked) setPending({ id: view.id, cfg: patch.config });
    updateView.mutate(
      { viewId: view.id, ...patch },
      {
        onSuccess: afterLinked,
        onError: (e) => {
          setPending(null);
          onErr(fallback)(e);
        },
      },
    );
  };
  const saveConfig = (cfg: DocsViewConfig) => {
    if (!viewsEditable) return setDraft(cfg);
    setDraft(null);
    saveView({ config: cfg });
  };
  const openRow = (id: string) => (onOpenRow ? onOpenRow(id) : router.push(token ? `/p/${token}/${id}` : `/docs/${id}`));
  const cardLike = type === "board" || type === "gallery" || type === "calendar" || type === "feed";
  const create = (values?: Record<string, DocsValue>, title?: string, templateId?: string | null) =>
    createRow.mutate(
      { ...(values ? { values } : {}), ...(title ? { title } : {}), ...(templateId ? { template_id: templateId } : templateId === null ? {} : { view_id: view.id }) },
      {
        onSuccess: (p) => {
          if (cardLike || templateId) openRow(p.id);
          else setAutoEdit(p.id);
        },
        onError: onErr("Couldn't add a row."),
      },
    );
  const addFilter = (p: DocsProperty) => {
    saveConfig({ ...config, filters: [...config.filters, docsDefaultFilter(p)] });
    if (filterBtn.current) setPanel({ kind: "filter", anchor: filterBtn.current });
  };

  const viewProps: ViewProps = {
    db,
    schema,
    view,
    config,
    rows: rows.data?.rows ?? [],
    refs: rows.data?.refs ?? { users: {}, pages: {} },
    loading: rows.isPending && !noRows,
    hasMore: !!rows.hasNextPage,
    loadMore: () => void rows.fetchNextPage(),
    editable: editable && !token,
    orgId,
    publicView: !!token,
    token: token ?? null,
    meta: { total: rows.data?.total, groups: rows.data?.groups, sub_groups: rows.data?.sub_groups, calculations: rows.data?.calculations },
    configEditable: viewsEditable,
    autoEditRow: autoEdit,
    onOpenRow: openRow,
    onSetValue: (row: DocsRow, propId: string, value: DocsValue) => setValues.mutate({ rowId: row.id, values: { [propId]: value } }, { onError: onErr("Couldn't save that value.") }),
    onSetTitle: (row, title) => setValues.mutate({ rowId: row.id, title }, { onError: onErr("Couldn't rename the row.") }),
    onCreateRow: (values, title) => create(values, title),
    onCreateSubRow: (parent) => createRow.mutate({ parent_row: parent.id, view_id: view.id }, { onSuccess: (p) => setAutoEdit(p.id), onError: onErr("Couldn't add a sub-item.") }),
    onMoveRow: (row, change) => setValues.mutate({ rowId: row.id, ...change }, { onError: onErr("Couldn't move the row.") }),
    onSchemaChange: (next: DocsProperty[]) => putSchema.mutate(next, { onError: onErr("Couldn't change the properties.") }),
    onConfigChange: saveConfig,
    onTrashRow: (row) => trash.mutate({ pageId: row.id, databaseId: db.id }, { onSuccess: () => toast.success("Moved to the trash."), onError: onErr("Couldn't move it to the trash.") }),
    onDuplicateRow: (row) => duplicate.mutate(row.id, { onError: onErr("Couldn't duplicate the row.") }),
    onPressButton: (row, propId) =>
      press.mutate(
        { rowId: row.id, propId },
        {
          onSuccess: (res) => {
            for (const u of res.open) window.open(u, "_blank", "noopener,noreferrer");
            if (res.created.length) toast.success(res.created.length === 1 ? "Added a row." : `Added ${res.created.length} rows.`);
          },
          onError: onErr("The button didn't finish."),
        },
      ),
    onEditProperty: (p, isNew) => setPropSheet({ prop: p, isNew: !!isNew }),
    onAddFilter: addFilter,
    onOpenSettings: (section) => setSettings(section ? { section } : {}),
    onDeleteProperty: (p) => setDeleting(p),
  };

  const filterCount = docsFilterCount(config.filters);
  const sortCount = config.sorts.length;
  const Body = BODY[view.type];
  const canEditRows = editable && !token;

  return (
    <div className={cn("flex flex-col gap-3", inline && "rounded-md")}>
      <div className="flex flex-col gap-2 border-b border-border pb-2 sm:flex-row sm:items-center">
        <div role="tablist" aria-label="Views" className="scrollbar-none flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          {views.map((v) => {
            const Icon = VIEW_ICON[v.type];
            const active = v.id === view.id;
            return renaming === v.id ? (
              <form
                key={v.id}
                onSubmit={(e) => {
                  e.preventDefault();
                  const name = (new FormData(e.currentTarget).get("name") as string).trim();
                  setRenaming(null);
                  if (name && name !== v.name) updateView.mutate({ viewId: v.id, name }, { onSuccess: afterLinked, onError: onErr("Couldn't rename the view.") });
                }}
              >
                <Input name="name" defaultValue={v.name} aria-label="View name" autoFocus maxLength={100} className="h-8 w-36" onBlur={(e) => e.currentTarget.form?.requestSubmit()} />
              </form>
            ) : (
              <button
                key={v.id}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={(e) => {
                  if (active && viewsEditable) setPanel({ kind: "view-menu", anchor: e.currentTarget });
                  setViewId(v.id);
                  rememberView(db.id, v.id);
                  setSearchOpen(!!searches[v.id]);
                }}
                className={cn(
                  "flex min-h-8 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-sm px-2 type-label focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring pointer-coarse:min-h-tap",
                  active ? "bg-sunken font-semibold text-fg" : "text-fg-muted hover:bg-sunken hover:text-fg",
                )}
              >
                <Icon aria-hidden className="size-4" />
                {v.name}
              </button>
            );
          })}
          {viewsEditable ? <IconButton label="Add a view" icon={<Plus />} size="sm" variant="ghost" onClick={(e) => setPanel({ kind: "new-view", anchor: e.currentTarget })} /> : null}
        </div>
        <div className="flex flex-wrap items-center justify-end gap-1">
          {noRows ? null : searchOpen ? (
            <div className="flex items-center gap-1">
              <Input value={search} onChange={(e) => setSearches((s) => ({ ...s, [view.id]: e.target.value }))} placeholder="Search rows" aria-label="Search rows" autoFocus className="h-8 w-40" />
              <IconButton
                label="Close search"
                icon={<X />}
                size="sm"
                variant="ghost"
                onClick={() => {
                  setSearches((s) => ({ ...s, [view.id]: "" }));
                  setSearchOpen(false);
                }}
              />
            </div>
          ) : (
            <IconButton label="Search rows" icon={<Search />} size="sm" variant="ghost" onClick={() => setSearchOpen(true)} />
          )}
          {view.type !== "form" ? (
            <Button ref={filterBtn} variant="ghost" size="sm" leftIcon={<Filter />} onClick={(e) => setPanel({ kind: "filter", anchor: e.currentTarget })} aria-label={filterCount ? `Filter, ${filterCount} active` : "Filter"}>
              <span className="hidden sm:inline">Filter</span>
              {filterCount ? (
                <Badge tone="primary" size="sm">
                  {filterCount}
                </Badge>
              ) : null}
            </Button>
          ) : null}
          {!noRows ? (
            <Button variant="ghost" size="sm" leftIcon={<ArrowUpDown />} onClick={(e) => setPanel({ kind: "sort", anchor: e.currentTarget })} aria-label={sortCount ? `Sort, ${sortCount} active` : "Sort"}>
              <span className="hidden sm:inline">Sort</span>
              {sortCount ? (
                <Badge tone="primary" size="sm">
                  {sortCount}
                </Badge>
              ) : null}
            </Button>
          ) : null}
          {view.type !== "form" ? <IconButton label="View settings" icon={<SlidersHorizontal />} size="sm" variant="ghost" onClick={() => setSettings({})} /> : null}
          {!token ? (
            <ResourceMenu
              feature="docs"
              title={docsTitle(db.title)}
              label="Database actions"
              actions={[
                ...(canEditRows ? [{ id: "automations", label: "Automations", icon: Workflow, onSelect: () => setSheet("automations") }] : []),
                ...(canEditRows ? [{ id: "import", label: "Import CSV…", icon: Import, onSelect: () => setSheet("import") }] : []),
                { id: "export", label: "Export CSV…", icon: Download, onSelect: () => setSheet("export") },
              ]}
            />
          ) : null}
          {canEditRows && view.type !== "chart" && view.type !== "form" ? (
            <NewWithTemplates dbId={db.id} orgId={orgId} view={view} config={config} canEditView={viewsEditable} creating={createRow.isPending} onCreate={(t) => create(undefined, undefined, t)} onConfig={saveConfig} />
          ) : null}
        </div>
      </div>
      {draft && !viewsEditable ? (
        <div className="flex items-center gap-2 type-caption text-fg-muted">
          Your changes to this view aren't saved.
          <Button size="sm" variant="ghost" onClick={() => setDraft(null)}>
            Reset
          </Button>
        </div>
      ) : null}
      {rows.isError ? <ErrorState title="Couldn't load the rows." onRetry={() => void rows.refetch()} /> : <Body {...viewProps} />}

      <Floating open={panel?.kind === "filter"} anchor={panel?.anchor ?? null} onClose={() => setPanel(null)} autoFocus placement="bottom-end" label="Filters">
        <FilterPanel schema={schema} config={config} refs={viewProps.refs} onChange={(filters, conj) => saveConfig({ ...config, filters, ...(conj ? { filter_conj: conj } : {}) })} />
      </Floating>
      <Floating open={panel?.kind === "sort"} anchor={panel?.anchor ?? null} onClose={() => setPanel(null)} autoFocus placement="bottom-end" label="Sorts">
        <SortPanel schema={schema} config={config} onChange={(sorts) => saveConfig({ ...config, sorts })} />
      </Floating>
      <Floating open={panel?.kind === "new-view"} anchor={panel?.anchor ?? null} onClose={() => setPanel(null)} autoFocus label="New view">
        <FloatingMenu
          onDone={() => setPanel(null)}
          items={DOCS_VIEW_TYPES.map((t) => {
            const Icon = VIEW_ICON[t];
            return { id: t, label: DOCS_VIEW_LABEL[t], icon: <Icon />, onSelect: () => addView(t) };
          })}
        />
      </Floating>
      <Floating open={panel?.kind === "view-menu"} anchor={panel?.anchor ?? null} onClose={() => setPanel(null)} autoFocus label="View actions">
        <FloatingMenu
          onDone={() => setPanel(null)}
          items={[
            { id: "rename", label: "Rename", icon: <Pencil />, onSelect: () => setRenaming(view.id) },
            { id: "settings", label: "Edit view", icon: <Settings2 />, onSelect: () => setSettings({}) },
            { id: "dup", label: "Duplicate view", icon: <Copy />, onSelect: () => addView(view.type, `${view.name} (copy)`, view.config) },
            "separator" as const,
            { id: "delete", label: "Delete view", icon: <Trash2 />, destructive: true, disabled: views.length < 2, onSelect: () => setDeletingView(view) },
          ]}
        />
      </Floating>

      {settings ? (
        <ViewSettingsSheet
          open
          onOpenChange={(o) => !o && setSettings(null)}
          db={db}
          view={view}
          config={config}
          section={settings.section}
          onConfig={saveConfig}
          onType={viewsEditable ? (t) => saveView({ type: t }, "Couldn't change the view.") : undefined}
          canSave={viewsEditable}
          schemaEditable={canEditRows}
        />
      ) : null}
      {propSheet ? <PropertySheet open onOpenChange={(o) => !o && setPropSheet(null)} dbId={db.id} orgId={orgId} schema={schema} prop={propSheet.prop} isNew={propSheet.isNew} /> : null}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete “${deleting?.name ?? ""}”?`}
        confirmLabel="Delete property"
        tone="danger"
        onConfirm={() => {
          const p = deleting;
          setDeleting(null);
          if (p) putSchema.mutate(schema.filter((x) => x.id !== p.id), { onError: onErr("Couldn't delete the property.") });
        }}
      >
        Every row loses its {deleting?.name} value. It can't be undone.
      </ConfirmDialog>
      <ConfirmDialog
        open={!!deletingView}
        onOpenChange={(o) => !o && setDeletingView(null)}
        title={`Delete the view “${deletingView?.name ?? ""}”?`}
        confirmLabel="Delete view"
        tone="danger"
        onConfirm={() => {
          const v = deletingView;
          setDeletingView(null);
          if (v)
            deleteView.mutate(v.id, {
              onSuccess: () => {
                setViewId(null);
                afterLinked();
              },
              onError: onErr("Couldn't delete the view."),
            });
        }}
      >
        Rows stay in the database.
      </ConfirmDialog>
      {sheet === "automations" ? <AutomationsSheet open onOpenChange={(o) => !o && setSheet(null)} db={db} orgId={orgId} /> : null}
      {sheet === "import" ? <CsvImportDialog db={db} onClose={() => setSheet(null)} /> : null}
      {sheet === "export" ? <CsvExportDialog db={db} view={view} onClose={() => setSheet(null)} /> : null}
    </div>
  );
}
