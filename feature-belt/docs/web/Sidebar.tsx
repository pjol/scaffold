"use client";

import {
  docsTimeAgo,
  docsTitle,
  errorMessage,
  orgsNoun,
  useDocsChildren,
  useDocsDuplicatePage,
  useDocsFind,
  useDocsMovePage,
  useDocsRecent,
  useDocsSetTeamspaceHidden,
  useDocsSidebar,
  useDocsTrashPage,
  type DocsFindParams,
  type DocsPageSummary,
} from "@scaffoldapp/shared";
import { Dialog, ErrorState, IconButton, Input, Skeleton, SkeletonRow, Tooltip, cn, toast } from "@scaffoldapp/ui-web";
import { ChevronDown, ChevronRight, Copy, CornerUpRight, Eye, EyeOff, LayoutTemplate, Plus, Search, Trash2, Upload, Users } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, type DragEvent, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { ConnectedDocsSection } from "./connected/ConnectedDocsSection";
import { ImportDialog, PagePicker, TrashDialog } from "./Dialogs";
import { NewPageDialog } from "./NewPageDialog";
import { PageIcon } from "./PageIcon";

const OPEN_KEY = "scaffoldapp.docs.tree";
const SECTION_KEY = "scaffoldapp.docs.sections";

function loadSet(key: string): Set<string> {
  try {
    return new Set(JSON.parse(window.localStorage.getItem(key) ?? "[]") as string[]);
  } catch {
    return new Set();
  }
}

function saveSet(key: string, set: Set<string>) {
  try {
    window.localStorage.setItem(key, JSON.stringify([...set].slice(-300)));
  } catch {
    // Not persisted in private mode.
  }
}

interface TreeCtx {
  orgId: string;
  open: Set<string>;
  toggle: (id: string, value?: boolean) => void;
  activeId: string | null;
  drag: { id: string } | null;
  setDrag: (d: { id: string } | null) => void;
  onDrop: (dragged: string, target: DocsPageSummary, place: "before" | "after" | "inside", siblings: DocsPageSummary[]) => void;
  onNew: (parent: string) => void;
  onMove: (page: DocsPageSummary) => void;
}

