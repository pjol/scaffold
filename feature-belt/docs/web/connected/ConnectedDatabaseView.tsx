"use client";

import {
  docsConnectedHref,
  errorMessage,
  useDocsConnectedCreateRow,
  useDocsConnectedRows,
  useDocsConnectedSetRow,
  type DocsPage,
  type DocsRow,
  type DocsValue,
  type DocsView,
  type DocsViewConfig,
  type DocsViewType,
} from "@scaffoldapp/shared";
import { Badge, Button, ErrorState, IconButton, Input, cn, toast } from "@scaffoldapp/ui-web";
import { ArrowUpDown, CalendarDays, Columns3, Filter, LayoutList, Plus, Search, Table2, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { Floating } from "../Floating";
import { FilterPanel, SortPanel } from "../database/panels";
import { BoardView, CalendarView, ListView, TableView, type ViewProps } from "../database/views";

const VIEW_ICON: Record<DocsViewType, typeof Table2> = { table: Table2, board: Columns3, list: LayoutList, calendar: CalendarDays };

function recall(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function remember(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // Not remembered in private mode.
  }
}

export interface ConnectedDatabaseViewProps {
  connectionId: string;
  dbRef: string;
  db: DocsPage;
  views: DocsView[];
  orgId: string;
  editable: boolean;
  platform: string;
}

/**
 * A connected database (Notion) in Docs' own table, board, list and calendar views. Views,
 * filters and sorts are this viewer's (kept in the browser); rows, values, new rows and
 * board moves are read and written on the platform. Properties stay as the platform has
 * them (the ones Docs can't edit are read-only).
 */
export function ConnectedDatabaseView({ connectionId, dbRef, db, views, orgId, editable, platform }: ConnectedDatabaseViewProps) {
  const router = useRouter();
  const viewKey = `scaffoldapp.docs.connected.view.${connectionId}.${dbRef}`;
  const [viewId, setViewId] = useState<string | null>(null);
  useEffect(() => {
    const saved = recall(viewKey);
    if (saved && views.some((v) => v.id === saved)) setViewId(saved);
  }, [viewKey, views]);
  const view = views.find((v) => v.id === viewId) ?? views[0];
  const [configs, setConfigs] = useState<Record<string, DocsViewConfig>>({});
  const config = (view && configs[view.id]) ?? view?.config ?? { filters: [], sorts: [], columns: [], card_properties: [] };
  const [search, setSearch] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [panel, setPanel] = useState<{ kind: "filter" | "sort"; anchor: HTMLElement } | null>(null);
  const schema = db.schema ?? [];
  const params = useMemo(() => ({ filters: config.filters, sorts: config.sorts, ...(search.trim() ? { q: search.trim() } : {}), limit: 100 }), [config.filters, config.sorts, search]);
  const rows = useDocsConnectedRows(connectionId, dbRef, params, !!view);
  const setRow = useDocsConnectedSetRow(connectionId, dbRef);
  const createRow = useDocsConnectedCreateRow(connectionId, dbRef);
  if (!view) return <p className="type-label text-fg-muted">This database has no views.</p>;

  const onErr = (fallback: string) => (e: unknown) => toast.error(errorMessage(e, fallback));
  const refOf = (id: string) => rows.data?.rows.find((r) => r.id === id)?.ref ?? id;
  const openRow = (id: string) => router.push(docsConnectedHref(connectionId, refOf(id)));
  const saveConfig = (cfg: DocsViewConfig) => setConfigs((c) => ({ ...c, [view.id]: cfg }));
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
    editable,
    orgId,
    publicView: false,
    schemaEditable: false,
    external: { platform, connectionId },
    onOpenRow: openRow,
    onSetValue: (row: DocsRow, propId: string, value: DocsValue) =>
      setRow.mutate({ rowRef: row.ref ?? row.id, rowId: row.id, values: { [propId]: value } }, { onError: onErr(`Couldn't save that value in ${platform}.`) }),
    onSetTitle: (row, title) => setRow.mutate({ rowRef: row.ref ?? row.id, rowId: row.id, title }, { onError: onErr(`Couldn't rename it in ${platform}.`) }),
    onCreateRow: (values, title) =>
      createRow.mutate(
        { ...(values ? { values } : {}), ...(title ? { title } : {}) },
        { onSuccess: (r) => router.push(docsConnectedHref(connectionId, r.ref ?? r.id)), onError: onErr(`Couldn't add a row in ${platform}.`) },
      ),
    onMoveRow: (row, change) => {
      // Board moves change the grouped value; the platform keeps its own order.
      if (change.values) setRow.mutate({ rowRef: row.ref ?? row.id, rowId: row.id, values: change.values }, { onError: onErr(`Couldn't move it in ${platform}.`) });
    },
    onSchemaChange: () => toast.error(`Change properties in ${platform}.`),
    onConfigChange: saveConfig,
    onTrashRow: () => toast.error(`Delete rows in ${platform}.`),
    onDuplicateRow: () => toast.error(`Duplicate rows in ${platform}.`),
  };
  const Body = { table: TableView, board: BoardView, list: ListView, calendar: CalendarView }[view.type];
  const filterCount = config.filters.length;
  const sortCount = config.sorts.length;
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-2 border-b border-border pb-2 sm:flex-row sm:items-center">
        <div role="tablist" aria-label="Views" className="scrollbar-none flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          {views.map((v) => {
            const Icon = VIEW_ICON[v.type];
            const active = v.id === view.id;
            return (
              <button
                key={v.id}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => {
                  setViewId(v.id);
                  remember(viewKey, v.id);
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
        </div>
        <div className="flex items-center justify-end gap-1">
          {searchOpen ? (
            <div className="flex items-center gap-1">
              <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search titles" aria-label="Search titles" autoFocus className="h-8 w-40" />
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
            <IconButton label="Search titles" icon={<Search />} size="sm" variant="ghost" onClick={() => setSearchOpen(true)} />
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
          {editable ? (
            <Button size="sm" leftIcon={<Plus />} loading={createRow.isPending} onClick={() => viewProps.onCreateRow()}>
              New
            </Button>
          ) : null}
        </div>
      </div>
      {rows.isError ? <ErrorState title={`Couldn't load the rows from ${platform}.`} onRetry={() => void rows.refetch()} /> : <Body {...viewProps} />}
      <Floating open={panel?.kind === "filter"} anchor={panel?.anchor ?? null} onClose={() => setPanel(null)} autoFocus placement="bottom-end" label="Filters">
        <FilterPanel schema={schema.filter((p) => !p.read_only)} config={config} refs={viewProps.refs} onChange={(filters) => saveConfig({ ...config, filters })} />
      </Floating>
      <Floating open={panel?.kind === "sort"} anchor={panel?.anchor ?? null} onClose={() => setPanel(null)} autoFocus placement="bottom-end" label="Sorts">
        <SortPanel schema={schema.filter((p) => !p.read_only)} config={config} onChange={(sorts) => saveConfig({ ...config, sorts })} />
      </Floating>
    </div>
  );
}
