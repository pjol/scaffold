"use client";

import {
  CRM_TYPE_LABEL,
  crmFieldText,
  crmMoney,
  crmTimeZone,
  crmTotal,
  errorMessage,
  useCrmBulk,
  useCrmCreateView,
  useCrmDeleteView,
  useCrmExport,
  useCrmPipelines,
  useCrmProperties,
  useCrmRecords,
  useCrmTags,
  useCrmUpdateView,
  useCrmViews,
  type CrmFilter,
  type CrmObjectType,
  type CrmProperty,
  type CrmRecord,
  type CrmSortDir,
  type CrmView,
} from "@scaffoldapp/shared";
import {
  Badge,
  Button,
  Card,
  Checkbox,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  IconButton,
  Input,
  Menu,
  MenuItem,
  MenuLabel,
  MenuSeparator,
  Skeleton,
  SkeletonGroup,
  cn,
  toast,
} from "@scaffoldapp/ui-web";
import {
  ArrowDown,
  ArrowUp,
  Columns3,
  Download,
  Filter,
  KanbanSquare,
  MoreHorizontal,
  Pencil,
  Plus,
  Save,
  Search,
  Tag as TagIcon,
  Trash2,
  UserRound,
  X,
} from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { CrmFrame, useCrm } from "./CrmFrame";
import { CreateRecordDialog } from "./CreateRecordDialog";
import { ColumnDialog, DEFAULT_COLUMNS, FilterDialog, SaveViewDialog, filterText } from "./ListParts";
import { MemberPicker, MoneyText, PersonChip, RecordLink, StageBadge, TYPE_ICON, TagList, dayLabel, recordHref } from "./parts";

/** /crm/contacts, /crm/companies, /crm/deals: views, search, filters, columns, sort, bulk actions, export. */
export function CrmListScreen({ type }: { type: CrmObjectType }) {
  const [creating, setCreating] = useState(false);
  return (
    <CrmFrame
      actions={
        <>
          {type === "deal" ? (
            <Button asChild variant="secondary" size="auto" leftIcon={<KanbanSquare />}>
              <Link href="/crm/deals/board">Board</Link>
            </Button>
          ) : null}
          <Button size="auto" leftIcon={<Plus />} onClick={() => setCreating(true)}>
            New {CRM_TYPE_LABEL[type].one}
          </Button>
        </>
      }
    >
      <RecordList type={type} />
      <CreateRecordDialog type={type} open={creating} onOpenChange={setCreating} />
    </CrmFrame>
  );
}

function parseFilters(raw: string): CrmFilter[] {
  if (!raw) return [];
  try {
    const v = JSON.parse(raw) as unknown;
    return Array.isArray(v) ? (v as CrmFilter[]) : [];
  } catch {
    return [];
  }
}

const MINE: CrmFilter[] = [{ key: "owner", op: "is_any", value: ["me"] }];

