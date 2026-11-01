"use client";

import {
  docsTitle,
  errorMessage,
  orgsNoun,
  useDocsChildren,
  useDocsCreatePage,
  useDocsDuplicatePage,
  useDocsMovePage,
  useDocsSearch,
  useDocsSidebar,
  useDocsTrashPage,
  type DocsPageSummary,
} from "@scaffoldapp/shared";
import { Dialog, ErrorState, IconButton, Input, Skeleton, Tooltip, cn, toast } from "@scaffoldapp/ui-web";
import { ChevronDown, ChevronRight, Copy, CornerUpRight, FileText, Plus, Search, Table2, Trash2, Upload } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, type DragEvent, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { ImportDialog, PagePicker, TrashDialog } from "./Dialogs";

const OPEN_KEY = "scaffoldapp.docs.tree";

function loadOpen(orgId: string): Set<string> {
  try {
    return new Set(JSON.parse(window.localStorage.getItem(`${OPEN_KEY}.${orgId}`) ?? "[]") as string[]);
  } catch {
    return new Set();
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
          <span aria-hidden className="flex size-5 shrink-0 items-center justify-center">
            {page.icon ?? (page.kind === "database" ? <Table2 className="size-4" /> : <FileText className="size-4" />)}
          </span>
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

function Section({ title, info, action, children, empty }: { title: string; info?: string; action?: ReactNode; children: ReactNode; empty?: ReactNode }) {
  const [open, setOpen] = useState(true);
  return (
    <section className="flex flex-col gap-0.5">
      <div className="flex min-h-8 items-center gap-1 px-1">
        <button type="button" aria-expanded={open} onClick={() => setOpen((o) => !o)} className="flex min-w-0 flex-1 items-center gap-1 rounded-sm px-1 py-1 text-left type-caption font-semibold text-fg-muted hover:text-fg focus-visible:outline-2 focus-visible:outline-ring">
          {open ? <ChevronDown aria-hidden className="size-4" /> : <ChevronRight aria-hidden className="size-4" />}
          {info ? (
            <Tooltip content={info}>
              <span>{title}</span>
            </Tooltip>
          ) : (
            title
          )}
        </button>
        {action}
      </div>
      {open ? children ?? empty : null}
    </section>
  );
}

/** Quick page search inside the workspace. */
function SearchDialog({ open, onOpenChange, orgId }: { open: boolean; onOpenChange: (o: boolean) => void; orgId: string }) {
  const [q, setQ] = useState("");
  const results = useDocsSearch(orgId, q, { limit: 20, enabled: open });
  const router = useRouter();
  const go = (id: string) => {
    onOpenChange(false);
    setQ("");
    router.push(`/docs/${id}`);
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Search pages">
      <div className="flex flex-col gap-3">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (results.data?.[0]) go(results.data[0].id);
          }}
        >
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search titles and text" aria-label="Search titles and text" autoFocus leadingIcon={<Search />} />
        </form>
        <ul className="flex max-h-96 flex-col overflow-y-auto">
          {(results.data ?? []).map((h) => (
            <li key={h.id}>
              <button type="button" onClick={() => go(h.id)} className="flex w-full flex-col items-start gap-0.5 rounded-sm px-3 py-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
                <span className="flex items-center gap-2 type-body text-fg">
                  <span aria-hidden>{h.icon ?? (h.kind === "database" ? <Table2 className="size-4" /> : <FileText className="size-4" />)}</span>
                  {docsTitle(h.title)}
                </span>
                {h.snippet ? <span className="line-clamp-2 type-caption text-fg-muted">{h.snippet}</span> : null}
              </button>
            </li>
          ))}
          {!results.isPending && q.trim() && (results.data ?? []).length === 0 ? <li className="px-3 py-2 type-label text-fg-muted">No pages match.</li> : null}
        </ul>
      </div>
    </Dialog>
  );
}