function Row({ page, depth, siblings, ctx }: { page: DocsPageSummary; depth: number; siblings: DocsPageSummary[]; ctx: TreeCtx }) {
  const isOpen = ctx.open.has(page.id);
  const kids = useDocsChildren(page.id, isOpen);
  const [over, setOver] = useState<"before" | "after" | "inside" | null>(null);
  const trash = useDocsTrashPage(ctx.orgId);
  const duplicate = useDocsDuplicatePage(ctx.orgId);
  const router = useRouter();
  const active = ctx.activeId === page.id;
  const canEdit = page.my_level === "edit" || page.my_level === "manage";
  const place = (e: DragEvent<HTMLDivElement>): "before" | "after" | "inside" => {
    const r = e.currentTarget.getBoundingClientRect();
    const y = e.clientY - r.top;
    if (y < r.height * 0.28) return "before";
    if (y > r.height * 0.72) return "after";
    return page.kind === "database" ? "after" : "inside";
  };
  return (
    <li role="treeitem" aria-expanded={page.has_children ? isOpen : undefined} aria-selected={active} className="flex flex-col">
      <div
        draggable
        onDragStart={(e) => {
          ctx.setDrag({ id: page.id });
          e.dataTransfer.effectAllowed = "move";
          e.dataTransfer.setData("text/plain", docsTitle(page.title));
        }}
        onDragEnd={() => ctx.setDrag(null)}
        onDragOver={(e) => {
          if (!ctx.drag || ctx.drag.id === page.id) return;
          e.preventDefault();
          setOver(place(e));
        }}
        onDragLeave={() => setOver(null)}
        onDrop={(e) => {
          e.preventDefault();
          const p = place(e);
          setOver(null);
          if (ctx.drag && ctx.drag.id !== page.id) ctx.onDrop(ctx.drag.id, page, p, siblings);
          ctx.setDrag(null);
        }}
        style={{ paddingLeft: `${depth * 12 + 4}px` }}
        className={cn(
          "group/row relative flex min-h-8 items-center gap-0.5 rounded-sm pr-1 pointer-coarse:min-h-tap",
          active ? "bg-sunken font-medium text-fg" : "text-fg-muted hover:bg-sunken hover:text-fg",
          over === "inside" && "ring-2 ring-primary",
        )}
      >
        {over === "before" ? <span aria-hidden className="absolute -top-px right-1 left-1 h-0.5 rounded-pill bg-primary" /> : null}
        {over === "after" ? <span aria-hidden className="absolute right-1 -bottom-px left-1 h-0.5 rounded-pill bg-primary" /> : null}
        <button
          type="button"
          aria-label={isOpen ? `Collapse ${docsTitle(page.title)}` : `Expand ${docsTitle(page.title)}`}
          className={cn("flex size-6 shrink-0 items-center justify-center rounded-sm hover:bg-surface focus-visible:outline-2 focus-visible:outline-ring [&_svg]:size-4", !page.has_children && page.kind !== "page" && "invisible")}
          onClick={() => ctx.toggle(page.id)}
        >
          {isOpen ? <ChevronDown aria-hidden /> : <ChevronRight aria-hidden className={cn(!page.has_children && "opacity-40")} />}
        </button>
        <Link href={`/docs/${page.id}`} aria-current={active ? "page" : undefined} className="flex min-w-0 flex-1 items-center gap-2 rounded-sm py-1 focus-visible:outline-2 focus-visible:outline-ring">
          <PageIcon icon={page.icon} custom={page.custom_icon} kind={page.kind} size="xs" />
          <span className="truncate type-label">{docsTitle(page.title)}</span>
        </Link>
        <span className="flex items-center opacity-100 transition-opacity lg:opacity-0 lg:group-hover/row:opacity-100 lg:group-focus-within/row:opacity-100 motion-reduce:transition-none">
          <ResourceMenu
            feature="docs"
            kind="docs.page"
            id={page.id}
            title={docsTitle(page.title)}
            orgId={ctx.orgId}
            label={`More actions for ${docsTitle(page.title)}`}
            actions={
              canEdit
                ? [
                    ...(page.kind === "page" ? [{ id: "new", label: "Add a page inside", icon: Plus, onSelect: () => ctx.onNew(page.id) }] : []),
                    { id: "dup", label: "Duplicate", icon: Copy, onSelect: () => duplicate.mutate(page.id, { onSuccess: (p) => router.push(`/docs/${p.id}`), onError: (e) => toast.error(errorMessage(e, "Couldn't duplicate the page.")) }) },
                    { id: "move", label: "Move to…", icon: CornerUpRight, onSelect: () => ctx.onMove(page) },
                    {
                      id: "trash",
                      label: "Move to trash",
                      icon: Trash2,
                      destructive: true,
                      onSelect: () => trash.mutate({ pageId: page.id }, { onSuccess: () => toast.success(`“${docsTitle(page.title)}” is in the trash.`), onError: (e) => toast.error(errorMessage(e, "Couldn't move it to the trash.")) }),
                    },
                  ]
                : []
            }
          />
          {canEdit && page.kind === "page" ? <IconButton label={`Add a page inside ${docsTitle(page.title)}`} icon={<Plus />} size="sm" variant="ghost" onClick={() => ctx.onNew(page.id)} /> : null}
        </span>
      </div>
      {isOpen ? (
        kids.isPending ? (
          <Skeleton className="my-1 h-6" style={{ marginLeft: `${(depth + 1) * 12 + 28}px` }} />
        ) : kids.data && kids.data.length > 0 ? (
          <ul role="group" className="flex flex-col">
            {kids.data.map((c) => (
              <Row key={c.id} page={c} depth={depth + 1} siblings={kids.data} ctx={ctx} />
            ))}
          </ul>
        ) : (
          <p className="py-1 type-caption text-fg-subtle" style={{ paddingLeft: `${(depth + 1) * 12 + 32}px` }}>
            No pages inside
          </p>
        )
      ) : null}
    </li>
  );
}