function RecordList({ type }: { type: CrmObjectType }) {
  const { orgId, settings } = useCrm();
  const props = useCrmProperties(orgId, type);
  const views = useCrmViews(orgId, type);
  const tags = useCrmTags(orgId);
  const pipelines = useCrmPipelines(orgId);
  const [viewId, setViewId] = useUrlState("view", "all");
  const [q, setQ] = useUrlState("q", "");
  const [fRaw, setFRaw] = useUrlState("f", "");
  const [sort, setSort] = useUrlState("sort", "");
  const [dir, setDir] = useUrlState<CrmSortDir | "">("dir", "");
  const [colsRaw, setColsRaw] = useUrlState("cols", "");
  const [search, setSearch] = useState(q);
  useEffect(() => setSearch(q), [q]);
  useEffect(() => {
    const t = setTimeout(() => {
      if (search !== q) setQ(search);
    }, 300);
    return () => clearTimeout(t);
  }, [search, q, setQ]);

  const filters = useMemo(() => parseFilters(fRaw), [fRaw]);
  const columns = colsRaw ? colsRaw.split(",") : DEFAULT_COLUMNS[type];
  const params = useMemo(() => ({ filters, q, ...(sort ? { sort } : {}), ...(dir ? { dir: dir as CrmSortDir } : {}), tz: crmTimeZone(), limit: 50 }), [filters, q, sort, dir]);
  const list = useCrmRecords(orgId, type, params);
  const total = crmTotal(list.data);

  const [filtering, setFiltering] = useState(false);
  const [picking, setPicking] = useState(false);
  const [saving, setSaving] = useState<"new" | "rename" | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  useEffect(() => setSelected(new Set()), [fRaw, q, sort, dir, viewId]);

  const current = views.data?.find((v) => v.id === viewId) ?? null;
  const apply = (v: { filters: CrmFilter[]; sort?: string; dir?: string; columns?: string[]; q?: string }) => {
    setFRaw(v.filters.length ? JSON.stringify(v.filters) : "");
    setSort(v.sort ?? "");
    setDir((v.dir as CrmSortDir) ?? "");
    setColsRaw(v.columns && v.columns.length ? v.columns.join(",") : "");
    setQ(v.q ?? "");
  };
  const openView = (id: string) => {
    setViewId(id);
    if (id === "all") apply({ filters: [] });
    else if (id === "mine") apply({ filters: MINE });
    else {
      const v = views.data?.find((x) => x.id === id);
      if (v) apply({ filters: v.filters, sort: v.sort_key, dir: v.sort_dir, columns: v.columns, q: v.q });
    }
  };
  const modified =
    current !== null &&
    (JSON.stringify(current.filters) !== JSON.stringify(filters) || current.q !== q || (current.sort_key || "") !== (sort || current.sort_key) || current.columns.join(",") !== columns.join(","));

  const createView = useCrmCreateView(orgId, type);
  const updateView = useCrmUpdateView(orgId, type);
  const deleteView = useCrmDeleteView(orgId, type);
  const exporter = useCrmExport(orgId, type);
  const propList = props.data ?? [];
  const shownProps = columns.map((k) => propList.find((p) => p.key === k)).filter((p): p is CrmProperty => !!p);
  const ctx = { tags: tags.data ?? [], pipelines: pipelines.data ?? [], currency: settings.currency };

  const toggleSort = (p: CrmProperty) => {
    if (!p.sortable) return;
    if (sort === p.key) setDir(dir === "asc" ? "desc" : "asc");
    else {
      setSort(p.key);
      setDir(["name", "first_name", "last_name", "company"].includes(p.key) ? "asc" : "desc");
    }
  };
  const doExport = () =>
    exporter.mutate(
      { filters, q, ...(sort ? { sort } : {}), ...(dir ? { dir: dir as CrmSortDir } : {}), columns },
      {
        onSuccess: (csv) => {
          const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
          const url = URL.createObjectURL(blob);
          const a = document.createElement("a");
          a.href = url;
          a.download = `${CRM_TYPE_LABEL[type].many}-${new Date().toISOString().slice(0, 10)}.csv`;
          document.body.appendChild(a);
          a.click();
          a.remove();
          setTimeout(() => URL.revokeObjectURL(url), 1000);
          toast.success("Export downloaded");
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't export.")),
      },
    );

  const rows = list.data?.items ?? [];
  const allSelected = rows.length > 0 && rows.every((r) => selected.has(r.id));
  return (
    <section className="flex flex-col gap-4" aria-label={CRM_TYPE_LABEL[type].Many}>
      <div className="-mx-1 flex items-center gap-1 overflow-x-auto px-1 pb-1" role="tablist" aria-label="Views">
        {[
          { id: "all", name: `All ${CRM_TYPE_LABEL[type].many}` },
          { id: "mine", name: `My ${CRM_TYPE_LABEL[type].many}` },
          ...(views.data ?? []).map((v) => ({ id: v.id, name: v.name, shared: v.shared })),
        ].map((v) => (
          <button
            key={v.id}
            type="button"
            role="tab"
            aria-selected={viewId === v.id}
            onClick={() => openView(v.id)}
            className={cn(
              "inline-flex h-control shrink-0 items-center gap-1.5 rounded-md px-3 type-label font-semibold transition-colors",
              viewId === v.id ? "bg-sunken text-fg" : "text-fg-muted hover:bg-sunken hover:text-fg",
            )}
          >
            {v.name}
            {"shared" in v && v.shared ? <Badge tone="info">Shared</Badge> : null}
          </button>
        ))}
        {current?.can_edit ? (
          <Menu trigger={<IconButton label="View actions" icon={<MoreHorizontal />} size="sm" />}>
            <MenuItem icon={<Pencil />} onSelect={() => setSaving("rename")}>
              Rename or share
            </MenuItem>
            <MenuSeparator />
            <MenuItem
              icon={<Trash2 />}
              destructive
              onSelect={() =>
                deleteView.mutate(current.id, {
                  onSuccess: () => {
                    toast.success("View deleted");
                    openView("all");
                  },
                  onError: (e) => toast.error(errorMessage(e, "Couldn't delete the view.")),
                })
              }
            >
              Delete view
            </MenuItem>
          </Menu>
        ) : null}
      </div>
      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        <Input
          aria-label={`Search ${CRM_TYPE_LABEL[type].many}`}
          placeholder={type === "contact" ? "Search name, email or phone" : type === "company" ? "Search name or domain" : "Search deals"}
          leadingIcon={<Search aria-hidden />}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          wrapperClassName="md:w-72"
        />
        <div className="flex flex-wrap items-center gap-2 md:ml-auto">
          <Button variant="secondary" size="sm" leftIcon={<Filter />} onClick={() => setFiltering(true)}>
            Filters{filters.length ? ` · ${filters.length}` : ""}
          </Button>
          <Button variant="secondary" size="sm" leftIcon={<Columns3 />} onClick={() => setPicking(true)}>
            Columns
          </Button>
          {modified && current?.can_edit ? (
            <Button
              variant="secondary"
              size="sm"
              leftIcon={<Save />}
              loading={updateView.isPending}
              onClick={() =>
                updateView.mutate(
                  { id: current.id, filters, q, columns, ...(sort ? { sort_key: sort } : {}), ...(dir ? { sort_dir: dir as CrmSortDir } : {}) },
                  { onSuccess: () => toast.success("View saved"), onError: (e) => toast.error(errorMessage(e, "Couldn't save the view.")) },
                )
              }
            >
              Save view
            </Button>
          ) : null}
          {(filters.length > 0 || q || colsRaw || sort) && !current ? (
            <Button variant="secondary" size="sm" leftIcon={<Save />} onClick={() => setSaving("new")}>
              Save as view
            </Button>
          ) : null}
          {settings.can.export ? (
            <Button variant="ghost" size="sm" leftIcon={<Download />} loading={exporter.isPending} onClick={doExport}>
              Export
            </Button>
          ) : null}
        </div>
      </div>
      {filters.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2" aria-label="Active filters">
          {filters.map((f, i) => (
            <span key={i} className="inline-flex h-8 items-center gap-1 rounded-md bg-primary-soft pl-3 pr-1 type-label text-primary-strong">
              {filterText(f, propList, ctx)}
              <IconButton
                label={`Remove filter ${filterText(f, propList, ctx)}`}
                size="sm"
                icon={<X />}
                onClick={() => {
                  const next = filters.filter((_, j) => j !== i);
                  setFRaw(next.length ? JSON.stringify(next) : "");
                }}
              />
            </span>
          ))}
          <Button variant="ghost" size="sm" onClick={() => setFRaw("")}>
            Clear
          </Button>
        </div>
      ) : null}
      {selected.size > 0 ? <BulkBar type={type} ids={[...selected]} onDone={() => setSelected(new Set())} /> : null}
      <Card padding="none" className="overflow-hidden">
        <div className="flex min-h-row items-center gap-2 border-b border-border px-4 type-caption text-fg-muted">
          {list.isPending ? <Skeleton shape="text" className="h-3 w-24" /> : <span>{total === null ? "" : `${total.toLocaleString()} ${total === 1 ? CRM_TYPE_LABEL[type].one : CRM_TYPE_LABEL[type].many}`}</span>}
          {list.isFetching && !list.isPending ? <span className="ml-auto">Updating…</span> : null}
        </div>
        {list.isPending || props.isPending ? (
          <SkeletonGroup label={`Loading ${CRM_TYPE_LABEL[type].many}`} className="flex flex-col">
            {[0, 1, 2, 3, 4, 5].map((i) => (
              <div key={i} className="flex h-row items-center gap-4 border-b border-border px-4 last:border-b-0">
                <Skeleton className="size-4" />
                <Skeleton shape="text" className="h-4 max-w-48 flex-1" />
                <Skeleton shape="text" className="hidden h-4 flex-1 sm:block" />
                <Skeleton shape="text" className="hidden h-4 flex-1 md:block" />
              </div>
            ))}
          </SkeletonGroup>
        ) : list.isError ? (
          <ErrorState title={`Couldn't load ${CRM_TYPE_LABEL[type].many}`} info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isRefetching} />
        ) : rows.length === 0 ? (
          (() => {
            const Icon = TYPE_ICON[type];
            return (
              <EmptyState
                icon={<Icon />}
                title={filters.length || q ? `No ${CRM_TYPE_LABEL[type].many} match` : `No ${CRM_TYPE_LABEL[type].many} yet`}
                action={
                  filters.length || q ? (
                    <Button variant="secondary" size="auto" onClick={() => openView("all")}>
                      Clear filters
                    </Button>
                  ) : undefined
                }
              />
            );
          })()
        ) : (
          <>
            {/* Phones: one row per record. */}
            <ul className="flex flex-col sm:hidden">
              {rows.map((r) => (
                <li key={r.id} className="flex items-center gap-3 border-b border-border px-4 py-3 last:border-b-0">
                  <Checkbox aria-label={`Select ${r.title}`} checked={selected.has(r.id)} onCheckedChange={(c) => setSelected(toggleSet(selected, r.id, c))} />
                  <Link href={recordHref(type, r.id)} className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="truncate type-label font-semibold text-fg">{r.title}</span>
                    <span className="truncate type-caption text-fg-muted">{mobileDetail(r, settings.currency)}</span>
                  </Link>
                </li>
              ))}
            </ul>
            {/* Wider: the table. */}
            <div className="hidden w-full overflow-x-auto sm:block">
              <table aria-label={CRM_TYPE_LABEL[type].Many} className="w-full min-w-160 border-separate border-spacing-0 text-left type-label">
                <thead>
                  <tr>
                    <th scope="col" className="sticky left-0 z-10 h-row w-10 border-b border-border bg-surface pl-4">
                      <Checkbox aria-label="Select all on this page" checked={allSelected} onCheckedChange={(c) => setSelected(c ? new Set(rows.map((r) => r.id)) : new Set())} />
                    </th>
                    {shownProps.map((p, i) => (
                      <th
                        key={p.key}
                        scope="col"
                        aria-sort={sort === p.key ? (dir === "asc" ? "ascending" : "descending") : undefined}
                        className={cn("h-row border-b border-border px-3 type-caption font-semibold whitespace-nowrap text-fg-muted last:pr-4", i === 0 && "sticky left-10 z-10 bg-surface")}
                      >
                        {p.sortable ? (
                          <button type="button" onClick={() => toggleSort(p)} className="inline-flex items-center gap-1 hover:text-fg">
                            {p.label}
                            {sort === p.key ? dir === "asc" ? <ArrowUp className="size-3" aria-hidden /> : <ArrowDown className="size-3" aria-hidden /> : null}
                          </button>
                        ) : (
                          p.label
                        )}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {rows.map((r) => (
                    <tr key={r.id} className="group [&>td]:border-b [&>td]:border-border last:[&>td]:border-b-0 hover:bg-sunken">
                      <td className="sticky left-0 z-10 h-row w-10 bg-surface pl-4 group-hover:bg-sunken">
                        <Checkbox aria-label={`Select ${r.title}`} checked={selected.has(r.id)} onCheckedChange={(c) => setSelected(toggleSet(selected, r.id, c))} />
                      </td>
                      {shownProps.map((p, i) => (
                        <td key={p.key} className={cn("h-row max-w-64 px-3 py-2 align-middle text-fg last:pr-4", i === 0 && "sticky left-10 z-10 bg-surface group-hover:bg-sunken")}>
                          <Cell rec={r} prop={p} first={i === 0} currency={settings.currency} />
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
        <LoadMore hasNextPage={!!list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
      </Card>
      {props.data ? (
        <>
          <FilterDialog
            open={filtering}
            onOpenChange={setFiltering}
            orgId={orgId}
            props={props.data}
            filters={filters}
            currency={settings.currency}
            onApply={(f) => {
              setFRaw(f.length ? JSON.stringify(f) : "");
              setFiltering(false);
            }}
          />
          <ColumnDialog
            open={picking}
            onOpenChange={setPicking}
            props={props.data}
            columns={columns}
            onApply={(c) => {
              setColsRaw(c.join(",") === DEFAULT_COLUMNS[type].join(",") ? "" : c.join(","));
              setPicking(false);
            }}
          />
        </>
      ) : null}
      <SaveViewDialog
        open={saving !== null}
        onOpenChange={(v) => !v && setSaving(null)}
        {...(saving === "rename" && current ? { initial: { name: current.name, shared: current.shared } } : {})}
        saving={createView.isPending || updateView.isPending}
        onSave={({ name, shared }) => {
          if (saving === "rename" && current) {
            updateView.mutate({ id: current.id, name, shared }, { onSuccess: () => setSaving(null), onError: (e) => toast.error(errorMessage(e, "Couldn't save the view.")) });
            return;
          }
          createView.mutate(
            { name, shared, filters, q, columns, ...(sort ? { sort_key: sort } : {}), ...(dir ? { sort_dir: dir as CrmSortDir } : {}) },
            {
              onSuccess: (v: CrmView) => {
                setSaving(null);
                setViewId(v.id);
                toast.success(`“${v.name}” saved`);
              },
              onError: (e) => toast.error(errorMessage(e, "Couldn't save the view.")),
            },
          );
        }}
      />
    </section>
  );
}

function toggleSet(s: Set<string>, id: string, on: boolean): Set<string> {
  const n = new Set(s);
  if (on) n.add(id);
  else n.delete(id);
  return n;
}

function mobileDetail(r: CrmRecord, currency: string): string {
  switch (r.type) {
    case "contact":
      return [r.company?.name, r.emails[0]].filter(Boolean).join(" · ") || r.job_title;
    case "company":
      return [r.domain, r.industry].filter(Boolean).join(" · ");
    default:
      return [r.stage.name, r.amount !== null ? crmMoney(r.amount, r.currency) : "", r.close_date ? dayLabel(r.close_date) : ""].filter(Boolean).join(" · ") || currency;
  }
}

function Cell({ rec, prop, first, currency }: { rec: CrmRecord; prop: CrmProperty; first: boolean; currency: string }) {
  if (first) {
    return (
      <Link href={recordHref(rec.type, rec.id)} className="block truncate font-semibold text-fg hover:underline">
        {rec.title}
      </Link>
    );
  }
  switch (prop.key) {
    case "owner":
      return <PersonChip user={rec.owner} />;
    case "tags":
      return <TagList tags={rec.tags} />;
    case "stage":
      return rec.type === "deal" ? <StageBadge name={rec.stage.name} outcome={rec.stage.outcome} /> : null;
    case "amount":
      return rec.type === "deal" ? <MoneyText minor={rec.amount} currency={rec.currency} /> : null;
    case "weighted_amount":
      return rec.type === "deal" ? <MoneyText minor={rec.weighted_amount} currency={rec.currency} /> : null;
    case "company":
      return rec.type !== "company" && rec.company ? <RecordLink type="company" id={rec.company.id} name={rec.company.name} /> : <span className="text-fg-subtle">—</span>;
    case "emails":
      return rec.type === "contact" && rec.emails[0] ? (
        <a href={`mailto:${rec.emails[0]}`} className="truncate text-fg hover:underline">
          {rec.emails[0]}
        </a>
      ) : (
        <span className="text-fg-subtle">—</span>
      );
    case "phones":
      return rec.type === "contact" && rec.phones[0] ? (
        <a href={`tel:${rec.phones[0].replace(/[^\d+]/g, "")}`} className="whitespace-nowrap tabular-nums text-fg hover:underline">
          {rec.phones[0]}
        </a>
      ) : (
        <span className="text-fg-subtle">—</span>
      );
  }
  if (prop.type === "datetime") {
    const v = (rec as unknown as Record<string, string | null>)[prop.key];
    return v ? <span className="whitespace-nowrap text-fg-muted">{formatRelative(v)}</span> : <span className="text-fg-subtle">—</span>;
  }
  if (prop.type === "date") {
    const v = prop.builtin ? (rec as unknown as Record<string, string | null>)[prop.key] : (rec.props[prop.key] as string | null | undefined);
    return v ? <span className="whitespace-nowrap tabular-nums">{dayLabel(v)}</span> : <span className="text-fg-subtle">—</span>;
  }
  if (!prop.builtin && prop.type === "currency") {
    const v = rec.props[prop.key];
    return <MoneyText minor={typeof v === "number" ? v : null} currency={currency} />;
  }
  const text = crmFieldText(rec, prop, { currency });
  return text ? <span className="block truncate">{text}</span> : <span className="text-fg-subtle">—</span>;
}

/** Actions on the selected rows. */
function BulkBar({ type, ids, onDone }: { type: CrmObjectType; ids: string[]; onDone: () => void }) {
  const { orgId, settings } = useCrm();
  const bulk = useCrmBulk(orgId);
  const tags = useCrmTags(orgId);
  const pipelines = useCrmPipelines(type === "deal" ? orgId : null);
  const [assigning, setAssigning] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const label = `${ids.length} ${ids.length === 1 ? CRM_TYPE_LABEL[type].one : CRM_TYPE_LABEL[type].many}`;
  const run = (body: Omit<Parameters<typeof bulk.mutate>[0], "object_type" | "ids">, done: string) =>
    bulk.mutateAsync({ object_type: type, ids, ...body }).then(
      (r) => {
        toast.success(r.skipped.length ? `${done}: ${r.updated}. ${r.skipped.length} skipped (no edit access).` : `${done}: ${r.updated}`);
        onDone();
      },
      (e: unknown) => {
        toast.error(errorMessage(e, "Couldn't update the records."));
        throw e;
      },
    );
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-md bg-sunken px-3 py-2" role="region" aria-label="Selected records">
      <span className="type-label font-semibold text-fg">{label} selected</span>
      <div className="flex flex-wrap items-center gap-2 sm:ml-auto">
        <Button variant="secondary" size="sm" leftIcon={<UserRound />} onClick={() => setAssigning(true)}>
          Assign
        </Button>
        <Menu
          trigger={
            <Button variant="secondary" size="sm" leftIcon={<TagIcon />}>
              Tag
            </Button>
          }
        >
          <MenuLabel>Add a tag</MenuLabel>
          {(tags.data ?? []).map((t) => (
            <MenuItem key={t.id} onSelect={() => void run({ action: "add_tag", tag_id: t.id }, `Tagged “${t.name}”`).catch(() => undefined)}>
              {t.name}
            </MenuItem>
          ))}
          {(tags.data ?? []).length === 0 ? <MenuLabel>No tags yet</MenuLabel> : null}
        </Menu>
        {type === "deal" ? (
          <Menu
            trigger={
              <Button variant="secondary" size="sm" leftIcon={<KanbanSquare />}>
                Move
              </Button>
            }
          >
            {(pipelines.data ?? []).map((p) => (
              <div key={p.id}>
                {(pipelines.data?.length ?? 0) > 1 ? <MenuLabel>{p.name}</MenuLabel> : null}
                {p.stages.map((s) => (
                  <MenuItem key={s.id} onSelect={() => void run({ action: "move_stage", stage_id: s.id }, `Moved to ${s.name}`).catch(() => undefined)}>
                    {s.name}
                  </MenuItem>
                ))}
              </div>
            ))}
          </Menu>
        ) : null}
        {settings.can.delete ? (
          <Button variant="danger-ghost" size="sm" leftIcon={<Trash2 />} onClick={() => setDeleting(true)}>
            Delete
          </Button>
        ) : null}
        <IconButton label="Clear selection" icon={<X />} size="sm" onClick={onDone} />
      </div>
      <Dialog open={assigning} onOpenChange={setAssigning} title={`Assign ${label}`} size="sm">
        <div className="flex flex-col gap-4">
          <MemberPicker
            orgId={orgId}
            label="Choose the owner"
            autoFocus
            onPick={(u) => {
              setAssigning(false);
              void run({ action: "assign_owner", owner_id: u.id }, `Assigned to ${u.name}`).catch(() => undefined);
            }}
          />
          <Button
            variant="ghost"
            onClick={() => {
              setAssigning(false);
              void run({ action: "assign_owner", owner_id: null }, "Unassigned").catch(() => undefined);
            }}
          >
            Remove the owner
          </Button>
        </div>
      </Dialog>
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        tone="danger"
        title={`Delete ${label}?`}
        confirmLabel={`Delete ${label}`}
        onConfirm={() => run({ action: "delete" }, "Deleted")}
      >
        Their timelines go too, and notes logged only on them. It can&apos;t be undone.
      </ConfirmDialog>
    </div>
  );
}
