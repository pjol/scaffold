"use client";

import {
  docsTitle,
  errorMessage,
  useDocsCreateRow,
  useDocsCreateView,
  useDocsDeleteView,
  useDocsDuplicatePage,
  useDocsPage,
  useDocsPutSchema,
  useDocsRows,
  useDocsSetRowValues,
  useDocsTrashPage,
  useDocsUpdateView,
  type DocsPage,
  type DocsProperty,
  type DocsRow,
  type DocsValue,
  type DocsView,
  type DocsViewConfig,
  type DocsViewType,
} from "@scaffoldapp/shared";
import { Badge, Button, ErrorState, IconButton, Input, Skeleton, cn, toast } from "@scaffoldapp/ui-web";
import { ArrowUpDown, CalendarDays, Columns3, Filter, LayoutList, Pencil, Plus, Search, Table2, Trash2, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { Floating, FloatingMenu } from "../Floating";
import { FilterPanel, SortPanel } from "./panels";
import { BoardView, CalendarView, ListView, TableView, type ViewProps } from "./views";

const VIEW_ICON: Record<DocsViewType, typeof Table2> = { table: Table2, board: Columns3, list: LayoutList, calendar: CalendarDays };
const VIEW_LABEL: Record<DocsViewType, string> = { table: "Table", board: "Board", list: "List", calendar: "Calendar" };

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

/** An inline database block: its own title links to the full page. */
export function InlineDatabase({ databaseId }: { databaseId: string }) {
  const detail = useDocsPage(databaseId);
  if (detail.isPending) return <Skeleton className="h-40 w-full rounded-md" />;
  if (detail.isError) {
    return detail.error.status === 404 ? (
      <p className="rounded-md border border-dashed border-border px-3 py-4 type-label text-fg-muted">You can't open this database.</p>
    ) : (
      <ErrorState title="Couldn't load this database." onRetry={() => void detail.refetch()} />
    );
  }
  const d = detail.data;
  const editable = (d.page.my_level === "edit" || d.page.my_level === "manage") && !d.page.locked && !d.page.deleted_at;
  return (
    <div data-database className="flex flex-col gap-2">
      <Link href={`/docs/${d.page.id}`} className="flex w-fit items-center gap-2 rounded-sm type-title-sm text-fg hover:underline focus-visible:outline-2 focus-visible:outline-ring">
        {d.page.icon ? <span aria-hidden>{d.page.icon}</span> : null}
        {docsTitle(d.page.title)}
      </Link>
      <DatabaseView db={d.page} views={d.views} orgId={d.page.org_id} editable={editable} inline />
    </div>
  );
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
}

/** Views (tabs), filters and sorts saved per view, search, and the table, board, list or calendar. */
export function DatabaseView({ db, views, orgId, editable, inline, token, onOpenRow }: DatabaseViewProps) {
  const router = useRouter();
  const [viewId, setViewId] = useState<string | null>(null);
  useEffect(() => {
    const saved = recallView(db.id);
    if (saved && views.some((v) => v.id === saved)) setViewId(saved);
  }, [db.id, views]);
  const view = views.find((v) => v.id === viewId) ?? views[0];
  const [draft, setDraft] = useState<DocsViewConfig | null>(null);
  useEffect(() => setDraft(null), [view?.id]);
  const config = draft ?? view?.config ?? { filters: [], sorts: [], columns: [], card_properties: [] };
  const [search, setSearch] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [panel, setPanel] = useState<{ kind: "filter" | "sort" | "new-view" | "view-menu"; anchor: HTMLElement } | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const schema = db.schema ?? [];
  const big = view?.type === "board" || view?.type === "calendar";
  const params = useMemo(() => ({ filters: config.filters, sorts: config.sorts, ...(search.trim() ? { q: search.trim() } : {}), limit: big ? 1000 : 100 }), [config.filters, config.sorts, search, big]);
  const rows = useDocsRows(db.id, params, { token: token ?? null, enabled: !!view });
  const setValues = useDocsSetRowValues(db.id);
  const createRow = useDocsCreateRow(db.id);
  const putSchema = useDocsPutSchema(db.id);
  const updateView = useDocsUpdateView(db.id);
  const createView = useDocsCreateView(db.id);
  const deleteView = useDocsDeleteView(db.id);
  const trash = useDocsTrashPage(orgId);
  const duplicate = useDocsDuplicatePage(orgId);

  if (!view) return <p className="type-label text-fg-muted">This database has no views.</p>;

  const saveConfig = (cfg: DocsViewConfig) => {
    if (!editable || token) return setDraft(cfg);
    setDraft(null);
    updateView.mutate({ viewId: view.id, config: cfg }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save the view.")) });
  };
  const onErr = (fallback: string) => (e: unknown) => toast.error(errorMessage(e, fallback));
  const openRow = (id: string) => (onOpenRow ? onOpenRow(id) : router.push(token ? `/p/${token}/${id}` : `/docs/${id}`));

  const viewProps: ViewProps = {
    db,
    schema,
    view,
    config,
    rows: rows.data?.rows ?? [],
    refs: rows.data?.refs ?? { users: {}, pages: {} },
    loading: rows.isPending,
    hasMore: !!rows.hasNextPage,
    loadMore: () => void rows.fetchNextPage(),
    editable: editable && !token,
    orgId,
    publicView: !!token,
    onOpenRow: openRow,
    onSetValue: (row: DocsRow, propId: string, value: DocsValue) => setValues.mutate({ rowId: row.id, values: { [propId]: value } }, { onError: onErr("Couldn't save that value.") }),
    onSetTitle: (row, title) => setValues.mutate({ rowId: row.id, title }, { onError: onErr("Couldn't rename the row.") }),
    onCreateRow: (values, title) =>
      createRow.mutate({ ...(values ? { values } : {}), ...(title ? { title } : {}) }, { onSuccess: (p) => openRow(p.id), onError: onErr("Couldn't add a row.") }),
    onMoveRow: (row, change) => setValues.mutate({ rowId: row.id, ...change }, { onError: onErr("Couldn't move the row.") }),
    onSchemaChange: (next: DocsProperty[]) => putSchema.mutate(next, { onError: onErr("Couldn't change the properties.") }),
    onConfigChange: saveConfig,
    onTrashRow: (row) => trash.mutate({ pageId: row.id, databaseId: db.id }, { onSuccess: () => toast.success("Moved to the trash."), onError: onErr("Couldn't move it to the trash.") }),
    onDuplicateRow: (row) => duplicate.mutate(row.id, { onError: onErr("Couldn't duplicate the row.") }),
  };

  const filterCount = config.filters.length;
  const sortCount = config.sorts.length;
  const Body = { table: TableView, board: BoardView, list: ListView, calendar: CalendarView }[view.type];

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
                  if (name && name !== v.name) updateView.mutate({ viewId: v.id, name }, { onError: onErr("Couldn't rename the view.") });
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
                  if (active && editable && !token) setPanel({ kind: "view-menu", anchor: e.currentTarget });
                  setViewId(v.id);
                  rememberView(db.id, v.id);
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
          {editable && !token ? <IconButton label="Add a view" icon={<Plus />} size="sm" variant="ghost" onClick={(e) => setPanel({ kind: "new-view", anchor: e.currentTarget })} /> : null}
        </div>
        <div className="flex items-center justify-end gap-1">
          {searchOpen ? (
            <div className="flex items-center gap-1">
              <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search rows" aria-label="Search rows" autoFocus className="h-8 w-40" />
              <IconButton
                label="Close search"
                icon={<X />}
                size="sm"
                variant="ghost"
                onClick={() => {
                  setSearch("");
                  setSearchOpen(false);
                }}
              />
            </div>
          ) : (
            <IconButton label="Search rows" icon={<Search />} size="sm" variant="ghost" onClick={() => setSearchOpen(true)} />
          )}
          <Button variant="ghost" size="sm" leftIcon={<Filter />} onClick={(e) => setPanel({ kind: "filter", anchor: e.currentTarget })} aria-label={filterCount ? `Filter, ${filterCount} active` : "Filter"}>
            <span className="hidden sm:inline">Filter</span>
            {filterCount ? (
              <Badge tone="primary" size="sm">
                {filterCount}
              </Badge>
            ) : null}
          </Button>
          <Button variant="ghost" size="sm" leftIcon={<ArrowUpDown />} onClick={(e) => setPanel({ kind: "sort", anchor: e.currentTarget })} aria-label={sortCount ? `Sort, ${sortCount} active` : "Sort"}>
            <span className="hidden sm:inline">Sort</span>
            {sortCount ? (
              <Badge tone="primary" size="sm">
                {sortCount}
              </Badge>
            ) : null}
          </Button>
          {editable && !token ? (
            <Button size="sm" leftIcon={<Plus />} loading={createRow.isPending} onClick={() => viewProps.onCreateRow()}>
              New
            </Button>
          ) : null}
        </div>
      </div>
      {draft && editable && !token ? (
        <div className="flex items-center gap-2 type-caption text-fg-muted">
          These filters aren't saved.
          <Button size="sm" variant="ghost" onClick={() => saveConfig(draft)}>
            Save for everyone
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setDraft(null)}>
            Reset
          </Button>
        </div>
      ) : null}
      {rows.isError ? <ErrorState title="Couldn't load the rows." onRetry={() => void rows.refetch()} /> : <Body {...viewProps} />}

      <Floating open={panel?.kind === "filter"} anchor={panel?.anchor ?? null} onClose={() => setPanel(null)} autoFocus placement="bottom-end" label="Filters">
        <FilterPanel schema={schema} config={config} refs={viewProps.refs} onChange={(filters) => saveConfig({ ...config, filters })} />
      </Floating>
      <Floating open={panel?.kind === "sort"} anchor={panel?.anchor ?? null} onClose={() => setPanel(null)} autoFocus placement="bottom-end" label="Sorts">
        <SortPanel schema={schema} config={config} onChange={(sorts) => saveConfig({ ...config, sorts })} />
      </Floating>
      <Floating open={panel?.kind === "new-view"} anchor={panel?.anchor ?? null} onClose={() => setPanel(null)} autoFocus label="New view">
        <FloatingMenu
          onDone={() => setPanel(null)}
          items={(["table", "board", "list", "calendar"] as DocsViewType[]).map((t) => {
            const Icon = VIEW_ICON[t];
            return {
              id: t,
              label: VIEW_LABEL[t],
              icon: <Icon />,
              onSelect: () =>
                createView.mutate(
                  { type: t, name: VIEW_LABEL[t] },
                  {
                    onSuccess: (v) => {
                      setViewId(v.id);
                      rememberView(db.id, v.id);
                    },
                    onError: onErr("Couldn't add the view."),
                  },
                ),
            };
          })}
        />
      </Floating>
      <Floating open={panel?.kind === "view-menu"} anchor={panel?.anchor ?? null} onClose={() => setPanel(null)} autoFocus label="View actions">
        <FloatingMenu
          onDone={() => setPanel(null)}
          items={[
            { id: "rename", label: "Rename", icon: <Pencil />, onSelect: () => setRenaming(view.id) },
            ...(["table", "board", "list", "calendar"] as DocsViewType[])
              .filter((t) => t !== view.type)
              .map((t) => {
                const Icon = VIEW_ICON[t];
                return { id: `as-${t}`, label: `Show as ${VIEW_LABEL[t].toLowerCase()}`, icon: <Icon />, onSelect: () => updateView.mutate({ viewId: view.id, type: t }, { onError: onErr("Couldn't change the view.") }) };
              }),
            "separator" as const,
            {
              id: "delete",
              label: "Delete view",
              icon: <Trash2 />,
              destructive: true,
              disabled: views.length < 2,
              onSelect: () => deleteView.mutate(view.id, { onSuccess: () => setViewId(null), onError: onErr("Couldn't delete the view.") }),
            },
          ]}
        />
      </Floating>
    </div>
  );
}