function Section({ id, title, info, action, children, closed, onToggle }: { id: string; title: ReactNode; info?: string; action?: ReactNode; children: ReactNode; closed: Set<string>; onToggle: (id: string) => void }) {
  const open = !closed.has(id);
  return (
    <section className="flex flex-col gap-0.5">
      <div className="flex min-h-8 items-center gap-1 px-1">
        <button type="button" aria-expanded={open} onClick={() => onToggle(id)} className="flex min-w-0 flex-1 items-center gap-1 rounded-sm px-1 py-1 text-left type-caption font-semibold text-fg-muted hover:text-fg focus-visible:outline-2 focus-visible:outline-ring">
          {open ? <ChevronDown aria-hidden className="size-4 shrink-0" /> : <ChevronRight aria-hidden className="size-4 shrink-0" />}
          {info ? (
            <Tooltip content={info}>
              <span className="truncate">{title}</span>
            </Tooltip>
          ) : (
            <span className="truncate">{title}</span>
          )}
        </button>
        {action}
      </div>
      {open ? children : null}
    </section>
  );
}

const KIND_FILTERS: { value: DocsFindParams["kind"] | "all"; label: string }[] = [
  { value: "all", label: "All" },
  { value: "page", label: "Pages" },
  { value: "database", label: "Databases" },
];
const EDITED_FILTERS: { value: DocsFindParams["edited"] | "any"; label: string }[] = [
  { value: "any", label: "Any time" },
  { value: "today", label: "Today" },
  { value: "week", label: "Past week" },
  { value: "month", label: "Past month" },
];

function Chip({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn("rounded-pill px-3 py-1 type-caption focus-visible:outline-2 focus-visible:outline-ring", active ? "bg-fg text-fg-inverse" : "bg-sunken text-fg-muted hover:text-fg")}
    >
      {children}
    </button>
  );
}

/** Quick find (⌘P): titles and text, filtered by type, author, when it was edited and where it lives. */
function QuickFind({ open, onOpenChange, orgId, activeId }: { open: boolean; onOpenChange: (o: boolean) => void; orgId: string; activeId: string | null }) {
  const [q, setQ] = useState("");
  const [kind, setKind] = useState<DocsFindParams["kind"] | "all">("all");
  const [mine, setMine] = useState(false);
  const [edited, setEdited] = useState<DocsFindParams["edited"] | "any">("any");
  const [inPage, setInPage] = useState(false);
  const [sort, setSort] = useState<"relevance" | "edited">("relevance");
  const [cursor, setCursor] = useState(0);
  const params: DocsFindParams = {
    q,
    ...(kind !== "all" && kind ? { kind } : {}),
    ...(mine ? { created_by: "me" } : {}),
    ...(edited !== "any" && edited ? { edited } : {}),
    ...(inPage && activeId ? { in: activeId } : {}),
    sort: q.trim() ? sort : "edited",
    limit: 20,
  };
  const results = useDocsFind(orgId, params, open);
  const router = useRouter();
  const items = results.data ?? [];
  useEffect(() => setCursor(0), [q, kind, mine, edited, inPage, sort]);
  const go = (id: string) => {
    onOpenChange(false);
    setQ("");
    router.push(`/docs/${id}`);
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Find a page" size="lg">
      <div className="flex flex-col gap-3">
        <Input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search titles and text"
          aria-label="Search titles and text"
          autoFocus
          leadingIcon={<Search />}
          role="combobox"
          aria-expanded={items.length > 0}
          aria-controls="docs-find-results"
          aria-activedescendant={items[cursor] ? `find-${items[cursor].id}` : undefined}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setCursor((c) => Math.min(c + 1, items.length - 1));
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setCursor((c) => Math.max(c - 1, 0));
            } else if (e.key === "Enter" && items[cursor]) {
              e.preventDefault();
              go(items[cursor].id);
            }
          }}
        />
        <div className="flex flex-wrap items-center gap-1" aria-label="Filters" role="group">
          {KIND_FILTERS.map((f) => (
            <Chip key={f.label} active={kind === f.value} onClick={() => setKind(f.value)}>
              {f.label}
            </Chip>
          ))}
          <span aria-hidden className="mx-1 h-4 w-px bg-border" />
          <Chip active={mine} onClick={() => setMine((m) => !m)}>
            Created by me
          </Chip>
          {activeId ? (
            <Chip active={inPage} onClick={() => setInPage((v) => !v)}>
              In this page
            </Chip>
          ) : null}
          <span aria-hidden className="mx-1 h-4 w-px bg-border" />
          {EDITED_FILTERS.map((f) => (
            <Chip key={f.label} active={edited === f.value} onClick={() => setEdited(f.value)}>
              {f.label}
            </Chip>
          ))}
          {q.trim() ? (
            <>
              <span aria-hidden className="mx-1 h-4 w-px bg-border" />
              <Chip active={sort === "edited"} onClick={() => setSort((s) => (s === "edited" ? "relevance" : "edited"))}>
                Newest first
              </Chip>
            </>
          ) : null}
        </div>
        {!q.trim() ? <p className="type-caption font-semibold text-fg-muted">Recently edited</p> : null}
        <ul id="docs-find-results" role="listbox" aria-label="Pages" className="flex max-h-96 flex-col overflow-y-auto">
          {results.isPending ? (
            <>
              <SkeletonRow />
              <SkeletonRow />
            </>
          ) : results.isError ? (
            <ErrorState title="Couldn't search." onRetry={() => void results.refetch()} />
          ) : items.length === 0 ? (
            <li className="px-3 py-2 type-label text-fg-muted">No pages match.</li>
          ) : (
            items.map((h, i) => (
              <li key={h.id} id={`find-${h.id}`} role="option" aria-selected={i === cursor}>
                <button
                  type="button"
                  onClick={() => go(h.id)}
                  onMouseEnter={() => setCursor(i)}
                  className={cn("flex w-full items-start gap-2 rounded-sm px-3 py-2 text-left focus-visible:outline-2 focus-visible:outline-ring", i === cursor ? "bg-sunken" : "hover:bg-sunken")}
                >
                  <PageIcon icon={h.icon} custom={h.custom_icon} kind={h.kind} size="sm" />
                  <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="truncate type-body text-fg">{docsTitle(h.title)}</span>
                    {h.snippet ? <span className="line-clamp-2 type-caption text-fg-muted">{h.snippet}</span> : null}
                    <span className="truncate type-caption text-fg-subtle">{[h.parent_title ? `In ${h.parent_title}` : "", `edited ${docsTimeAgo(h.updated_at)}`].filter(Boolean).join(" · ")}</span>
                  </span>
                </button>
              </li>
            ))
          )}
        </ul>
      </div>
    </Dialog>
  );
}