/** The workspace tree: Favorites, Shared, Private, with drag to reorder and nest. */
export function Sidebar({ orgId, orgName }: { orgId: string; orgName: string }) {
  const sidebar = useDocsSidebar(orgId);
  const pathname = usePathname() ?? "";
  const router = useRouter();
  const create = useDocsCreatePage(orgId);
  const move = useDocsMovePage(orgId);
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [drag, setDrag] = useState<{ id: string } | null>(null);
  const [dialog, setDialog] = useState<null | "search" | "trash" | "import">(null);
  const [moving, setMoving] = useState<DocsPageSummary | null>(null);
  useEffect(() => setOpen(loadOpen(orgId)), [orgId]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "p" && e.shiftKey) {
        e.preventDefault();
        setDialog("search");
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
      try {
        window.localStorage.setItem(`${OPEN_KEY}.${orgId}`, JSON.stringify([...next].slice(-300)));
      } catch {
        // Not persisted in private mode.
      }
      return next;
    });
  const newPage = (parent: string | null, shared = false) =>
    create.mutate(
      { parent_id: parent, ...(parent ? {} : { shared }) },
      {
        onSuccess: (p) => {
          if (parent) toggle(parent, true);
          router.push(`/docs/${p.id}`);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't create the page.")),
      },
    );
  const ctx: TreeCtx = {
    orgId,
    open,
    toggle,
    activeId,
    drag,
    setDrag,
    onNew: (parent) => newPage(parent),
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
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-1 border-b border-border px-3 py-2">
        <h2 className="min-w-0 flex-1 truncate type-title-sm text-fg">{orgName}</h2>
        <IconButton label="Search pages (Shift+⌘P)" icon={<Search />} size="sm" variant="ghost" onClick={() => setDialog("search")} />
        <IconButton label="New private page" icon={<Plus />} size="sm" variant="ghost" loading={create.isPending} onClick={() => newPage(null, false)} />
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
            {sidebar.data.favorites.length ? <Section title="Favorites">{list(sidebar.data.favorites)}</Section> : null}
            <Section
              title="Shared"
              info={`Pages shared with teams, people or the whole ${orgsNoun()}.`}
              action={sidebar.data.can_create_shared ? <IconButton label="New page for everyone" icon={<Plus />} size="sm" variant="ghost" onClick={() => newPage(null, true)} /> : undefined}
            >
              {sidebar.data.shared.length ? list(sidebar.data.shared) : <p className="px-3 py-1 type-caption text-fg-subtle">Nothing shared yet</p>}
            </Section>
            <Section title="Private" info="Only you can see these, until you share them." action={<IconButton label="New private page" icon={<Plus />} size="sm" variant="ghost" onClick={() => newPage(null, false)} />}>
              {sidebar.data.private.length ? list(sidebar.data.private) : <p className="px-3 py-1 type-caption text-fg-subtle">No private pages</p>}
            </Section>
          </>
        )}
      </nav>
      <div className="flex flex-col gap-0.5 border-t border-border p-2">
        <button type="button" onClick={() => setDialog("import")} className="flex min-h-8 items-center gap-2 rounded-sm px-2 type-label text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-tap">
          <Upload aria-hidden className="size-4" />
          Import Markdown
        </button>
        <button type="button" onClick={() => setDialog("trash")} className="flex min-h-8 items-center gap-2 rounded-sm px-2 type-label text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-tap">
          <Trash2 aria-hidden className="size-4" />
          Trash
        </button>
      </div>
      <SearchDialog open={dialog === "search"} onOpenChange={(o) => setDialog(o ? "search" : null)} orgId={orgId} />
      <TrashDialog open={dialog === "trash"} onOpenChange={(o) => setDialog(o ? "trash" : null)} orgId={orgId} />
      <ImportDialog open={dialog === "import"} onOpenChange={(o) => setDialog(o ? "import" : null)} orgId={orgId} parentId={null} />
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