/** The workspace tree: Recent, Favorites, Teamspaces, Shared, Private and Connected, with drag to reorder and nest. */
export function Sidebar({ orgId, orgName }: { orgId: string; orgName: string }) {
  const sidebar = useDocsSidebar(orgId);
  const recent = useDocsRecent(orgId);
  const pathname = usePathname() ?? "";
  const move = useDocsMovePage(orgId);
  const setHidden = useDocsSetTeamspaceHidden(orgId);
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [closed, setClosed] = useState<Set<string>>(new Set(["recent"]));
  const [drag, setDrag] = useState<{ id: string } | null>(null);
  const [dialog, setDialog] = useState<null | "find" | "trash" | "import">(null);
  const [newPage, setNewPage] = useState<null | { parentId: string | null; tab: "empty" | "templates" | "import"; teamId?: string; shared?: boolean }>(null);
  const [moving, setMoving] = useState<DocsPageSummary | null>(null);
  useEffect(() => {
    setOpen(loadSet(`${OPEN_KEY}.${orgId}`));
    const s = loadSet(`${SECTION_KEY}.${orgId}`);
    if (s.size) setClosed(s);
  }, [orgId]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "p") {
        e.preventDefault();
        setDialog("find");
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  const activeId = /^\/docs\/([0-9a-f-]{36})/.exec(pathname)?.[1] ?? null;
  const toggle = (id: string, value?: boolean) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (value ?? !next.has(id)) next.add(id);
      else next.delete(id);
      saveSet(`${OPEN_KEY}.${orgId}`, next);
      return next;
    });
  const toggleSection = (id: string) =>
    setClosed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      saveSet(`${SECTION_KEY}.${orgId}`, next);
      return next;
    });
  const ctx: TreeCtx = {
    orgId,
    open,
    toggle,
    activeId,
    drag,
    setDrag,
    onNew: (parent) => {
      toggle(parent, true);
      setNewPage({ parentId: parent, tab: "empty" });
    },
    onMove: setMoving,
    onDrop: (dragged, target, place, siblings) => {
      const parent = place === "inside" ? target.id : target.parent_id;
      let after: string | null = null;
      if (place === "after") after = target.id;
      if (place === "before") {
        const i = siblings.findIndex((s) => s.id === target.id);
        after = siblings.slice(0, i).reverse().find((s) => s.id !== dragged)?.id ?? null;
      }
      if (place === "inside") toggle(target.id, true);
      move.mutate(
        { pageId: dragged, parent_id: parent, ...(after ? { after_id: after } : place === "inside" ? { last: true } : {}) },
        { onError: (e) => toast.error(errorMessage(e, "Couldn't move the page.")) },
      );
    },
  };
  const list = (items: DocsPageSummary[]) => (
    <ul role="tree" className="flex flex-col">
      {items.map((p) => (
        <Row key={p.id} page={p} depth={0} siblings={items} ctx={ctx} />
      ))}
    </ul>
  );
  const footerItem = "flex min-h-8 items-center gap-2 rounded-sm px-2 type-label text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-tap";
  const hidden = sidebar.data?.hidden_teamspaces ?? [];
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-1 border-b border-border px-3 py-2">
        <h2 className="min-w-0 flex-1 truncate type-title-sm text-fg">{orgName}</h2>
        <IconButton label="Find a page (⌘P)" icon={<Search />} size="sm" variant="ghost" onClick={() => setDialog("find")} />
        <IconButton label="New page" icon={<Plus />} size="sm" variant="ghost" onClick={() => setNewPage({ parentId: null, tab: "empty" })} />
      </div>
      <nav aria-label="Pages" className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-2">
        {sidebar.isPending ? (
          <div className="flex flex-col gap-2 p-2">
            {Array.from({ length: 6 }, (_, i) => (
              <Skeleton key={i} className="h-6 w-3/4" />
            ))}
          </div>
        ) : sidebar.isError ? (
          <ErrorState title="Couldn't load the pages." onRetry={() => void sidebar.refetch()} />
        ) : (
          <>
            {recent.data?.length ? (
              <Section id="recent" title="Recent" closed={closed} onToggle={toggleSection}>
                <ul className="flex flex-col">
                  {recent.data.slice(0, 6).map((p) => (
                    <li key={p.id}>
                      <Link
                        href={`/docs/${p.id}`}
                        aria-current={activeId === p.id ? "page" : undefined}
                        className={cn("flex min-h-8 items-center gap-2 rounded-sm px-2 py-1 pointer-coarse:min-h-tap focus-visible:outline-2 focus-visible:outline-ring", activeId === p.id ? "bg-sunken text-fg" : "text-fg-muted hover:bg-sunken hover:text-fg")}
                      >
                        <PageIcon icon={p.icon} custom={p.custom_icon} kind={p.kind} size="xs" />
                        <span className="min-w-0 flex-1 truncate type-label">{docsTitle(p.title)}</span>
                      </Link>
                    </li>
                  ))}
                </ul>
              </Section>
            ) : null}
            {sidebar.data.favorites.length ? (
              <Section id="favorites" title="Favorites" closed={closed} onToggle={toggleSection}>
                {list(sidebar.data.favorites)}
              </Section>
            ) : null}
            {(sidebar.data.teamspaces ?? []).map((t) => (
              <Section
                key={t.team.id}
                id={`team:${t.team.id}`}
                title={
                  <span className="flex items-center gap-1">
                    <Users aria-hidden className="size-3.5" />
                    {t.team.name}
                  </span>
                }
                info={`Pages shared with @${t.team.handle}.`}
                closed={closed}
                onToggle={toggleSection}
                action={
                  <span className="flex items-center">
                    <IconButton label={`New page in ${t.team.name}`} icon={<Plus />} size="sm" variant="ghost" onClick={() => setNewPage({ parentId: null, tab: "empty", teamId: t.team.id })} />
                    <IconButton
                      label={`Hide ${t.team.name} from the sidebar`}
                      icon={<EyeOff />}
                      size="sm"
                      variant="ghost"
                      onClick={() =>
                        setHidden.mutate(
                          { teamId: t.team.id, hidden: true },
                          {
                            onSuccess: () => toast.success(`${t.team.name} is hidden.`, { action: { label: "Undo", onClick: () => setHidden.mutate({ teamId: t.team.id, hidden: false }) } }),
                            onError: (e) => toast.error(errorMessage(e, "Couldn't hide the teamspace.")),
                          },
                        )
                      }
                    />
                  </span>
                }
              >
                {t.pages.length ? list(t.pages) : <p className="px-3 py-1 type-caption text-fg-subtle">No pages yet</p>}
              </Section>
            ))}
            <Section
              id="shared"
              title="Shared"
              info={`Pages shared with teams, people or the whole ${orgsNoun()}.`}
              closed={closed}
              onToggle={toggleSection}
              action={sidebar.data.can_create_shared ? <IconButton label="New page for everyone" icon={<Plus />} size="sm" variant="ghost" onClick={() => setNewPage({ parentId: null, tab: "empty", shared: true })} /> : undefined}
            >
              {sidebar.data.shared.length ? list(sidebar.data.shared) : <p className="px-3 py-1 type-caption text-fg-subtle">Nothing shared yet</p>}
            </Section>
            <Section
              id="private"
              title="Private"
              info="Only you can see these, until you share them."
              closed={closed}
              onToggle={toggleSection}
              action={<IconButton label="New private page" icon={<Plus />} size="sm" variant="ghost" onClick={() => setNewPage({ parentId: null, tab: "empty" })} />}
            >
              {sidebar.data.private.length ? list(sidebar.data.private) : <p className="px-3 py-1 type-caption text-fg-subtle">No private pages</p>}
            </Section>
            {hidden.length ? (
              <Section id="hidden" title={`Hidden teamspaces (${hidden.length})`} closed={closed} onToggle={toggleSection}>
                <ul className="flex flex-col">
                  {hidden.map((t) => (
                    <li key={t.id} className="flex min-h-8 items-center gap-2 px-2 pointer-coarse:min-h-tap">
                      <Users aria-hidden className="size-4 text-fg-muted" />
                      <span className="min-w-0 flex-1 truncate type-label text-fg-muted">{t.name}</span>
                      <IconButton label={`Show ${t.name}`} icon={<Eye />} size="sm" variant="ghost" onClick={() => setHidden.mutate({ teamId: t.id, hidden: false }, { onError: (e) => toast.error(errorMessage(e, "Couldn't show the teamspace.")) })} />
                    </li>
                  ))}
                </ul>
              </Section>
            ) : null}
            <ConnectedDocsSection orgId={orgId} />
          </>
        )}
      </nav>
      <div className="flex flex-col gap-0.5 border-t border-border p-2">
        <button type="button" onClick={() => setNewPage({ parentId: null, tab: "templates" })} className={footerItem}>
          <LayoutTemplate aria-hidden className="size-4" />
          Templates
        </button>
        <button type="button" onClick={() => setDialog("import")} className={footerItem}>
          <Upload aria-hidden className="size-4" />
          Import
        </button>
        <button type="button" onClick={() => setDialog("trash")} className={footerItem}>
          <Trash2 aria-hidden className="size-4" />
          Trash
        </button>
      </div>
      <QuickFind open={dialog === "find"} onOpenChange={(o) => setDialog(o ? "find" : null)} orgId={orgId} activeId={activeId} />
      <TrashDialog open={dialog === "trash"} onOpenChange={(o) => setDialog(o ? "trash" : null)} orgId={orgId} />
      <ImportDialog open={dialog === "import"} onOpenChange={(o) => setDialog(o ? "import" : null)} orgId={orgId} parentId={null} />
      {newPage ? (
        <NewPageDialog
          key={`${newPage.parentId}-${newPage.tab}-${newPage.teamId ?? ""}-${newPage.shared ? 1 : 0}`}
          open
          onOpenChange={(o) => !o && setNewPage(null)}
          orgId={orgId}
          parentId={newPage.parentId}
          defaultTab={newPage.tab}
          {...(newPage.teamId ? { defaultPlace: { kind: "team" as const, teamId: newPage.teamId } } : newPage.shared ? { defaultPlace: { kind: "shared" as const } } : {})}
        />
      ) : null}
      <PagePicker
        open={!!moving}
        onOpenChange={(o) => !o && setMoving(null)}
        orgId={orgId}
        title={`Move “${docsTitle(moving?.title)}”`}
        exclude={moving ? [moving.id] : []}
        allowTop={moving?.my_level === "manage"}
        onPick={(parent) => {
          const p = moving;
          setMoving(null);
          if (p) move.mutate({ pageId: p.id, parent_id: parent, last: true }, { onSuccess: () => toast.success("Moved."), onError: (e) => toast.error(errorMessage(e, "Couldn't move the page.")) });
        }}
      />
    </div>
  );
}
